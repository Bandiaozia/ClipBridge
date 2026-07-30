package com.clipbridge.app.crypto

import com.clipbridge.app.domain.model.Device
import com.clipbridge.app.domain.model.EncryptedEnvelope
import com.goterl.lazysodium.LazySodiumAndroid
import com.goterl.lazysodium.SodiumAndroid
import org.json.JSONObject
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.UUID
import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

class CryptoEngine(private val identity: Identity) {
    private val sodium = LazySodiumAndroid(SodiumAndroid())

    init {
        check(sodium.sodiumInit() >= 0) { "libsodium 初始化失败" }
    }

    fun encrypt(
        text: String,
        recipient: Device,
        ttlMillis: Long = 600_000,
        offlineAllowed: Boolean = true,
    ): EncryptedEnvelope {
        val now = System.currentTimeMillis()
        val envelope = EncryptedEnvelope(
            messageId = UUID.randomUUID().toString(),
            senderDeviceId = identity.deviceId,
            recipientDeviceId = recipient.id,
            createdAt = now,
            expiresAt = now + ttlMillis.coerceIn(60_000, 3_600_000),
            nonce = ByteArray(24).also(SecureRandom()::nextBytes).base64Url(),
            ciphertext = "",
            signature = "",
            offlineAllowed = offlineAllowed,
        )
        val payload = JSONObject()
            .put("text", text)
            .put("content_sha256", sha256(text.toByteArray()).base64Url())
            .put("sensitive", SensitiveDetector.isSensitive(text))
            .toString()
            .toByteArray()
        val nonce = envelope.nonce.base64UrlDecode()
        val aad = aad(envelope)
        val key = directionKey(
            recipient.x25519PublicKey.base64UrlDecode(),
            envelope.senderDeviceId,
            envelope.recipientDeviceId,
        )
        val ciphertext = ByteArray(payload.size + 16)
        val ciphertextLength = longArrayOf(0)
        check(
            sodium.cryptoAeadXChaCha20Poly1305IetfEncrypt(
                ciphertext,
                ciphertextLength,
                payload,
                payload.size.toLong(),
                aad,
                aad.size.toLong(),
                null,
                nonce,
                key,
            ),
        ) { "XChaCha20-Poly1305 加密失败" }
        key.fill(0)
        val exactCiphertext = ciphertext.copyOf(ciphertextLength[0].toInt())
        val digest = sha256(aad + nonce + exactCiphertext)
        val signature = ByteArray(64)
        check(
            sodium.cryptoSignDetached(
                signature,
                digest,
                digest.size.toLong(),
                identity.signPrivate,
            ),
        ) { "Ed25519 签名失败" }
        return envelope.copy(
            ciphertext = exactCiphertext.base64Url(),
            signature = signature.base64Url(),
        )
    }

    fun decrypt(envelope: EncryptedEnvelope, sender: Device): String {
        require(envelope.recipientDeviceId == identity.deviceId) { "消息收件设备不匹配" }
        require(envelope.expiresAt > System.currentTimeMillis()) { "消息已过期" }
        val nonce = envelope.nonce.base64UrlDecode()
        val ciphertext = envelope.ciphertext.base64UrlDecode()
        val signature = envelope.signature.base64UrlDecode()
        require(nonce.size == 24 && signature.size == 64 && ciphertext.size >= 16) {
            "密文格式无效"
        }
        val aad = aad(envelope)
        val digest = sha256(aad + nonce + ciphertext)
        require(
            sodium.cryptoSignVerifyDetached(
                signature,
                digest,
                digest.size,
                sender.ed25519PublicKey.base64UrlDecode(),
            ),
        ) { "发送设备签名验证失败" }
        val key = directionKey(
            sender.x25519PublicKey.base64UrlDecode(),
            envelope.senderDeviceId,
            envelope.recipientDeviceId,
        )
        val plain = ByteArray(ciphertext.size - 16)
        val plainLength = longArrayOf(0)
        val valid = sodium.cryptoAeadXChaCha20Poly1305IetfDecrypt(
            plain,
            plainLength,
            null,
            ciphertext,
            ciphertext.size.toLong(),
            aad,
            aad.size.toLong(),
            nonce,
            key,
        )
        key.fill(0)
        require(valid) { "密文认证失败" }
        val json = JSONObject(
            plain.copyOf(plainLength[0].toInt()).toString(StandardCharsets.UTF_8),
        )
        val text = json.getString("text")
        require(
            MessageDigest.isEqual(
                json.getString("content_sha256").base64UrlDecode(),
                sha256(text.toByteArray()),
            ),
        ) { "内容摘要验证失败" }
        return text
    }

    private fun directionKey(
        peerPublic: ByteArray,
        senderId: String,
        recipientId: String,
    ): ByteArray {
        require(peerPublic.size == 32) { "X25519 公钥长度无效" }
        val shared = ByteArray(32)
        check(sodium.cryptoScalarMult(shared, identity.xPrivate, peerPublic)) {
            "X25519 密钥协商失败"
        }
        val ordered = listOf(senderId, recipientId).sorted()
        val salt = sha256(
            "ClipBridge pairing v1".toByteArray() +
                ordered[0].toByteArray() +
                ordered[1].toByteArray(),
        )
        val prk = hmac(salt, shared)
        shared.fill(0)
        return hmac(
            prk,
            "ClipBridge message v1".toByteArray() +
                senderId.toByteArray() +
                recipientId.toByteArray() +
                byteArrayOf(1),
        )
    }

    private fun aad(envelope: EncryptedEnvelope): ByteArray =
        listOf(
            envelope.version.toString(),
            envelope.messageId,
            envelope.senderDeviceId,
            envelope.recipientDeviceId,
            envelope.type,
            envelope.createdAt.toString(),
            envelope.expiresAt.toString(),
        ).joinToString("\n").toByteArray()

    private fun hmac(key: ByteArray, data: ByteArray): ByteArray =
        Mac.getInstance("HmacSHA256").run {
            init(SecretKeySpec(key, "HmacSHA256"))
            doFinal(data)
        }

    companion object {
        fun sha256(data: ByteArray): ByteArray =
            MessageDigest.getInstance("SHA-256").digest(data)
    }
}

object SensitiveDetector {
    private val rules = listOf(
        Regex("""(?:^|\D)\d{6}(?:\D|$)""") to "六位验证码",
        Regex("""(?:\d[ -]?){13,19}""") to "疑似银行卡号",
        Regex("""-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----""") to "私钥",
        Regex("""(?i)\bBearer\s+[A-Za-z0-9._~+/\-=]{16,}""") to "Bearer Token",
        Regex("""\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b""") to "JWT",
        Regex("""(?i)\b(?:api[_-]?key|password|passwd|pwd)\s*[:=]\s*\S{6,}""") to
            "密钥或密码",
        Regex("""(?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis)://\S+""") to
            "数据库连接串",
    )

    fun reasons(text: String): List<String> =
        rules.filter { it.first.containsMatchIn(text) }.map { it.second }

    fun isSensitive(text: String): Boolean = reasons(text).isNotEmpty()
}
