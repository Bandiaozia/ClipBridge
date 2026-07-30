package model

type Device struct {
	ID               string `json:"id"`
	UserID           string `json:"-"`
	Name             string `json:"name"`
	Platform         string `json:"platform"`
	X25519PublicKey  string `json:"x25519_public_key"`
	Ed25519PublicKey string `json:"ed25519_public_key"`
	CreatedAt        int64  `json:"created_at"`
	LastSeenAt       *int64 `json:"last_seen_at,omitempty"`
	RevokedAt        *int64 `json:"revoked_at,omitempty"`
	Online           bool   `json:"online"`
}

type Claims struct {
	UserID   string
	DeviceID string
}

type Envelope struct {
	Version           int    `json:"version"`
	Type              string `json:"type"`
	MessageID         string `json:"message_id"`
	SenderDeviceID    string `json:"sender_device_id"`
	RecipientDeviceID string `json:"recipient_device_id"`
	CreatedAt         int64  `json:"created_at"`
	ExpiresAt         int64  `json:"expires_at"`
	Nonce             string `json:"nonce"`
	Ciphertext        string `json:"ciphertext"`
	Signature         string `json:"signature"`
	Sequence          int64  `json:"sequence,omitempty"`
	OfflineAllowed    *bool  `json:"offline_allowed,omitempty"`
}
