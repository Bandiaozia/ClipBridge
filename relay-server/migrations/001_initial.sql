PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 80),
    platform TEXT NOT NULL CHECK(platform IN ('windows', 'linux', 'android', 'test')),
    x25519_public_key TEXT NOT NULL CHECK(length(x25519_public_key) BETWEEN 40 AND 64),
    ed25519_public_key TEXT NOT NULL CHECK(length(ed25519_public_key) BETWEEN 40 AND 64),
    created_at INTEGER NOT NULL,
    last_seen_at INTEGER,
    revoked_at INTEGER,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE(user_id, id)
);
CREATE INDEX IF NOT EXISTS idx_devices_user_active
    ON devices(user_id, revoked_at);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id TEXT PRIMARY KEY,
    token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash) = 32),
    user_id TEXT NOT NULL,
    device_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER,
    replaced_by TEXT,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY(device_id) REFERENCES devices(id) ON DELETE CASCADE,
    FOREIGN KEY(replaced_by) REFERENCES refresh_tokens(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_refresh_lookup
    ON refresh_tokens(token_hash, expires_at, revoked_at);
CREATE INDEX IF NOT EXISTS idx_refresh_device
    ON refresh_tokens(device_id, revoked_at);

CREATE TABLE IF NOT EXISTS pairing_tokens (
    id TEXT PRIMARY KEY,
    token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash) = 32),
    user_id TEXT NOT NULL,
    initiator_device_id TEXT NOT NULL,
    acceptor_device_id TEXT,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER,
    rejected_at INTEGER,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY(initiator_device_id) REFERENCES devices(id) ON DELETE CASCADE,
    FOREIGN KEY(acceptor_device_id) REFERENCES devices(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_pairing_lookup
    ON pairing_tokens(token_hash, expires_at, used_at, rejected_at);

CREATE TABLE IF NOT EXISTS encrypted_messages (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    sender_device_id TEXT NOT NULL,
    recipient_device_id TEXT NOT NULL,
    message_type TEXT NOT NULL CHECK(message_type = 'clipboard_text'),
    protocol_version INTEGER NOT NULL CHECK(protocol_version = 1),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    nonce TEXT NOT NULL CHECK(length(nonce) BETWEEN 30 AND 40),
    ciphertext TEXT NOT NULL CHECK(length(ciphertext) <= 1048576),
    signature TEXT NOT NULL CHECK(length(signature) BETWEEN 80 AND 100),
    stored_at INTEGER NOT NULL,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY(sender_device_id) REFERENCES devices(id) ON DELETE CASCADE,
    FOREIGN KEY(recipient_device_id) REFERENCES devices(id) ON DELETE CASCADE,
    UNIQUE(recipient_device_id, message_id)
);
CREATE INDEX IF NOT EXISTS idx_messages_delivery
    ON encrypted_messages(recipient_device_id, expires_at, sequence);
CREATE INDEX IF NOT EXISTS idx_messages_expiry
    ON encrypted_messages(expires_at);
CREATE INDEX IF NOT EXISTS idx_messages_user
    ON encrypted_messages(user_id);

CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT,
    device_id TEXT,
    event TEXT NOT NULL,
    remote_addr TEXT,
    request_id TEXT NOT NULL,
    success INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE SET NULL,
    FOREIGN KEY(device_id) REFERENCES devices(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_user_time
    ON audit_logs(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_created
    ON audit_logs(created_at);

