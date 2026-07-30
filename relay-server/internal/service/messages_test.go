package service

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clipbridge/clipbridge/relay-server/internal/auth"
	"github.com/clipbridge/clipbridge/relay-server/internal/database"
	"github.com/clipbridge/clipbridge/relay-server/internal/model"
	"github.com/google/uuid"
)

func testStore(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(ctx, "file:"+path+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := NewStore(db, 2, 4096, 5*time.Minute)
	hash, _ := auth.HashPassword("correct horse battery staple")
	deviceA := uuid.NewString()
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	userID, err := store.CreateUser(ctx, "test@example.com", hash, NewDevice{
		ID: deviceA, Name: "A", Platform: "test",
		X25519PublicKey: key, Ed25519PublicKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	deviceB := uuid.NewString()
	err = store.UpsertDevice(ctx, userID, NewDevice{
		ID: deviceB, Name: "B", Platform: "test",
		X25519PublicKey: key, Ed25519PublicKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, userID, deviceA, deviceB
}

func envelope(sender, recipient string, now time.Time) model.Envelope {
	return model.Envelope{
		Version: 1, Type: "clipboard_text", MessageID: uuid.NewString(),
		SenderDeviceID: sender, RecipientDeviceID: recipient,
		CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(10 * time.Minute).UnixMilli(),
		Nonce:      base64.RawURLEncoding.EncodeToString(make([]byte, 24)),
		Ciphertext: base64.RawURLEncoding.EncodeToString([]byte("encrypted-only")),
		Signature:  base64.RawURLEncoding.EncodeToString(make([]byte, 64)),
	}
}

func TestQueueDeduplicateAckAndExpiry(t *testing.T) {
	store, userID, sender, recipient := testStore(t)
	ctx := context.Background()
	now := time.Now()
	message := envelope(sender, recipient, now)
	sequence, duplicate, err := store.QueueMessage(ctx, userID, message)
	if err != nil || duplicate || sequence == 0 {
		t.Fatalf("首次入队失败: sequence=%d duplicate=%v err=%v", sequence, duplicate, err)
	}
	sameSequence, duplicate, err := store.QueueMessage(ctx, userID, message)
	if err != nil || !duplicate || sameSequence != sequence {
		t.Fatalf("去重失败: sequence=%d duplicate=%v err=%v", sameSequence, duplicate, err)
	}
	pending, err := store.PendingMessages(ctx, userID, recipient)
	if err != nil || len(pending) != 1 {
		t.Fatalf("离线投递读取失败: count=%d err=%v", len(pending), err)
	}
	ackSender, err := store.AckMessage(ctx, userID, recipient, message.MessageID)
	if err != nil || ackSender != sender {
		t.Fatalf("ACK 失败: sender=%s err=%v", ackSender, err)
	}
	pending, _ = store.PendingMessages(ctx, userID, recipient)
	if len(pending) != 0 {
		t.Fatal("ACK 后密文仍存在")
	}

	expired := envelope(sender, recipient, now)
	store.Now = func() time.Time { return now }
	if _, _, err := store.QueueMessage(ctx, userID, expired); err != nil {
		t.Fatal(err)
	}
	store.Now = func() time.Time { return now.Add(11 * time.Minute) }
	deleted, err := store.Cleanup(ctx)
	if err != nil || deleted != 1 {
		t.Fatalf("过期清理失败: deleted=%d err=%v", deleted, err)
	}
}

func TestQuotaAndDeviceRevocation(t *testing.T) {
	store, userID, sender, recipient := testStore(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, _, err := store.QueueMessage(ctx, userID,
			envelope(sender, recipient, time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := store.QueueMessage(ctx, userID,
		envelope(sender, recipient, time.Now())); err != ErrQuotaExceeded {
		t.Fatalf("预期配额错误，得到 %v", err)
	}
	if err := store.RevokeDevice(ctx, userID, recipient); err != nil {
		t.Fatal(err)
	}
	if store.DeviceActive(ctx, userID, recipient) {
		t.Fatal("撤销设备仍为活跃状态")
	}
	var count int
	if err := store.DB.QueryRow(`SELECT COUNT(1) FROM encrypted_messages
		WHERE recipient_device_id=?`, recipient).Scan(&count); err != nil || count != 0 {
		t.Fatalf("撤销后暂存消息未删除: count=%d err=%v", count, err)
	}
}

func TestPairingTokenSingleUse(t *testing.T) {
	store, userID, initiator, acceptor := testStore(t)
	ctx := context.Background()
	token, _, err := store.CreatePairing(ctx, userID, initiator)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(token, initiator) {
		t.Fatal("配对令牌泄露设备信息")
	}
	device, err := store.ConsumePairing(ctx, userID, acceptor, token, false)
	if err != nil || device.ID != initiator {
		t.Fatalf("接受配对失败: device=%s err=%v", device.ID, err)
	}
	if _, err := store.ConsumePairing(ctx, userID, acceptor, token, false); err != ErrPairingInvalid {
		t.Fatalf("一次性令牌被重复使用: %v", err)
	}
}
