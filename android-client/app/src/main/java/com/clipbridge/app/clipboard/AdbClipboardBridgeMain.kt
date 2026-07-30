package com.clipbridge.app.clipboard

import android.content.ClipData
import android.content.Context
import android.os.IBinder
import android.os.Process
import java.io.BufferedReader
import java.io.BufferedWriter
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.lang.reflect.InvocationTargetException
import java.lang.reflect.Method
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.ServerSocket
import java.net.Socket
import java.util.Base64
import java.util.concurrent.atomic.AtomicBoolean

/**
 * 由桌面端通过 `adb shell app_process` 启动。它不是 Android 组件，不会被普通应用直接
 * 导出；唯一入口是回环端口，并要求桌面端每次启动生成的 256 位随机令牌。
 */
object AdbClipboardBridgeMain {
    @JvmStatic
    fun main(args: Array<String>) {
        require(Process.myUid() == Process.SHELL_UID || Process.myUid() == Process.ROOT_UID)
        require(args.size == 2)
        val token = args[0]
        val port = args[1].toInt()
        require(Regex("^[a-f0-9]{64}$").matches(token))
        require(port in 10_000..65_535)

        val clipboard = ClipboardBinderAccess()
        ServerSocket().use { server ->
            server.reuseAddress = true
            server.bind(InetSocketAddress(InetAddress.getLoopbackAddress(), port), 1)
            while (true) {
                val client = server.accept()
                // 正式应用会长期占用一条连接。为诊断、进程平滑重连保留并发
                // 客户端能力，否则新连接会卡在 accept 队列，无法判断桥接是否健康。
                Thread({
                    runCatching { serve(client, token, clipboard) }
                    runCatching { client.close() }
                }, "clipbridge-adb-session").apply {
                    isDaemon = true
                    start()
                }
            }
        }
    }

    private fun serve(
        socket: Socket,
        token: String,
        clipboard: ClipboardBinderAccess,
    ) {
        socket.tcpNoDelay = true
        socket.soTimeout = AUTH_TIMEOUT_MS
        val input = BufferedReader(InputStreamReader(socket.getInputStream()))
        val output = BufferedWriter(OutputStreamWriter(socket.getOutputStream()))
        val authentication = input.readLine()
        if (authentication != "AUTH\t$token") return
        socket.soTimeout = 0
        val writeLock = Any()
        fun reply(line: String) {
            synchronized(writeLock) {
                output.write(line)
                output.newLine()
                output.flush()
            }
        }
        reply("READY")
        val running = AtomicBoolean(true)
        val poller = Thread({
            var previous = clipboard.readText()
            while (running.get() && !Thread.currentThread().isInterrupted) {
                try {
                    Thread.sleep(POLL_INTERVAL_MS)
                    val current = clipboard.readText()
                    if (current != null && current != previous) {
                        previous = current
                        reply("EVENT\t${encode(current)}")
                    } else if (current == null) {
                        previous = null
                    }
                } catch (_: InterruptedException) {
                    Thread.currentThread().interrupt()
                } catch (_: Throwable) {
                    break
                }
            }
        }, "clipbridge-adb-poller").apply {
            isDaemon = true
            start()
        }
        try {
            while (true) {
                val line = input.readLine() ?: break
                if (line.length > MAX_PROTOCOL_LINE) {
                    reply("ERROR\trequest-too-large")
                    continue
                }
                when {
                    line == "PING" -> reply("PONG")
                    line == "GET" -> reply(
                        "CURRENT\t${encode(clipboard.readText().orEmpty())}",
                    )
                    line.startsWith("SET\t") -> {
                        val text = decode(line.substringAfter('\t'))
                        if (text == null || text.isBlank() || text.length > MAX_TEXT_LENGTH) {
                            reply("ERROR\tinvalid-text")
                        } else if (clipboard.setText(text)) {
                            // 不能只返回一个无状态的 OK：客户端可能刚完成重连，若此时恰好
                            // 错过轮询 EVENT，就无法确认本地镜像已经更新。直接回传写入值，
                            // EVENT 仍保留用于捕获来自输入法或其他应用的后续变化。
                            reply("CURRENT\t${encode(text)}")
                        } else {
                            reply("ERROR\tclipboard-write-failed")
                        }
                    }
                }
            }
        } finally {
            running.set(false)
            poller.interrupt()
            runCatching { poller.join(1_000) }
        }
    }

    private fun encode(text: String): String =
        Base64.getEncoder().encodeToString(text.toByteArray(Charsets.UTF_8))

    private fun decode(value: String): String? = runCatching {
        String(Base64.getDecoder().decode(value), Charsets.UTF_8)
    }.getOrNull()

    private const val AUTH_TIMEOUT_MS = 5_000
    private const val POLL_INTERVAL_MS = 350L
    private const val MAX_TEXT_LENGTH = 1_000_000
    private const val MAX_PROTOCOL_LINE = 1_400_000
}

/**
 * 根据设备运行时 IClipboard 签名构造参数，兼容 Android 10～16 的 userId/deviceId
 * 变化；不硬编码 Binder 事务编号。
 */
private class ClipboardBinderAccess {
    private val service: Any
    private val getMethod: Method
    private val setMethod: Method

    init {
        val serviceManager = Class.forName("android.os.ServiceManager")
        val binder = serviceManager.getDeclaredMethod("getService", String::class.java)
            .invoke(null, Context.CLIPBOARD_SERVICE) as IBinder
        val stub = Class.forName("android.content.IClipboard\$Stub")
        service = requireNotNull(
            stub.getDeclaredMethod("asInterface", IBinder::class.java).invoke(null, binder),
        )
        getMethod = service.javaClass.methods.first {
            it.name == "getPrimaryClip" && ClipData::class.java.isAssignableFrom(it.returnType)
        }.apply { isAccessible = true }
        setMethod = service.javaClass.methods.first {
            it.name == "setPrimaryClip" &&
                it.parameterTypes.firstOrNull() == ClipData::class.java
        }.apply { isAccessible = true }
    }

    fun readText(): String? = try {
        val clip = getMethod.invoke(service, *arguments(getMethod, null)) as? ClipData
            ?: return null
        if (clip.itemCount == 0) null else clip.getItemAt(0).text?.toString()
            ?.takeIf { it.isNotBlank() && it.length <= MAX_TEXT_LENGTH }
    } catch (_: Throwable) {
        null
    }

    fun setText(text: String): Boolean = try {
        setMethod.invoke(
            service,
            *arguments(setMethod, ClipData.newPlainText("ClipBridge", text)),
        )
        true
    } catch (_: InvocationTargetException) {
        false
    } catch (_: Throwable) {
        false
    }

    private fun arguments(method: Method, clip: ClipData?): Array<Any?> {
        var stringIndex = 0
        var intIndex = 0
        return method.parameterTypes.map { type ->
            when {
                type == ClipData::class.java -> clip
                type == String::class.java ->
                    if (stringIndex++ == 0) SHELL_PACKAGE else null
                type == Int::class.javaPrimitiveType ->
                    if (intIndex++ == 0) USER_ID else DEFAULT_DEVICE_ID
                type == Boolean::class.javaPrimitiveType -> false
                else -> null
            }
        }.toTypedArray()
    }

    private companion object {
        const val SHELL_PACKAGE = "com.android.shell"
        const val USER_ID = 0
        const val DEFAULT_DEVICE_ID = 0
        const val MAX_TEXT_LENGTH = 1_000_000
    }
}
