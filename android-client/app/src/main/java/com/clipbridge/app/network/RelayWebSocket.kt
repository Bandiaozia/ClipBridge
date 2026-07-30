package com.clipbridge.app.network

import com.clipbridge.app.domain.model.EncryptedEnvelope
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject
import java.util.concurrent.ConcurrentHashMap
import kotlin.math.min
import kotlin.random.Random

class RelayWebSocket(
    private val client: OkHttpClient,
    private val serverUrl: () -> String,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val _state = MutableStateFlow("未连接")
    val state = _state.asStateFlow()
    private val _frames = MutableSharedFlow<JSONObject>(extraBufferCapacity = 64)
    val frames = _frames.asSharedFlow()
    private val pending = ConcurrentHashMap<String, Pending>()
    private var socket: WebSocket? = null
    private var accessToken = ""
    private var deviceId = ""
    private var intentionalClose = false
    private var reconnectAttempt = 0
    private var reconnectJob: Job? = null
    private var resendJob: Job? = null

    fun connect(accessToken: String, deviceId: String) {
        this.accessToken = accessToken
        this.deviceId = deviceId
        intentionalClose = false
        reconnectAttempt = 0
        open()
    }

    fun updateToken(accessToken: String) {
        this.accessToken = accessToken
        sendAuth()
    }

    fun send(envelope: EncryptedEnvelope) {
        val json = envelope.toJson().toString()
        pending[envelope.messageId] = Pending(json, 1, System.currentTimeMillis() + 1000)
        socket?.send(json)
    }

    fun ack(messageId: String, status: String = "processed") {
        socket?.send(
            JSONObject()
                .put("type", "message_ack")
                .put("message_id", messageId)
                .put("status", status)
                .toString(),
        )
    }

    /**
     * pending 只会在收到协议 message_ack 后移除，因此这里等待的是对端确认，
     * 不是“已经写入本机 socket 缓冲区”。超时由调用方决定是否保留输入内容重试。
     */
    suspend fun awaitAck(messageId: String, timeoutMillis: Long = 10_000): Boolean {
        return withTimeoutOrNull(timeoutMillis) {
            while (pending.containsKey(messageId)) {
                delay(50)
            }
            true
        } ?: false
    }

    fun close() {
        intentionalClose = true
        reconnectJob?.cancel()
        resendJob?.cancel()
        socket?.close(1000, "客户端退出")
        socket = null
        _state.value = "已断开"
    }

    private fun open() {
        if (accessToken.isBlank() || deviceId.isBlank() || socket != null) return
        _state.value = "正在连接"
        val wsUrl = serverUrl().trimEnd('/')
            .replaceFirst("https://", "wss://")
            .replaceFirst("http://", "ws://") + "/api/v1/ws"
        val request = Request.Builder()
            .url(wsUrl)
            .header("Sec-WebSocket-Protocol", "clipbridge.v1")
            .header("User-Agent", "ClipBridge-Android/0.4")
            .build()
        socket = client.newWebSocket(request, Listener())
    }

    private fun sendAuth() {
        socket?.send(
            JSONObject()
                .put("type", "auth")
                .put("access_token", accessToken)
                .put("device_id", deviceId)
                .put("last_sequence", 0)
                .toString(),
        )
    }

    private fun scheduleReconnect() {
        if (intentionalClose) return
        reconnectJob?.cancel()
        val base = min(60_000L, 1000L shl min(reconnectAttempt, 5))
        reconnectAttempt++
        reconnectJob = scope.launch {
            delay(base + Random.nextLong(maxOf(1, base / 5)))
            if (isActive) open()
        }
    }

    private fun startResendLoop() {
        resendJob?.cancel()
        resendJob = scope.launch {
            while (isActive) {
                delay(1000)
                val now = System.currentTimeMillis()
                pending.forEach { (_, value) ->
                    if (value.nextAttempt <= now) {
                        socket?.send(value.json)
                        val seconds = min(30, 1 shl min(value.attempts, 5))
                        value.attempts++
                        value.nextAttempt = now + seconds * 1000L
                    }
                }
            }
        }
    }

    private inner class Listener : WebSocketListener() {
        override fun onOpen(webSocket: WebSocket, response: Response) {
            _state.value = "正在认证"
            sendAuth()
        }

        override fun onMessage(webSocket: WebSocket, text: String) {
            val json = runCatching { JSONObject(text) }.getOrNull() ?: return
            when (json.optString("type")) {
                "auth_ok" -> {
                    reconnectAttempt = 0
                    _state.value = "已连接"
                    startResendLoop()
                }
                "message_ack" -> pending.remove(json.optString("message_id"))
            }
            _frames.tryEmit(json)
        }

        override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
            if (socket !== webSocket) return
            socket = null
            resendJob?.cancel()
            _state.value = "已断开"
            scheduleReconnect()
        }

        override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
            if (socket !== webSocket) return
            socket = null
            resendJob?.cancel()
            _state.value = "连接错误"
            scheduleReconnect()
        }
    }

    private data class Pending(
        val json: String,
        var attempts: Int,
        var nextAttempt: Long,
    )
}

private fun EncryptedEnvelope.toJson() = JSONObject()
    .put("version", version)
    .put("type", type)
    .put("message_id", messageId)
    .put("sender_device_id", senderDeviceId)
    .put("recipient_device_id", recipientDeviceId)
    .put("created_at", createdAt)
    .put("expires_at", expiresAt)
    .put("nonce", nonce)
    .put("ciphertext", ciphertext)
    .put("signature", signature)
    .put("offline_allowed", offlineAllowed)
