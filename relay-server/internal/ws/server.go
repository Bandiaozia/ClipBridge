package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/clipbridge/clipbridge/relay-server/internal/auth"
	"github.com/clipbridge/clipbridge/relay-server/internal/model"
	"github.com/clipbridge/clipbridge/relay-server/internal/service"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 25 * time.Second
	authWait       = 10 * time.Second
	maxWSMessage   = 2 * 1024 * 1024
	sendQueueDepth = 128
)

type Server struct {
	store         *service.Store
	tokens        *auth.TokenManager
	hub           *Hub
	log           *slog.Logger
	allowedOrigin string
	maxCiphertext int
	upgrader      websocket.Upgrader
}

type authFrame struct {
	Type        string `json:"type"`
	AccessToken string `json:"access_token"`
	DeviceID    string `json:"device_id"`
	LastSeq     int64  `json:"last_sequence"`
}

func NewServer(store *service.Store, tokens *auth.TokenManager, hub *Hub,
	log *slog.Logger, allowedOrigin string, maxCiphertext int) *Server {
	s := &Server{store: store, tokens: tokens, hub: hub, log: log,
		allowedOrigin: allowedOrigin, maxCiphertext: maxCiphertext}
	s.upgrader = websocket.Upgrader{
		Subprotocols:     []string{"clipbridge.v1"},
		HandshakeTimeout: 10 * time.Second,
		CheckOrigin:      s.checkOrigin,
	}
	return s
}

func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	// 原生客户端通常不发送 Origin；浏览器来源必须精确匹配配置。
	return origin == "" || (s.allowedOrigin != "" && origin == s.allowedOrigin)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(maxWSMessage)
	_ = conn.SetReadDeadline(time.Now().Add(authWait))
	var first authFrame
	if err := conn.ReadJSON(&first); err != nil || first.Type != "auth" {
		closeWith(conn, 4001, "需要认证")
		return
	}
	claims, err := s.tokens.ParseAccess(first.AccessToken)
	if err != nil || claims.DeviceID != first.DeviceID ||
		!s.store.DeviceActive(r.Context(), claims.UserID, claims.DeviceID) {
		_ = conn.WriteJSON(map[string]any{"type": "auth_error", "code": "invalid_token"})
		closeWith(conn, 4001, "认证失败")
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	var closeOnce sync.Once
	client := &Client{
		UserID: claims.UserID, DeviceID: claims.DeviceID,
		Send: make(chan []byte, sendQueueDepth),
		Close: func(code int, reason string) {
			closeOnce.Do(func() { closeWith(conn, code, reason) })
		},
	}
	if replaced := s.hub.Register(client); replaced != nil {
		replaced.Close(4009, "连接已被替换")
	}
	defer func() {
		wasCurrent := s.hub.Unregister(client)
		closeOnce.Do(func() { _ = conn.Close() })
		if wasCurrent {
			s.hub.BroadcastExcept(client.UserID, client.DeviceID, map[string]any{
				"type": "device_offline", "device_id": client.DeviceID,
			})
		}
	}()
	s.hub.BroadcastExcept(client.UserID, client.DeviceID, map[string]any{
		"type": "device_online", "device_id": client.DeviceID,
	})
	s.store.Audit(r.Context(), client.UserID, client.DeviceID, "ws_connected",
		r.RemoteAddr, uuid.NewString(), true)
	writerDone := make(chan struct{})
	go s.writePump(conn, client, writerDone)
	client.Send <- mustJSON(map[string]any{
		"type": "auth_ok", "server_time": time.Now().UnixMilli(),
		"heartbeat_interval_seconds": int64(pingPeriod.Seconds()),
	})
	pending, err := s.store.PendingMessages(r.Context(), client.UserID, client.DeviceID)
	if err == nil {
		for _, message := range pending {
			if !enqueue(client, mustJSON(message)) {
				break
			}
		}
	}
	s.readPump(r.Context(), conn, client)
	client.Close(websocket.CloseNormalClosure, "连接关闭")
	<-writerDone
}

func (s *Server) readPump(ctx context.Context, conn *websocket.Conn, client *Client) {
	windowStarted := time.Now()
	framesInWindow := 0
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		now := time.Now()
		if now.Sub(windowStarted) >= time.Minute {
			windowStarted = now
			framesInWindow = 0
		}
		framesInWindow++
		if framesInWindow > 240 {
			client.Close(4008, "消息速率过高")
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		var header struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(payload, &header) != nil {
			s.sendError(client, "invalid_json")
			continue
		}
		switch header.Type {
		case "heartbeat":
			enqueue(client, mustJSON(map[string]any{
				"type": "heartbeat_ack", "server_time": time.Now().UnixMilli(),
			}))
		case "auth":
			var frame authFrame
			if json.Unmarshal(payload, &frame) != nil {
				client.Close(4001, "令牌刷新失败")
				return
			}
			claims, err := s.tokens.ParseAccess(frame.AccessToken)
			if err != nil || claims.UserID != client.UserID || claims.DeviceID != client.DeviceID {
				client.Close(4001, "令牌刷新失败")
				return
			}
			enqueue(client, mustJSON(map[string]any{"type": "auth_ok",
				"server_time": time.Now().UnixMilli()}))
		case "clipboard_text":
			s.handleMessage(ctx, client, payload)
		case "message_ack":
			s.handleAck(ctx, client, payload)
		default:
			s.sendError(client, "unsupported_type")
		}
	}
}

