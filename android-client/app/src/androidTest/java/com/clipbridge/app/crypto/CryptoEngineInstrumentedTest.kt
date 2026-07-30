package com.clipbridge.app.crypto

import androidx.test.ext.junit.runners.AndroidJUnit4
import com.clipbridge.app.domain.model.Device
import com.goterl.lazysodium.LazySodiumAndroid
import com.goterl.lazysodium.SodiumAndroid
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import org.junit.runner.RunWith
import java.util.UUID

@RunWith(AndroidJUnit4::class)
class CryptoEngineInstrumentedTest {
    @Test
    fun roundTripAndTamperRejection() {
        val sodium = LazySodiumAndroid(SodiumAndroid())
        val senderIdentity = testIdentity(sodium, "sender")
        val recipientIdentity = testIdentity(sodium, "recipient")
        val senderCrypto = CryptoEngine(senderIdentity)
        val recipientCrypto = CryptoEngine(recipientIdentity)
        val recipient = Device(
            recipientIdentity.deviceId,
            "recipient",
            "android",
            recipientIdentity.xPublic.base64Url(),
            recipientIdentity.signPublic.base64Url(),
        )
        val sender = Device(
            senderIdentity.deviceId,
            "sender",
            "android",
            senderIdentity.xPublic.base64Url(),
            senderIdentity.signPublic.base64Url(),
        )
        val envelope = senderCrypto.encrypt("Android 加密测试", recipient)
        assertEquals("Android 加密测试", recipientCrypto.decrypt(envelope, sender))

        val damaged = envelope.copy(
            ciphertext = envelope.ciphertext.base64UrlDecode()
                .also { it[0] = (it[0].toInt() xor 1).toByte() }
                .base64Url(),
        )
        assertThrows(IllegalArgumentException::class.java) {
            recipientCrypto.decrypt(damaged, sender)
        }
    }

    private fun testIdentity(sodium: LazySodiumAndroid, name: String): Identity {
        val x = sodium.cryptoKxKeypair()
        val sign = sodium.cryptoSignKeypair()
        return Identity(
            UUID.randomUUID().toString(),
            name,
            x.publicKey.asBytes,
            x.secretKey.asBytes,
            sign.publicKey.asBytes,
            sign.secretKey.asBytes,
        )
    }
}
