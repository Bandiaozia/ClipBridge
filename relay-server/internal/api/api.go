package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/clipbridge/clipbridge/relay-server/internal/auth"
	"github.com/clipbridge/clipbridge/relay-server/internal/model"
	"github.com/clipbridge/clipbridge/relay-server/internal/service"
	clipws "github.com/clipbridge/clipbridge/relay-server/internal/ws"
	"github.com/google/uuid"
)

type contextKey int

const (
	claimsKey contextKey = iota
	requestIDKey
)

type API struct {
	store        *service.Store
	tokens       *auth.TokenManager
	hub          *clipws.Hub
	ws           *clipws.Server
	db           *sql.DB
	log          *slog.Logger
	maxBodyBytes int64
	limiter      *rateLimiter
	startedAt    time.Time
}

func New(store *service.Store, tokens *auth.TokenManager, hub *clipws.Hub,
	wsServer *clipws.Server, db *sql.DB, log *slog.Logger,
	maxBodyBytes int64, ratePerMinute int) http.Handler {
	api := &API{store: store, tokens: tokens, hub: hub, ws: wsServer,
		db: db, log: log, maxBodyBytes: maxBodyBytes,
		limiter: newRateLimiter(ratePerMinute), startedAt: time.Now()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("GET /ready", api.ready)
	mux.HandleFunc("GET /stats", api.stats)
	mux.HandleFunc("GET /proxy/shadowsocks", api.proxyShadowsocks)
	mux.HandleFunc("GET /proxy/wireguard", api.proxyWireGuard)
	mux.HandleFunc("POST /api/v1/auth/register", api.register)
	mux.HandleFunc("POST /api/v1/auth/login", api.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", api.refresh)
	mux.Handle("POST /api/v1/auth/logout", api.requireAuth(http.HandlerFunc(api.logout)))
	mux.Handle("POST /api/v1/auth/change-password", api.requireAuth(http.HandlerFunc(api.changePassword)))
	mux.Handle("GET /api/v1/account", api.requireAuth(http.HandlerFunc(api.account)))
	mux.Handle("DELETE /api/v1/account", api.requireAuth(http.HandlerFunc(api.deleteAccount)))
	mux.Handle("GET /api/v1/devices", api.requireAuth(http.HandlerFunc(api.devices)))
	mux.Handle("DELETE /api/v1/devices/{deviceId}", api.requireAuth(http.HandlerFunc(api.revokeDevice)))
	mux.Handle("POST /api/v1/pairing/create", api.requireAuth(http.HandlerFunc(api.createPairing)))
	mux.Handle("POST /api/v1/pairing/accept", api.requireAuth(http.HandlerFunc(api.acceptPairing)))
	mux.Handle("POST /api/v1/pairing/reject", api.requireAuth(http.HandlerFunc(api.rejectPairing)))
	mux.Handle("/api/v1/ws", api.ws)
	return api.middleware(mux)
}

type deviceInput struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	X25519PublicKey  string `json:"x25519_public_key"`
	Ed25519PublicKey string `json:"ed25519_public_key"`
}

func (d deviceInput) validate() error {
	if uuid.Validate(d.ID) != nil || len(strings.TrimSpace(d.Name)) < 1 ||
		len(d.Name) > 80 {
		return errors.New("设备 ID 或名称无效")
	}
	switch d.Platform {
	case "windows", "linux", "android", "test":
	default:
		return errors.New("设备平台无效")
	}
	for _, key := range []string{d.X25519PublicKey, d.Ed25519PublicKey} {
		decoded, err := base64.RawURLEncoding.DecodeString(key)
		if err != nil || len(decoded) != 32 {
			return errors.New("设备公钥无效")
		}
	}
	return nil
}

func (d deviceInput) model() service.NewDevice {
	return service.NewDevice{ID: d.ID, Name: strings.TrimSpace(d.Name),
		Platform: d.Platform, X25519PublicKey: d.X25519PublicKey,
		Ed25519PublicKey: d.Ed25519PublicKey}
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string      `json:"email"`
		Password string      `json:"password"`
		Device   deviceInput `json:"device"`
	}
	if err := a.decode(w, r, &input); err != nil || !validEmail(input.Email) ||
		input.Device.validate() != nil {
		a.fail(w, r, http.StatusBadRequest, "invalid_request", "注册信息无效")
		return
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		a.fail(w, r, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	userID, err := a.store.CreateUser(r.Context(), input.Email, hash, input.Device.model())
	if err != nil {
		if errors.Is(err, service.ErrConflict) {
			a.fail(w, r, http.StatusConflict, "email_exists", "邮箱已注册")
		} else {
			a.fail(w, r, http.StatusInternalServerError, "internal_error", "注册失败")
		}
		a.audit(r, "", input.Device.ID, "register", false)
		return
	}
	pair, err := a.tokens.Issue(r.Context(), userID, input.Device.ID)
	if err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal_error", "签发令牌失败")
		return
	}
	a.audit(r, userID, input.Device.ID, "register", true)
	a.write(w, http.StatusCreated, map[string]any{"user_id": userID, "tokens": pair})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string      `json:"email"`
		Password string      `json:"password"`
		Device   deviceInput `json:"device"`
	}
	if err := a.decode(w, r, &input); err != nil || !validEmail(input.Email) ||
		input.Device.validate() != nil || len(input.Password) > 256 {
		a.fail(w, r, http.StatusBadRequest, "invalid_request", "登录信息无效")
		return
	}
	userID, hash, err := a.store.Credentials(r.Context(), input.Email)
	if err != nil || !auth.VerifyPassword(hash, input.Password) {
		// 统一错误避免枚举账户。
		a.fail(w, r, http.StatusUnauthorized, "invalid_credentials", "邮箱或密码错误")
		a.audit(r, "", input.Device.ID, "login", false)
		return
	}
	if err := a.store.UpsertDevice(r.Context(), userID, input.Device.model()); err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal_error", "保存设备失败")
		return
	}
	pair, err := a.tokens.Issue(r.Context(), userID, input.Device.ID)
	if err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal_error", "签发令牌失败")
		return
	}
	a.audit(r, userID, input.Device.ID, "login", true)
	a.write(w, http.StatusOK, map[string]any{"user_id": userID, "tokens": pair})
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}
	if a.decode(w, r, &input) != nil {
		a.fail(w, r, http.StatusBadRequest, "invalid_request", "请求无效")
		return
	}
	pair, err := a.tokens.Rotate(r.Context(), input.RefreshToken)
	if err != nil {
		a.fail(w, r, http.StatusUnauthorized, "invalid_refresh_token", "刷新令牌无效")
		return
	}
	a.write(w, http.StatusOK, pair)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}
	if a.decode(w, r, &input) != nil {
		a.fail(w, r, http.StatusBadRequest, "invalid_request", "请求无效")
		return
	}
	if err := a.tokens.RevokeRefresh(r.Context(), input.RefreshToken); err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal_error", "注销失败")
		return
	}
	claims := claimsFrom(r)
	a.audit(r, claims.UserID, claims.DeviceID, "logout", true)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if a.decode(w, r, &input) != nil {
		a.fail(w, r, http.StatusBadRequest, "invalid_request", "请求无效")
		return
	}
	claims := claimsFrom(r)
	var existing string
	if err := a.db.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=?`,
		claims.UserID).Scan(&existing); err != nil ||
		!auth.VerifyPassword(existing, input.CurrentPassword) {
		a.fail(w, r, http.StatusUnauthorized, "invalid_credentials", "当前密码错误")
		return
	}
	hash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		a.fail(w, r, http.StatusBadRequest, "weak_password", err.Error())
		return
	}
	if err := a.store.ChangePassword(r.Context(), claims.UserID, hash); err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal_error", "修改密码失败")
		return
	}
	a.audit(r, claims.UserID, claims.DeviceID, "password_changed", true)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) account(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	var email string
	var created int64
	err := a.db.QueryRowContext(r.Context(), `SELECT email,created_at FROM users WHERE id=?`,
		claims.UserID).Scan(&email, &created)
	if err != nil {
		a.fail(w, r, http.StatusNotFound, "not_found", "账户不存在")
		return
	}
	a.write(w, http.StatusOK, map[string]any{
		"id": claims.UserID, "email": email, "created_at": created,
	})
}

func (a *API) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
	}
	if a.decode(w, r, &input) != nil {
		a.fail(w, r, http.StatusBadRequest, "invalid_request", "请求无效")
		return
	}
	claims := claimsFrom(r)
	var hash string
	if err := a.db.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=?`,
		claims.UserID).Scan(&hash); err != nil || !auth.VerifyPassword(hash, input.Password) {
		a.fail(w, r, http.StatusUnauthorized, "invalid_credentials", "密码错误")
		return
	}
	if err := a.store.DeleteAccount(r.Context(), claims.UserID); err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal_error", "删除账户失败")
		return
	}
	a.hub.RevokeUser(claims.UserID)
	a.log.Info("security_event", "event", "account_deleted", "user_id", claims.UserID,
		"request_id", requestID(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) devices(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	devices, err := a.store.ListDevices(r.Context(), claims.UserID)
	if err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal_error", "读取设备失败")
		return
	}
	ids := make([]string, 0, len(devices))
	for _, d := range devices {
		ids = append(ids, d.ID)
	}
	online := a.hub.MarkOnline(claims.UserID, ids)
	for i := range devices {
		devices[i].Online = online[devices[i].ID]
	}
	a.write(w, http.StatusOK, map[string]any{"devices": devices})
}

