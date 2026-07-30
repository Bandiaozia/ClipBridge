package com.clipbridge.app.domain.model

data class Tokens(
    val accessToken: String,
    val refreshToken: String,
    val expiresAt: Long,
)

data class Device(
    val id: String,
    val name: String,
    val platform: String,
    val x25519PublicKey: String,
    val ed25519PublicKey: String,
    val online: Boolean = false,
    val revoked: Boolean = false,
)

data class EncryptedEnvelope(
    val version: Int = 1,
    val type: String = "clipboard_text",
    val messageId: String,
    val senderDeviceId: String,
    val recipientDeviceId: String,
    val createdAt: Long,
    val expiresAt: Long,
    val nonce: String,
    val ciphertext: String,
    val signature: String,
    val offlineAllowed: Boolean = true,
)

