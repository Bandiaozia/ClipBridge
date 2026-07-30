package com.clipbridge.app.crypto

import android.content.Context
import android.os.Build
import android.util.Base64
import com.goterl.lazysodium.LazySodiumAndroid
import com.goterl.lazysodium.SodiumAndroid
import org.json.JSONObject
import java.util.UUID

data class Identity(
    val deviceId: String,
    val name: String,
    val xPublic: ByteArray,
    val xPrivate: ByteArray,
    val signPublic: ByteArray,
    val signPrivate: ByteArray,
) {
    fun xPublicBase64(): String = xPublic.base64Url()
    fun signPublicBase64(): String = signPublic.base64Url()
}

class DeviceIdentity(
    private val context: Context,
    private val secure: SecurePreferences,
) {
    private val sodium = LazySodiumAndroid(SodiumAndroid())

    fun loadOrCreate(): Identity {
        secure.get("identity")?.let { encoded ->
            runCatching { return decode(encoded) }
        }
        check(sodium.sodiumInit() >= 0) { "libsodium 初始化失败" }
        val xPair = sodium.cryptoKxKeypair()
        val signPair = sodium.cryptoSignKeypair()
        val identity = Identity(
            deviceId = UUID.randomUUID().toString(),
            name = "${Build.MANUFACTURER} ${Build.MODEL}".trim().take(80),
            xPublic = xPair.publicKey.asBytes,
            xPrivate = xPair.secretKey.asBytes,
            signPublic = signPair.publicKey.asBytes,
            signPrivate = signPair.secretKey.asBytes,
        )
        secure.put("identity", encode(identity))
        return identity
    }

    private fun encode(identity: Identity) = JSONObject()
        .put("device_id", identity.deviceId)
        .put("name", identity.name)
        .put("x_public", identity.xPublic.base64Url())
        .put("x_private", identity.xPrivate.base64Url())
        .put("sign_public", identity.signPublic.base64Url())
        .put("sign_private", identity.signPrivate.base64Url())
        .toString()

    private fun decode(value: String): Identity {
        val json = JSONObject(value)
        return Identity(
            json.getString("device_id"),
            json.getString("name"),
            json.getString("x_public").base64UrlDecode(),
            json.getString("x_private").base64UrlDecode(),
            json.getString("sign_public").base64UrlDecode(),
            json.getString("sign_private").base64UrlDecode(),
        ).also {
            require(it.xPublic.size == 32 && it.xPrivate.size == 32)
            require(it.signPublic.size == 32 && it.signPrivate.size == 64)
        }
    }
}

fun ByteArray.base64Url(): String =
    Base64.encodeToString(this, Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)

fun String.base64UrlDecode(): ByteArray =
    Base64.decode(this, Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
