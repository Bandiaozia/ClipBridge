package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/clipbridge/clipbridge/relay-server/internal/model"
	"github.com/google/uuid"
)

var (
	ErrNotFound       = errors.New("资源不存在")
	ErrConflict       = errors.New("资源已存在")
	ErrForbidden      = errors.New("无权执行此操作")
	ErrPairingInvalid = errors.New("配对令牌无效或已过期")
	ErrQuotaExceeded  = errors.New("离线消息配额已满")
	ErrRecipientOff   = errors.New("接收设备离线")
)

type Store struct {
	DB                *sql.DB
	MaxQueuedMessages int
	MaxQueuedBytes    int64
	PairingTTL        time.Duration
	Now               func() time.Time
}

type NewDevice struct {
	ID, Name, Platform, X25519PublicKey, Ed25519PublicKey string
}

func NewStore(db *sql.DB, maxMessages int, maxBytes int64, pairingTTL time.Duration) *Store {
	return &Store{DB: db, MaxQueuedMessages: maxMessages, MaxQueuedBytes: maxBytes,
		PairingTTL: pairingTTL, Now: time.Now}
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string, d NewDevice) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	userID := uuid.NewString()
	now := s.Now().UnixMilli()
	if _, err = tx.ExecContext(ctx, `INSERT INTO users
		(id, email, password_hash, created_at, updated_at) VALUES(?, ?, ?, ?, ?)`,
		userID, strings.ToLower(strings.TrimSpace(email)), passwordHash, now, now); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", ErrConflict
		}
		return "", fmt.Errorf("创建用户: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO devices
		(id, user_id, name, platform, x25519_public_key, ed25519_public_key, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, d.ID, userID, d.Name, d.Platform,
		d.X25519PublicKey, d.Ed25519PublicKey, now); err != nil {
		return "", fmt.Errorf("创建设备: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return userID, nil
}

func (s *Store) Credentials(ctx context.Context, email string) (string, string, error) {
	var id, hash string
	err := s.DB.QueryRowContext(ctx, `SELECT id, password_hash FROM users WHERE email = ?`,
		strings.ToLower(strings.TrimSpace(email))).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return id, hash, err
}

func (s *Store) UpsertDevice(ctx context.Context, userID string, d NewDevice) error {
	now := s.Now().UnixMilli()
	result, err := s.DB.ExecContext(ctx, `UPDATE devices SET name=?, platform=?,
		x25519_public_key=?, ed25519_public_key=?, last_seen_at=?
		WHERE id=? AND user_id=? AND revoked_at IS NULL`, d.Name, d.Platform,
		d.X25519PublicKey, d.Ed25519PublicKey, now, d.ID, userID)
	if err != nil {
		return fmt.Errorf("更新设备: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 1 {
		return nil
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO devices
		(id,user_id,name,platform,x25519_public_key,ed25519_public_key,created_at,last_seen_at)
		VALUES(?,?,?,?,?,?,?,?)`, d.ID, userID, d.Name, d.Platform,
		d.X25519PublicKey, d.Ed25519PublicKey, now, now)
	if err != nil {
		return fmt.Errorf("添加设备: %w", err)
	}
	return nil
}

func (s *Store) DeviceActive(ctx context.Context, userID, deviceID string) bool {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM devices
		WHERE id=? AND user_id=? AND revoked_at IS NULL`, deviceID, userID).Scan(&count)
	return err == nil && count == 1
}

func (s *Store) ListDevices(ctx context.Context, userID string) ([]model.Device, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,user_id,name,platform,
		x25519_public_key,ed25519_public_key,created_at,last_seen_at,revoked_at
		FROM devices WHERE user_id=? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	devices := make([]model.Device, 0)
	for rows.Next() {
		var d model.Device
		var last, revoked sql.NullInt64
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Platform, &d.X25519PublicKey,
			&d.Ed25519PublicKey, &d.CreatedAt, &last, &revoked); err != nil {
			return nil, err
		}
		if last.Valid {
			d.LastSeenAt = &last.Int64
		}
		if revoked.Valid {
			d.RevokedAt = &revoked.Int64
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *Store) RevokeDevice(ctx context.Context, userID, deviceID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `UPDATE devices SET revoked_at=?
		WHERE id=? AND user_id=? AND revoked_at IS NULL`, now, deviceID, userID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return ErrNotFound
	}
	for _, query := range []string{
		`UPDATE refresh_tokens SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`,
		`DELETE FROM encrypted_messages WHERE sender_device_id=? OR recipient_device_id=?`,
		`DELETE FROM pairing_tokens WHERE initiator_device_id=? AND used_at IS NULL`,
	} {
		var execErr error
		switch strings.Count(query, "?") {
		case 2:
			if strings.Contains(query, "refresh_tokens") {
				_, execErr = tx.ExecContext(ctx, query, now, deviceID)
			} else {
				_, execErr = tx.ExecContext(ctx, query, deviceID, deviceID)
			}
		default:
			_, execErr = tx.ExecContext(ctx, query, deviceID)
		}
		if execErr != nil {
			return execErr
		}
	}
	return tx.Commit()
}

func (s *Store) CreatePairing(ctx context.Context, userID, deviceID string) (string, int64, error) {
	if !s.DeviceActive(ctx, userID, deviceID) {
		return "", 0, ErrForbidden
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", 0, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	now := s.Now()
	expires := now.Add(s.PairingTTL).UnixMilli()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO pairing_tokens
		(id,token_hash,user_id,initiator_device_id,created_at,expires_at)
		VALUES(?,?,?,?,?,?)`, uuid.NewString(), digest[:], userID, deviceID,
		now.UnixMilli(), expires)
	return token, expires, err
}

func (s *Store) ConsumePairing(ctx context.Context, userID, acceptorID, token string, reject bool) (model.Device, error) {
	digest := sha256.Sum256([]byte(token))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return model.Device{}, err
	}
	defer tx.Rollback()
	var pairID, initiatorID string
	err = tx.QueryRowContext(ctx, `SELECT id,initiator_device_id FROM pairing_tokens
		WHERE token_hash=? AND user_id=? AND expires_at>? AND used_at IS NULL
		  AND rejected_at IS NULL`, digest[:], userID, s.Now().UnixMilli()).
		Scan(&pairID, &initiatorID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Device{}, ErrPairingInvalid
	}
	if err != nil || initiatorID == acceptorID {
		return model.Device{}, ErrPairingInvalid
	}
	column := "used_at"
	if reject {
		column = "rejected_at"
	}
	query := `UPDATE pairing_tokens SET ` + column + `=?, acceptor_device_id=?
		WHERE id=? AND used_at IS NULL AND rejected_at IS NULL`
	result, err := tx.ExecContext(ctx, query, s.Now().UnixMilli(), acceptorID, pairID)
	if err != nil {
		return model.Device{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return model.Device{}, ErrPairingInvalid
	}
	var d model.Device
	err = tx.QueryRowContext(ctx, `SELECT id,user_id,name,platform,x25519_public_key,
		ed25519_public_key,created_at FROM devices
		WHERE id=? AND user_id=? AND revoked_at IS NULL`, initiatorID, userID).
		Scan(&d.ID, &d.UserID, &d.Name, &d.Platform, &d.X25519PublicKey,
			&d.Ed25519PublicKey, &d.CreatedAt)
	if err != nil {
		return model.Device{}, ErrPairingInvalid
	}
	if err := tx.Commit(); err != nil {
		return model.Device{}, err
	}
	return d, nil
}

func (s *Store) DeleteAccount(ctx context.Context, userID string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id=?`, userID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ChangePassword(ctx context.Context, userID, hash string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := s.Now().UnixMilli()
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=?,updated_at=? WHERE id=?`,
		hash, now, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked_at=?
		WHERE user_id=? AND revoked_at IS NULL`, now, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Audit(ctx context.Context, userID, deviceID, event, remote, requestID string, success bool) {
	ok := 0
	if success {
		ok = 1
	}
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO audit_logs
		(user_id,device_id,event,remote_addr,request_id,success,created_at)
		VALUES(NULLIF(?,''),NULLIF(?,''),?,?,?,?,?)`,
		userID, deviceID, event, remote, requestID, ok, s.Now().UnixMilli())
}