func (a *API) revokeDevice(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	deviceID := r.PathValue("deviceId")
	if uuid.Validate(deviceID) != nil {
		a.fail(w, r, http.StatusBadRequest, "invalid_device_id", "设备 ID 无效")
		return
	}
	if err := a.store.RevokeDevice(r.Context(), claims.UserID, deviceID); err != nil {
		if errors.Is(err, service.ErrNotFound) {
			a.fail(w, r, http.StatusNotFound, "not_found", "设备不存在")
		} else {
			a.fail(w, r, http.StatusInternalServerError, "internal_error", "撤销设备失败")
		}
		return
	}
	a.hub.Revoke(claims.UserID, deviceID)
	a.audit(r, claims.UserID, claims.DeviceID, "device_revoked", true)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) createPairing(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	token, expires, err := a.store.CreatePairing(r.Context(), claims.UserID, claims.DeviceID)
	if err != nil {
		a.fail(w, r, http.StatusForbidden, "device_forbidden", "设备无权配对")
		return
	}
	devices, err := a.store.ListDevices(r.Context(), claims.UserID)
	if err != nil {
		a.fail(w, r, http.StatusInternalServerError, "internal_error", "读取设备失败")
		return
	}
	var current model.Device
	for _, d := range devices {
		if d.ID == claims.DeviceID {
			current = d
			break
		}
	}
	a.audit(r, claims.UserID, claims.DeviceID, "pairing_created", true)
	a.write(w, http.StatusCreated, map[string]any{
		"version": 1, "token": token, "expires_at": expires,
		"initiator_device": current,
	})
}

