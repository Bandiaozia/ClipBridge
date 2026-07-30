package com.clipbridge.app.crypto

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * 用 Android Keystore 中不可导出的 AES-GCM 密钥包装令牌和设备私钥。
 * SharedPreferences 只接触随机 IV 与密文，认证标签损坏时数据会被拒绝。
 */
class SecurePreferences(context: Context) {
    private val preferences =
        context.getSharedPreferences("clipbridge_secure", Context.MODE_PRIVATE)
    private val alias = "clipbridge.credentials.v1"

    fun put(name: String, value: String) {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        val encrypted = cipher.doFinal(value.toByteArray(Charsets.UTF_8))
        preferences.edit()
            .putString("$name.iv", Base64.encodeToString(cipher.iv, Base64.NO_WRAP))
            .putString("$name.data", Base64.encodeToString(encrypted, Base64.NO_WRAP))
            .apply()
    }

    fun get(name: String): String? = runCatching {
        val iv = Base64.decode(preferences.getString("$name.iv", null), Base64.NO_WRAP)
        val data = Base64.decode(preferences.getString("$name.data", null), Base64.NO_WRAP)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(128, iv))
        cipher.doFinal(data).toString(Charsets.UTF_8)
    }.getOrNull()

    fun remove(name: String) {
        preferences.edit().remove("$name.iv").remove("$name.data").apply()
    }

    private fun secretKey(): SecretKey {
        val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (keyStore.getKey(alias, null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(
            KeyProperties.KEY_ALGORITHM_AES,
            "AndroidKeyStore",
        )
        generator.init(
            KeyGenParameterSpec.Builder(
                alias,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setRandomizedEncryptionRequired(true)
                .build(),
        )
        return generator.generateKey()
    }
}