func (s *Server) handleMessage(ctx context.Context, client *Client, payload []byte) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil {
		s.sendError(client, "invalid_message")
		return
	}
	for _, forbidden := range []string{"text", "content", "plaintext"} {
		if _, exists := raw[forbidden]; exists {
			s.sendError(client, "plaintext_forbidden")
			return
		}
	}
	var e model.Envelope
	if json.Unmarshal(payload, &e) != nil || !s.validEnvelope(client, e) {
		s.sendError(client, "invalid_message")
		return
	}
	offlineAllowed := e.OfflineAllowed == nil || *e.OfflineAllowed
	if !offlineAllowed && !s.hub.IsOnline(client.UserID, e.RecipientDeviceID) {
		s.sendError(client, "recipient_offline")
		return
	}
	sequence, duplicate, err := s.store.QueueMessage(ctx, client.UserID, e)
	if err != nil {
		code := "storage_error"
		if errors.Is(err, service.ErrQuotaExceeded) {
			code = "quota_exceeded"
		} else if errors.Is(err, service.ErrForbidden) {
			code = "recipient_forbidden"
		}
		s.sendError(client, code)
		return
	}
	e.Sequence = sequence
	_ = s.hub.Send(client.UserID, e.RecipientDeviceID, e)
	enqueue(client, mustJSON(map[string]any{
		"type": "message_queued", "message_id": e.MessageID,
		"sequence": sequence, "duplicate": duplicate,
	}))
}

func (s *Server) validEnvelope(client *Client, e model.Envelope) bool {
	now := time.Now().UnixMilli()
	if e.Version != 1 || e.Type != "clipboard_text" ||
		e.SenderDeviceID != client.DeviceID ||
		uuid.Validate(e.MessageID) != nil ||
		uuid.Validate(e.SenderDeviceID) != nil ||
		uuid.Validate(e.RecipientDeviceID) != nil ||
		e.RecipientDeviceID == e.SenderDeviceID ||
		e.CreatedAt < now-5*60*1000 || e.CreatedAt > now+5*60*1000 ||
		e.ExpiresAt <= now || e.ExpiresAt-e.CreatedAt > int64(time.Hour/time.Millisecond) ||
		len(e.Ciphertext) == 0 || len(e.Ciphertext) > s.maxCiphertext*2 {
		return false
	}
	nonce, err1 := base64.RawURLEncoding.DecodeString(e.Nonce)
	ciphertext, err2 := base64.RawURLEncoding.DecodeString(e.Ciphertext)
	signature, err3 := base64.RawURLEncoding.DecodeString(e.Signature)
	return err1 == nil && len(nonce) == 24 && err2 == nil &&
		len(ciphertext) <= s.maxCiphertext && err3 == nil && len(signature) == 64
}

func (s *Server) handleAck(ctx context.Context, client *Client, payload []byte) {
	var ack struct {
		Type      string `json:"type"`
		MessageID string `json:"message_id"`
		Status    string `json:"status"`
	}
	if json.Unmarshal(payload, &ack) != nil || uuid.Validate(ack.MessageID) != nil ||
		(ack.Status != "processed" && ack.Status != "expired" && ack.Status != "ignored") {
		s.sendError(client, "invalid_ack")
		return
	}
	senderID, err := s.store.AckMessage(ctx, client.UserID, client.DeviceID, ack.MessageID)
	if err != nil {
		s.sendError(client, "storage_error")
		return
	}
	if senderID != "" {
		s.hub.Send(client.UserID, senderID, map[string]any{
			"type": "message_ack", "message_id": ack.MessageID,
			"status": ack.Status, "recipient_device_id": client.DeviceID,
		})
	}
}

func (s *Server) writePump(conn *websocket.Conn, client *Client, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case payload := <-client.Send:
			if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *Server) sendError(client *Client, code string) {
	enqueue(client, mustJSON(map[string]any{
		"type": "error", "code": code, "request_id": uuid.NewString(),
	}))
}

func enqueue(client *Client, payload []byte) bool {
	select {
	case client.Send <- payload:
		return true
	default:
		client.Close(4008, "发送队列已满")
		return false
	}
}

func mustJSON(value any) []byte {
	payload, _ := json.Marshal(value)
	return payload
}

func closeWith(conn *websocket.Conn, code int, reason string) {
	reason = strings.TrimSpace(reason)
	_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason), time.Now().Add(writeWait))
	_ = conn.Close()
}
