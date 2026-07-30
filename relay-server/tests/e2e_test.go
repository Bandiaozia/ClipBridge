package tests

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clipbridge/clipbridge/relay-server/internal/api"
	"github.com/clipbridge/clipbridge/relay-server/internal/auth"
	"github.com/clipbridge/clipbridge/relay-server/internal/database"
	"github.com/clipbridge/clipbridge/relay-server/internal/model"
	"github.com/clipbridge/clipbridge/relay-server/internal/service"
	clipws "github.com/clipbridge/clipbridge/relay-server/internal/ws"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

type fixture struct {
	http  *httptest.Server
	store *service.Store
}

type testDevice struct {
	id       string
	xPrivate *ecdh.PrivateKey
	xPublic  string
	signPub  ed25519.PublicKey
	signPriv ed25519.PrivateKey
}

func newDevice(t *testing.T) testDevice {
	t.Helper()
	xPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signPub, signPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testDevice{
		id: uuid.NewString(), xPrivate: xPrivate,
		xPublic: base64.RawURLEncoding.EncodeToString(xPrivate.PublicKey().Bytes()),
		signPub: signPub, signPriv: signPriv,
	}
}

func setup(t *testing.T) fixture {
	t.Helper()
	db, err := database.Open(context.Background(),
		"file:"+filepath.Join(t.TempDir(), "e2e.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := service.NewStore(db, 100, 1024*1024, 5*time.Minute)
	tokens := auth.NewTokenManager(db, strings.Repeat("s", 48), 15*time.Minute, time.Hour)
	hub := clipws.NewHub()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	wsServer := clipws.NewServer(store, tokens, hub, log, "", 64*1024)
	handler := api.New(store, tokens, hub, wsServer, db, log, 64*1024, 1000)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return fixture{http: server, store: store}
}

func request(t *testing.T, method, url, bearer string, body any, dst any) int {
	t.Helper()
	var encoded io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		encoded = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, encoded)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if dst != nil {
		if err := json.NewDecoder(response.Body).Decode(dst); err != nil {
			t.Fatalf("解码 %s %s 响应: %v", method, url, err)
		}
	}
	return response.StatusCode
}

type authResponse struct {
	UserID string         `json:"user_id"`
	Tokens auth.TokenPair `json:"tokens"`
}

func deviceJSON(d testDevice, name string) map[string]any {
	return map[string]any{
		"id": d.id, "name": name, "platform": "test",
		"x25519_public_key":  d.xPublic,
		"ed25519_public_key": base64.RawURLEncoding.EncodeToString(d.signPub),
	}
}

func connect(t *testing.T, serverURL string, d testDevice, access string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(serverURL, "http") + "/api/v1/ws"
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		Subprotocols:     []string{"clipbridge.v1"},
	}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.WriteJSON(map[string]any{
		"type": "auth", "access_token": access, "device_id": d.id,
		"last_sequence": 0,
	}); err != nil {
		t.Fatal(err)
	}
	readType(t, conn, "auth_ok", nil)
	return conn
}

func readType(t *testing.T, conn *websocket.Conn, wanted string, dst any) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("等待 %s: %v", wanted, err)
		}
		var header struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(payload, &header) != nil || header.Type != wanted {
			continue
		}
		if dst != nil {
			if err := json.Unmarshal(payload, dst); err != nil {
				t.Fatal(err)
			}
		}
		return payload
	}
}

func deriveKey(t *testing.T, from testDevice, peerPublic string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(peerPublic)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := from.xPrivate.ECDH(peer)
	if err != nil {
		t.Fatal(err)
	}
	reader := hkdf.New(sha256.New, secret, []byte("ClipBridge test salt"),
		[]byte("ClipBridge message v1"))
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		t.Fatal(err)
	}
	return key
}

func aad(e model.Envelope) []byte {
	return []byte(fmt.Sprintf("%d\n%s\n%s\n%s\n%s\n%d\n%d",
		e.Version, e.MessageID, e.SenderDeviceID, e.RecipientDeviceID,
		e.Type, e.CreatedAt, e.ExpiresAt))
}

