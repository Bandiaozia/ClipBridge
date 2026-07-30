package com.clipbridge.app

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.clipbridge.app.clipboard.PrivilegedClipboardState
import java.util.UUID
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class AdbClipboardBridgeInstrumentedTest {
    /**
     * 测试进程不会启动 MainActivity，因此普通前台剪贴板监听器没有注册。
     * 唯一能把这次变化送入 sendRequests 的路径就是 ADB shell 桥接，
     * 可据此验证应用退到后台后仍能收到系统剪贴板事件。
     */
    @Test
    fun privilegedServiceObservesClipboardWithoutActivity() = runBlocking {
        val application =
            ApplicationProvider.getApplicationContext<ClipBridgeApplication>()
        val bridge = application.container.clipboard.privileged
        assertTrue(bridge.requestPermissionOrConnect())
        withTimeout(10_000) {
            bridge.state.first { it == PrivilegedClipboardState.ACTIVE }
        }
        val expected = "ClipBridge-ADB-E2E-${UUID.randomUUID()}"
        val observed = async(start = CoroutineStart.UNDISPATCHED) {
            withTimeout(10_000) {
                application.container.clipboard.sendRequests.first { it == expected }
            }
        }
        // 测试进程本身处于后台，Android 会限制它直接访问 ClipboardManager。
        // 这里必须经由 shell 身份的桥接写入，才能验证我们要交付的真实工作路径。
        assertTrue(bridge.setText(expected))

        val readBack = withTimeoutOrNull(5_000) {
            var value = bridge.currentText()
            while (value != expected) {
                delay(50)
                value = bridge.currentText()
            }
            value
        }
        assertEquals("shell diagnostics: ${bridge.diagnostics()}", expected, readBack)
        assertEquals(expected, observed.await())
        delay(1_000)
    }

    @Test
    fun remoteTextIsInSystemClipboardAndHistory() = runBlocking {
        val expected = requireNotNull(
            InstrumentationRegistry.getArguments().getString("expected"),
        )
        val application =
            ApplicationProvider.getApplicationContext<ClipBridgeApplication>()
        val bridge = application.container.clipboard.privileged
        assertTrue(bridge.requestPermissionOrConnect())
        withTimeout(10_000) {
            bridge.state.first { it == PrivilegedClipboardState.ACTIVE }
        }
        val clipboardValue = withTimeoutOrNull(15_000) {
            var value = bridge.currentText()
            while (value != expected) {
                delay(100)
                value = bridge.currentText()
            }
            value
        }
        assertEquals("shell diagnostics: ${bridge.diagnostics()}", expected, clipboardValue)
        val received = withTimeout(10_000) {
            application.container.database.historyDao().observe(expected)
                .first { rows -> rows.any { it.content == expected && !it.local } }
        }
        assertTrue(received.any { it.content == expected && !it.local && it.sent })
    }
}
