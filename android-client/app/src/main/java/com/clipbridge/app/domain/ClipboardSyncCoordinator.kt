package com.clipbridge.app.domain

import com.clipbridge.app.clipboard.ClipboardController
import com.clipbridge.app.crypto.CryptoEngine
import com.clipbridge.app.crypto.SensitiveDetector
import com.clipbridge.app.database.HistoryDao
import com.clipbridge.app.database.HistoryEntity
import com.clipbridge.app.domain.model.Device
import com.clipbridge.app.domain.model.EncryptedEnvelope
import com.clipbridge.app.network.RelayWebSocket
import com.clipbridge.app.notification.ClipNotificationManager
import com.clipbridge.app.settings.SettingsStore
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import org.json.JSONObject
import java.util.ArrayDeque

class ClipboardSyncCoordinator(
    private val clipboard: ClipboardController,
    private val crypto: CryptoEngine,
    private val webSocket: RelayWebSocket,
    private val history: HistoryDao,
    private val notifications: ClipNotificationManager,
    private val settings: SettingsStore,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val devices = MutableStateFlow<List<Device>>(emptyList())
    private val _status = MutableStateFlow<String?>(null)
    val status = _status.asStateFlow()
    private val recent = LinkedHashSet<String>()
    private val recentOrder = ArrayDeque<String>()

    init {
        scope.launch { clipboard.sendRequests.collect { sendText(it) } }
        scope.launch {
            webSocket.frames.collect { frame ->
                when (frame.optString("type")) {
                    "clipboard_text" -> receive(frame)
                    "message_ack" -> history.markSent(frame.optString("message_id"))
                    "device_revoked" -> {
                        val id = frame.optString("device_id")
                        devices.value = devices.value.filterNot { it.id == id }
                    }
                }
            }
        }
    }

    fun updateDevices(value: List<Device>) {
        devices.value = value.filterNot(Device::revoked)
    }

    fun selectTarget(id: String) {
        settings.selectedDeviceId = id
    }

    suspend fun sendText(text: String): String? {
        val reasons = SensitiveDetector.reasons(text)
        if (reasons.isNotEmpty()) {
            _status.value = "检测到疑似敏感内容：${reasons.joinToString("、")}，已阻止自动发送"
            return null
        }
        val target = devices.value.firstOrNull { it.id == settings.selectedDeviceId }
        if (target == null) {
            _status.value = "请先选择已配对设备"
            return null
        }
        return runCatching {
            val envelope = crypto.encrypt(text, target)
            history.insert(
                HistoryEntity(
                    messageId = envelope.messageId,
                    content = text,
                    contentHash = CryptoEngine.sha256(text.toByteArray())
                        .joinToString("") { "%02x".format(it) },
                    sourceDevice = envelope.senderDeviceId,
                    targetDevice = envelope.recipientDeviceId,
                    createdAt = envelope.createdAt,
                    receivedAt = envelope.createdAt,
                    local = true,
                ),
            )
            remember(envelope.messageId)
            webSocket.send(envelope)
            envelope.messageId
        }.onSuccess {
            _status.value = null
        }.onFailure {
            _status.value = it.message
        }.getOrNull()
    }

    private suspend fun receive(frame: JSONObject) {
        val envelope = frame.toEnvelope()
        if (envelope.messageId in recent || history.contains(envelope.messageId)) {
            webSocket.ack(envelope.messageId)
            return
        }
        if (envelope.expiresAt <= System.currentTimeMillis()) {
            webSocket.ack(envelope.messageId, "expired")
            return
        }
        val sender = devices.value.firstOrNull { it.id == envelope.senderDeviceId }
        if (sender == null || sender.revoked) {
            _status.value = "拒绝未知或已撤销设备的消息"
            return
        }
        runCatching { crypto.decrypt(envelope, sender) }
            .onSuccess { text ->
                val sensitive = SensitiveDetector.isSensitive(text)
                if (!sensitive) {
                    history.insert(
                        HistoryEntity(
                            messageId = envelope.messageId,
                            content = text,
                            contentHash = CryptoEngine.sha256(text.toByteArray())
                                .joinToString("") { "%02x".format(it) },
                            sourceDevice = sender.id,
                            targetDevice = envelope.recipientDeviceId,
                            createdAt = envelope.createdAt,
                            receivedAt = System.currentTimeMillis(),
                            read = false,
                            sent = true,
                        ),
                    )
                }
                remember(envelope.messageId)
                webSocket.ack(envelope.messageId)
                notifications.showRemote(
                    envelope.messageId,
                    sender.name,
                    text,
                    sensitive,
                )
                if (settings.autoCopyRemote && !sensitive) clipboard.copyRemote(text)
            }
            .onFailure { _status.value = it.message }
    }

    private fun remember(messageId: String) {
        if (!recent.add(messageId)) return
        recentOrder.addLast(messageId)
        while (recentOrder.size > 4096) recent.remove(recentOrder.removeFirst())
    }
}

private fun JSONObject.toEnvelope() = EncryptedEnvelope(
    version = getInt("version"),
    type = getString("type"),
    messageId = getString("message_id"),
    senderDeviceId = getString("sender_device_id"),
    recipientDeviceId = getString("recipient_device_id"),
    createdAt = getLong("created_at"),
    expiresAt = getLong("expires_at"),
    nonce = getString("nonce"),
    ciphertext = getString("ciphertext"),
    signature = getString("signature"),
    offlineAllowed = optBoolean("offline_allowed", true),
)