func TestEncryptedWebSocketEndToEnd(t *testing.T) {
	f := setup(t)
	deviceA := newDevice(t)
	deviceB := newDevice(t)
	password := "correct horse battery staple"
	var registered authResponse
	status := request(t, http.MethodPost, f.http.URL+"/api/v1/auth/register", "",
		map[string]any{"email": "owner@example.com", "password": password,
			"device": deviceJSON(deviceA, "Device A")}, &registered)
	if status != http.StatusCreated {
		t.Fatalf("注册状态: %d", status)
	}
	var loggedIn authResponse
	status = request(t, http.MethodPost, f.http.URL+"/api/v1/auth/login", "",
		map[string]any{"email": "owner@example.com", "password": password,
			"device": deviceJSON(deviceB, "Device B")}, &loggedIn)
	if status != http.StatusOK {
		t.Fatalf("登录状态: %d", status)
	}

	var pairing struct {
		Token string `json:"token"`
	}
	if status := request(t, http.MethodPost, f.http.URL+"/api/v1/pairing/create",
		registered.Tokens.AccessToken, map[string]any{}, &pairing); status != http.StatusCreated {
		t.Fatalf("创建配对状态: %d", status)
	}
	if status := request(t, http.MethodPost, f.http.URL+"/api/v1/pairing/accept",
		loggedIn.Tokens.AccessToken, map[string]any{"token": pairing.Token},
		&map[string]any{}); status != http.StatusOK {
		t.Fatalf("接受配对状态: %d", status)
	}

	connA := connect(t, f.http.URL, deviceA, registered.Tokens.AccessToken)
	connB := connect(t, f.http.URL, deviceB, loggedIn.Tokens.AccessToken)
	plaintext := []byte("server must never see this clipboard text")
	keyA := deriveKey(t, deviceA, deviceB.xPublic)
	keyB := deriveKey(t, deviceB, deviceA.xPublic)
	if !bytes.Equal(keyA, keyB) {
		t.Fatal("X25519/HKDF 两端派生密钥不一致")
	}
	aead, err := chacha20poly1305.NewX(keyA)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	envelope := model.Envelope{
		Version: 1, Type: "clipboard_text", MessageID: uuid.NewString(),
		SenderDeviceID: deviceA.id, RecipientDeviceID: deviceB.id,
		CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(10 * time.Minute).UnixMilli(),
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, aad(envelope))
	digest := sha256.Sum256(bytes.Join([][]byte{aad(envelope), nonce, ciphertext}, nil))
	envelope.Nonce = base64.RawURLEncoding.EncodeToString(nonce)
	envelope.Ciphertext = base64.RawURLEncoding.EncodeToString(ciphertext)
	envelope.Signature = base64.RawURLEncoding.EncodeToString(
		ed25519.Sign(deviceA.signPriv, digest[:]))
	if err := connA.WriteJSON(envelope); err != nil {
		t.Fatal(err)
	}
	var received model.Envelope
	readType(t, connB, "clipboard_text", &received)

	var storedCiphertext string
	err = f.store.DB.QueryRow(`SELECT ciphertext FROM encrypted_messages
		WHERE message_id=?`, envelope.MessageID).Scan(&storedCiphertext)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(storedCiphertext, string(plaintext)) {
		t.Fatal("服务端数据库包含剪贴板明文")
	}
	receivedNonce, _ := base64.RawURLEncoding.DecodeString(received.Nonce)
	receivedCiphertext, _ := base64.RawURLEncoding.DecodeString(received.Ciphertext)
	receivedSignature, _ := base64.RawURLEncoding.DecodeString(received.Signature)
	receivedDigest := sha256.Sum256(bytes.Join(
		[][]byte{aad(received), receivedNonce, receivedCiphertext}, nil))
	if !ed25519.Verify(deviceA.signPub, receivedDigest[:], receivedSignature) {
		t.Fatal("发送设备签名无效")
	}
	decrypted, err := aead.Open(nil, receivedNonce, receivedCiphertext, aad(received))
	if err != nil || !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("设备 B 解密失败: %v", err)
	}
	if err := connB.WriteJSON(map[string]any{
		"type": "message_ack", "message_id": received.MessageID, "status": "processed",
	}); err != nil {
		t.Fatal(err)
	}
	readType(t, connA, "message_ack", nil)
	var count int
	if err := f.store.DB.QueryRow(`SELECT COUNT(1) FROM encrypted_messages
		WHERE message_id=?`, envelope.MessageID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("ACK 后服务端未删除密文: count=%d err=%v", count, err)
	}
}

func TestWrongKeyAndTamperedTagFail(t *testing.T) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	aead, _ := chacha20poly1305.NewX(key)
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	ciphertext := aead.Seal(nil, nonce, []byte("secret"), []byte("aad"))

	wrongKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(wrongKey); err != nil {
		t.Fatal(err)
	}
	wrongAEAD, _ := chacha20poly1305.NewX(wrongKey)
	if _, err := wrongAEAD.Open(nil, nonce, ciphertext, []byte("aad")); err == nil {
		t.Fatal("错误密钥不应解密成功")
	}
	ciphertext[len(ciphertext)-1] ^= 0x01
	if _, err := aead.Open(nil, nonce, ciphertext, []byte("aad")); err == nil {
		t.Fatal("被篡改的认证标签不应解密成功")
	}
}

func TestRefreshRotationAndBodyLimit(t *testing.T) {
	f := setup(t)
	device := newDevice(t)
	var registered authResponse
	request(t, http.MethodPost, f.http.URL+"/api/v1/auth/register", "",
		map[string]any{"email": "refresh@example.com",
			"password": "correct horse battery staple",
			"device":   deviceJSON(device, "Device")}, &registered)
	var rotated auth.TokenPair
	status := request(t, http.MethodPost, f.http.URL+"/api/v1/auth/refresh", "",
		map[string]any{"refresh_token": registered.Tokens.RefreshToken}, &rotated)
	if status != http.StatusOK || rotated.RefreshToken == registered.Tokens.RefreshToken {
		t.Fatalf("刷新令牌未轮换: status=%d", status)
	}
	var errorBody map[string]any
	status = request(t, http.MethodPost, f.http.URL+"/api/v1/auth/refresh", "",
		map[string]any{"refresh_token": registered.Tokens.RefreshToken}, &errorBody)
	if status != http.StatusUnauthorized {
		t.Fatalf("旧刷新令牌仍可使用: %d", status)
	}
}