func (a *API) acceptPairing(w http.ResponseWriter, r *http.Request) {
	a.consumePairing(w, r, false)
}

func (a *API) rejectPairing(w http.ResponseWriter, r *http.Request) {
	a.consumePairing(w, r, true)
}

func (a *API) consumePairing(w http.ResponseWriter, r *http.Request, reject bool) {
	var input struct {
		Token string `json:"token"`
	}
	if a.decode(w, r, &input) != nil || len(input.Token) < 40 || len(input.Token) > 128 {
		a.fail(w, r, http.StatusBadRequest, "invalid_request", "配对请求无效")
		return
	}
	claims := claimsFrom(r)
	device, err := a.store.ConsumePairing(r.Context(), claims.UserID,
		claims.DeviceID, input.Token, reject)
	if err != nil {
		a.fail(w, r, http.StatusGone, "pairing_invalid", "配对令牌无效或已过期")
		return
	}
	event := "pair_accept"
	if reject {
		event = "pair_reject"
	}
	a.hub.Send(claims.UserID, device.ID, map[string]any{
		"type": event, "device_id": claims.DeviceID,
	})
	a.audit(r, claims.UserID, claims.DeviceID, event, true)
	a.write(w, http.StatusOK, map[string]any{"initiator_device": device, "rejected": reject})
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	a.write(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (a *API) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.PingContext(ctx); err != nil {
		a.write(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
		return
	}
	a.write(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (a *API) stats(w http.ResponseWriter, _ *http.Request) {
	hubStats := a.hub.Stats()
	a.write(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"users":          hubStats.Users,
		"devices":        hubStats.Devices,
		"uptime_seconds": int64(time.Since(a.startedAt).Seconds()),
	})
}

func (a *API) proxyShadowsocks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://172.17.0.1:8388", nil)
	resp, err := http.DefaultClient.Do(req)
	online := err == nil && resp.StatusCode < 500
	if resp != nil { resp.Body.Close() }
	a.write(w, http.StatusOK, map[string]any{"online": online})
}

func (a *API) proxyWireGuard(w http.ResponseWriter, r *http.Request) {
	_, err := os.Lstat("/sys/class/net/wg0")
	online := err == nil
	a.write(w, http.StatusOK, map[string]any{"online": online})
}

func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			a.fail(w, r, http.StatusUnauthorized, "unauthorized", "需要认证")
			return
		}
		claims, err := a.tokens.ParseAccess(strings.TrimPrefix(header, "Bearer "))
		if err != nil || !a.store.DeviceActive(r.Context(), claims.UserID, claims.DeviceID) {
			a.fail(w, r, http.StatusUnauthorized, "invalid_token", "访问令牌无效")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
	})
}

