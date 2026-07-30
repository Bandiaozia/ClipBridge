CREATE INDEX IF NOT EXISTS idx_pairing_user_device
    ON pairing_tokens(user_id, initiator_device_id, expires_at);
CREATE INDEX IF NOT EXISTS idx_messages_sender
    ON encrypted_messages(sender_device_id, stored_at);

