package com.clipbridge.app.clipboard

import android.content.Context
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Socket
import java.util.Base64
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow

enum class PrivilegedClipboardState(val label: String) {
    ADB_STOPPED("ADB 后台桥接未启动"),
    CONNECTING("正在连接 ADB 剪贴板桥接"),
    ACTIVE("后台剪贴板互通已启用"),
    ERROR("ADB 剪贴板桥接不可用"),
}

/**
 * 普通应用只连接本机回环地址，系统剪贴板隐藏 API 由 ADB 启动的 shell 进程调用。
 * 256 位随机令牌同时交给两端，避免其他普通应用猜测端口后读写用户剪贴板。
 */
class AdbClipboardBridge(
    context: Context,
    private val onTextChanged: (String) -> Unit,
) {
    private val preferences = context.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)
    private val _state = MutableStateFlow(PrivilegedClipboardState.ADB_STOPPED)
    val state = _state.asStateFlow()
    private val closed = AtomicBoolean(false)
    private val loopStarted = AtomicBoolean(false)
    private val writeLock = Any()

    @Volatile
    private var socket: Socket? = null
    @Volatile
    private var writer: BufferedWriter? = null
    @Volatile
    private var lastText: String? = null
    @Volatile
    private var lastDiagnostic = "not-connected"

    fun initialize() {
        ensureConnectionLoop()
    }

    fun activate(token: String, port: Int): Boolean {
        if (!TOKEN_REGEX.matches(token) || port !in 10_000..65_535) return false
        preferences.edit()
            .putString(KEY_TOKEN, token)
            .putInt(KEY_PORT, port)
            .apply()
        _state.value = PrivilegedClipboardState.CONNECTING
        runCatching { socket?.close() }
        ensureConnectionLoop()
        return true
    }

    fun requestPermissionOrConnect(): Boolean {
        val token = preferences.getString(KEY_TOKEN, null)
        if (token == null || !TOKEN_REGEX.matches(token)) {
            _state.value = PrivilegedClipboardState.ADB_STOPPED
            return false
        }
        _state.value = PrivilegedClipboardState.CONNECTING
        runCatching { socket?.close() }
        ensureConnectionLoop()
        return true
    }

    fun currentText(): String? = lastText

    fun setText(text: String): Boolean {
        if (text.isBlank() || text.length > MAX_TEXT_LENGTH) return false
        return send("SET\t${encode(text)}")
    }

    fun diagnostics(): String = lastDiagnostic

    fun isActive(): Boolean =
        writer != null && _state.value == PrivilegedClipboardState.ACTIVE

    fun close() {
        closed.set(true)
        writer = null
        runCatching { socket?.close() }
        socket = null
        _state.value = PrivilegedClipboardState.ADB_STOPPED
    }

    private fun ensureConnectionLoop() {
        if (!loopStarted.compareAndSet(false, true)) return
        Thread(::connectionLoop, "clipbridge-adb-client").apply {
            isDaemon = true
            start()
        }
    }

    private fun connectionLoop() {
        while (!closed.get()) {
            val token = preferences.getString(KEY_TOKEN, null)
            val port = preferences.getInt(KEY_PORT, DEFAULT_PORT)
            if (token == null || !TOKEN_REGEX.matches(token)) {
                _state.value = PrivilegedClipboardState.ADB_STOPPED
                sleepBeforeRetry()
                continue
            }
            _state.value = PrivilegedClipboardState.CONNECTING
            try {
                val connection = Socket()
                socket = connection
                connection.tcpNoDelay = true
                connection.connect(
                    InetSocketAddress(InetAddress.getLoopbackAddress(), port),
                    CONNECT_TIMEOUT_MS,
                )
                val output = BufferedWriter(OutputStreamWriter(connection.getOutputStream()))
                val input = BufferedReader(InputStreamReader(connection.getInputStream()))
                synchronized(writeLock) {
                    writer = output
                    output.write("AUTH\t$token\n")
                    output.flush()
                }
                require(input.readLine() == "READY") { "bridge-authentication-failed" }
                lastDiagnostic = "adb-loopback:$port"
                _state.value = PrivilegedClipboardState.ACTIVE
                send("GET")
                while (!closed.get()) {
                    val line = input.readLine() ?: break
                    if (line.length > MAX_PROTOCOL_LINE) continue
                    when {
                        line.startsWith("EVENT\t") -> decode(line.substringAfter('\t'))
                            ?.let {
                                lastText = it
                                if (it.isNotBlank()) onTextChanged(it)
                            }
                        line.startsWith("CURRENT\t") -> decode(line.substringAfter('\t'))
                            ?.let { lastText = it }
                        line.startsWith("ERROR\t") ->
                            lastDiagnostic = line.substringAfter('\t').take(240)
                    }
                }
            } catch (error: Throwable) {
                lastDiagnostic = "${error.javaClass.simpleName}:${error.message}"
                _state.value = PrivilegedClipboardState.ADB_STOPPED
            } finally {
                synchronized(writeLock) { writer = null }
                runCatching { socket?.close() }
                socket = null
            }
            sleepBeforeRetry()
        }
    }

    private fun send(line: String): Boolean {
        if (line.length > MAX_PROTOCOL_LINE) return false
        return synchronized(writeLock) {
            val output = writer ?: return@synchronized false
            runCatching {
                output.write(line)
                output.newLine()
                output.flush()
            }.isSuccess
        }
    }

    private fun sleepBeforeRetry() {
        try {
            Thread.sleep(RECONNECT_DELAY_MS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
        }
    }

    private fun encode(text: String): String =
        Base64.getEncoder().encodeToString(text.toByteArray(Charsets.UTF_8))

    private fun decode(value: String): String? = runCatching {
        String(Base64.getDecoder().decode(value), Charsets.UTF_8)
    }.getOrNull()

    private companion object {
        const val PREFERENCES = "clipbridge_adb_bridge"
        const val KEY_TOKEN = "token"
        const val KEY_PORT = "port"
        const val DEFAULT_PORT = 39_471
        const val CONNECT_TIMEOUT_MS = 1_500
        const val RECONNECT_DELAY_MS = 2_000L
        const val MAX_TEXT_LENGTH = 1_000_000
        const val MAX_PROTOCOL_LINE = 1_400_000
        val TOKEN_REGEX = Regex("^[a-f0-9]{64}$")
    }
}