func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := uuid.NewString()
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey, id))
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if !a.limiter.allow(remoteIP(r.RemoteAddr)) {
			a.fail(w, r, http.StatusTooManyRequests, "rate_limited", "请求过于频繁")
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, a.maxBodyBytes)
		}
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		a.log.Info("http_request", "method", r.Method, "path", r.URL.Path,
			"status", recorder.status, "duration_ms", time.Since(started).Milliseconds(),
			"request_id", id, "remote_ip", remoteIP(r.RemoteAddr))
	})
}

func (a *API) decode(w http.ResponseWriter, r *http.Request, dst any) error {
	if contentType := r.Header.Get("Content-Type"); contentType != "" &&
		!strings.HasPrefix(contentType, "application/json") {
		return errors.New("Content-Type 必须为 application/json")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("请求只能包含一个 JSON 对象")
	}
	return nil
}

func (a *API) write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func (a *API) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	a.write(w, status, map[string]any{"error": map[string]any{
		"code": code, "message": message, "request_id": requestID(r),
	}})
}

func (a *API) audit(r *http.Request, userID, deviceID, event string, success bool) {
	a.store.Audit(r.Context(), userID, deviceID, event, remoteIP(r.RemoteAddr),
		requestID(r), success)
}

func claimsFrom(r *http.Request) model.Claims {
	claims, _ := r.Context().Value(claimsKey).(model.Claims)
	return claims
}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey).(string)
	return id
}

func validEmail(value string) bool {
	if len(value) < 3 || len(value) > 254 || strings.TrimSpace(value) != value {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("响应不支持连接接管")
	}
	return hijacker.Hijack()
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

type rateLimiter struct {
	mu       sync.Mutex
	limit    int
	requests map[string]*rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

func newRateLimiter(limit int) *rateLimiter {
	return &rateLimiter{limit: limit, requests: make(map[string]*rateWindow)}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	window := l.requests[key]
	if window == nil || now.Sub(window.start) >= time.Minute {
		l.requests[key] = &rateWindow{start: now, count: 1}
		return true
	}
	window.count++
	if len(l.requests) > 10000 {
		for k, value := range l.requests {
			if now.Sub(value.start) > 2*time.Minute {
				delete(l.requests, k)
			}
		}
	}
	return window.count <= l.limit
}

func ErrorText(err error) string {
	return fmt.Sprintf("%v", err)
}
