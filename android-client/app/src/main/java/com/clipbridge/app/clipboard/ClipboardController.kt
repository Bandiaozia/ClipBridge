package com.clipbridge.app.clipboard

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import java.security.MessageDigest

/**
 * 普通模式仍只在 Activity 前台监听。用户通过桌面 ADB 启动桥接后，由 shell 进程
 * 提供后台监听；两种来源最终进入同一个去重/发送流，避免产生两套同步逻辑。
 */
class ClipboardController(context: Context) {
    private val clipboard =
        context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    private val _sendRequests = MutableSharedFlow<String>(extraBufferCapacity = 16)
    val sendRequests = _sendRequests.asSharedFlow()
    private var listening = false
    private var suppressedHash: String? = null
    private var suppressUntil = 0L
    private var lastEmittedHash: String? = null
    private var lastEmittedAt = 0L
    val privileged = AdbClipboardBridge(context, ::handleChangedText)
    private val listener = ClipboardManager.OnPrimaryClipChangedListener {
        // Activity 在前台时系统允许直接读取 ClipboardManager。不能在这里优先
        // 使用 ADB 桥接缓存：系统写入与桥接 CURRENT 回包之间存在短暂时差，
        // 会把写入前的旧文本误判成一次新的本地复制并发回其他设备。
        foregroundText()?.let(::handleChangedText)
    }

    fun initialize() = privileged.initialize()

    fun startForegroundListening() {
        if (!listening) {
            clipboard.addPrimaryClipChangedListener(listener)
            listening = true
        }
    }

    fun stopForegroundListening() {
        if (listening) {
            clipboard.removePrimaryClipChangedListener(listener)
            listening = false
        }
    }

    fun currentText(): String? = privileged.currentText() ?: foregroundText()

    private fun foregroundText(): String? = runCatching {
        clipboard.primaryClip?.getItemAt(0)?.coerceToText(null)?.toString()
    }.getOrNull()

    fun requestCurrent(): Boolean {
        val text = currentText()?.takeIf(String::isNotBlank) ?: return false
        return _sendRequests.tryEmit(text)
    }

    fun requestText(text: String): Boolean =
        text.takeIf(String::isNotBlank)?.let(_sendRequests::tryEmit) ?: false

    @Synchronized
    fun copyRemote(text: String) {
        suppressedHash = hash(text)
        suppressUntil = System.currentTimeMillis() + 5_000
        if (!privileged.setText(text)) {
            clipboard.setPrimaryClip(ClipData.newPlainText("ClipBridge", text))
        }
    }

    @Synchronized
    fun handleChangedText(text: String) {
        if (text.isBlank()) return
        val hash = hash(text)
        val now = System.currentTimeMillis()
        if (now <= suppressUntil && hash == suppressedHash) {
            // 同一次系统剪贴板写入可能同时触发 Android 前台监听器和 ADB
            // 轮询事件，部分厂商系统甚至会重复通知。不能在第一次命中后立即
            // 清除抑制标记，否则第二个回调会把远端内容原样发回，形成回声。
            return
        }
        // Samsung 等系统会把一次用户复制同时通知普通 ClipboardManager 和
        // shell 桥接。按内容摘要做短窗口去重，仍允许用户稍后主动重复复制。
        if (hash == lastEmittedHash && now - lastEmittedAt <= LOCAL_DEDUP_WINDOW_MS) return
        if (_sendRequests.tryEmit(text)) {
            lastEmittedHash = hash
            lastEmittedAt = now
        }
    }

    private fun hash(text: String): String =
        MessageDigest.getInstance("SHA-256").digest(text.toByteArray())
            .joinToString("") { "%02x".format(it) }

    private companion object {
        const val LOCAL_DEDUP_WINDOW_MS = 3_000L
    }
}
