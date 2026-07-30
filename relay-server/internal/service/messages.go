package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/clipbridge/clipbridge/relay-server/internal/model"
)

func (s *Store) QueueMessage(ctx context.Context, userID string, e model.Envelope) (int64, bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	var valid int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM devices sender
		JOIN devices recipient ON recipient.user_id=sender.user_id
		WHERE sender.id=? AND recipient.id=? AND sender.user_id=?
		  AND sender.revoked_at IS NULL AND recipient.revoked_at IS NULL`,
		e.SenderDeviceID, e.RecipientDeviceID, userID).Scan(&valid)
	if err != nil || valid != 1 {
		return 0, false, ErrForbidden
	}
	var count int
	var bytes int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1), COALESCE(SUM(length(ciphertext)),0)
		FROM encrypted_messages WHERE user_id=?`, userID).Scan(&count, &bytes); err != nil {
		return 0, false, fmt.Errorf("检查离线配额: %w", err)
	}
	if count >= s.MaxQueuedMessages || bytes+int64(len(e.Ciphertext)) > s.MaxQueuedBytes {
		return 0, false, ErrQuotaExceeded
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO encrypted_messages
		(message_id,user_id,sender_device_id,recipient_device_id,message_type,
		 protocol_version,created_at,expires_at,nonce,ciphertext,signature,stored_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(recipient_device_id,message_id) DO NOTHING`,
		e.MessageID, userID, e.SenderDeviceID, e.RecipientDeviceID, e.Type,
		e.Version, e.CreatedAt, e.ExpiresAt, e.Nonce, e.Ciphertext, e.Signature,
		s.Now().UnixMilli())
	if err != nil {
		return 0, false, fmt.Errorf("暂存密文: %w", err)
	}
	affected, _ := result.RowsAffected()
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT sequence FROM encrypted_messages
		WHERE recipient_device_id=? AND message_id=?`,
		e.RecipientDeviceID, e.MessageID).Scan(&sequence); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return sequence, affected == 0, nil
}

func (s *Store) PendingMessages(ctx context.Context, userID, recipientID string) ([]model.Envelope, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT protocol_version,message_type,message_id,
		sender_device_id,recipient_device_id,created_at,expires_at,nonce,ciphertext,
		signature,sequence FROM encrypted_messages
		WHERE user_id=? AND recipient_device_id=? AND expires_at>?
		ORDER BY sequence`, userID, recipientID, s.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.Envelope, 0)
	for rows.Next() {
		var e model.Envelope
		if err := rows.Scan(&e.Version, &e.Type, &e.MessageID, &e.SenderDeviceID,
			&e.RecipientDeviceID, &e.CreatedAt, &e.ExpiresAt, &e.Nonce,
			&e.Ciphertext, &e.Signature, &e.Sequence); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func (s *Store) AckMessage(ctx context.Context, userID, recipientID, messageID string) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var senderID string
	err = tx.QueryRowContext(ctx, `SELECT sender_device_id FROM encrypted_messages
		WHERE user_id=? AND recipient_device_id=? AND message_id=?`,
		userID, recipientID, messageID).Scan(&senderID)
	if errors.Is(err, sql.ErrNoRows) {
		// ACK 自身也必须幂等；记录已被删时返回空发送方。
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM encrypted_messages
		WHERE user_id=? AND recipient_device_id=? AND message_id=?`,
		userID, recipientID, messageID); err != nil {
		return "", err
	}
	return senderID, tx.Commit()
}

func (s *Store) Cleanup(ctx context.Context) (int64, error) {
	now := s.Now().UnixMilli()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM encrypted_messages WHERE expires_at<=?`, now)
	if err != nil {
		return 0, err
	}
	deleted, _ := result.RowsAffected()
	if _, err = tx.ExecContext(ctx, `DELETE FROM pairing_tokens
		WHERE expires_at<=? OR used_at IS NOT NULL OR rejected_at IS NOT NULL`, now); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM refresh_tokens
		WHERE expires_at<=? OR (revoked_at IS NOT NULL AND revoked_at<?)`,
		now, time.UnixMilli(now).Add(-30*24*time.Hour).UnixMilli()); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM audit_logs WHERE created_at<?`,
		time.UnixMilli(now).Add(-90*24*time.Hour).UnixMilli()); err != nil {
		return 0, err
	}
	return deleted, tx.Commit()
}
