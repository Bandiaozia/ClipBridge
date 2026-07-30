package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/clipbridge/clipbridge/relay-server/internal/model"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var ErrInvalidToken = errors.New("令牌无效或已过期")

type jwtClaims struct {
	DeviceID string `json:"device_id"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	db         *sql.DB
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

func NewTokenManager(db *sql.DB, secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{
		db: db, secret: []byte(secret), accessTTL: accessTTL,
		refreshTTL: refreshTTL, now: time.Now,
	}
}

func (m *TokenManager) issueAccess(userID, deviceID string) (string, error) {
	now := m.now()
	claims := jwtClaims{
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "clipbridge-relay", Subject: userID,
			Audience:  jwt.ClaimStrings{"clipbridge-client"},
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *TokenManager) Issue(ctx context.Context, userID, deviceID string) (TokenPair, error) {
	access, err := m.issueAccess(userID, deviceID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("签发访问令牌: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return TokenPair{}, fmt.Errorf("生成刷新令牌: %w", err)
	}
	refresh := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(refresh))
	now := m.now()
	if _, err := m.db.ExecContext(ctx, `INSERT INTO refresh_tokens
		(id, token_hash, user_id, device_id, created_at, expires_at)
		VALUES(?, ?, ?, ?, ?, ?)`, uuid.NewString(), digest[:], userID, deviceID,
		now.UnixMilli(), now.Add(m.refreshTTL).UnixMilli()); err != nil {
		return TokenPair{}, fmt.Errorf("保存刷新令牌: %w", err)
	}
	return TokenPair{
		AccessToken: access, RefreshToken: refresh, TokenType: "Bearer",
		ExpiresIn: int64(m.accessTTL.Seconds()),
	}, nil
}

func (m *TokenManager) ParseAccess(tokenText string) (model.Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenText, &jwtClaims{},
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, ErrInvalidToken
			}
			return m.secret, nil
		},
		jwt.WithAudience("clipbridge-client"),
		jwt.WithIssuer("clipbridge-relay"),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{"HS256"}),
	)
	if err != nil || !parsed.Valid {
		return model.Claims{}, ErrInvalidToken
	}
	claims, ok := parsed.Claims.(*jwtClaims)
	if !ok || claims.Subject == "" || claims.DeviceID == "" {
		return model.Claims{}, ErrInvalidToken
	}
	return model.Claims{UserID: claims.Subject, DeviceID: claims.DeviceID}, nil
}

func (m *TokenManager) Rotate(ctx context.Context, refresh string) (TokenPair, error) {
	if len(refresh) < 40 || len(refresh) > 128 {
		return TokenPair{}, ErrInvalidToken
	}
	digest := sha256.Sum256([]byte(refresh))
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return TokenPair{}, fmt.Errorf("开始刷新事务: %w", err)
	}
	defer tx.Rollback()
	var id, userID, deviceID string
	err = tx.QueryRowContext(ctx, `SELECT rt.id, rt.user_id, rt.device_id
		FROM refresh_tokens rt JOIN devices d ON d.id = rt.device_id
		WHERE rt.token_hash = ? AND rt.revoked_at IS NULL AND rt.expires_at > ?
		  AND d.revoked_at IS NULL`, digest[:], m.now().UnixMilli()).
		Scan(&id, &userID, &deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return TokenPair{}, ErrInvalidToken
	}
	if err != nil {
		return TokenPair{}, fmt.Errorf("查询刷新令牌: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return TokenPair{}, fmt.Errorf("生成刷新令牌: %w", err)
	}
	newRefresh := base64.RawURLEncoding.EncodeToString(raw)
	newDigest := sha256.Sum256([]byte(newRefresh))
	newID := uuid.NewString()
	now := m.now()
	if _, err = tx.ExecContext(ctx, `INSERT INTO refresh_tokens
		(id, token_hash, user_id, device_id, created_at, expires_at)
		VALUES(?, ?, ?, ?, ?, ?)`, newID, newDigest[:], userID, deviceID,
		now.UnixMilli(), now.Add(m.refreshTTL).UnixMilli()); err != nil {
		return TokenPair{}, fmt.Errorf("保存新刷新令牌: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE refresh_tokens
		SET revoked_at = ?, replaced_by = ? WHERE id = ? AND revoked_at IS NULL`,
		now.UnixMilli(), newID, id)
	if err != nil {
		return TokenPair{}, fmt.Errorf("撤销旧刷新令牌: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return TokenPair{}, ErrInvalidToken
	}
	if err := tx.Commit(); err != nil {
		return TokenPair{}, fmt.Errorf("提交刷新事务: %w", err)
	}
	access, err := m.issueAccess(userID, deviceID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("签发访问令牌: %w", err)
	}
	return TokenPair{AccessToken: access, RefreshToken: newRefresh,
		TokenType: "Bearer", ExpiresIn: int64(m.accessTTL.Seconds())}, nil
}

func (m *TokenManager) RevokeRefresh(ctx context.Context, refresh string) error {
	if refresh == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(refresh))
	_, err := m.db.ExecContext(ctx, `UPDATE refresh_tokens
		SET revoked_at = COALESCE(revoked_at, ?) WHERE token_hash = ?`,
		m.now().UnixMilli(), digest[:])
	return err
}
