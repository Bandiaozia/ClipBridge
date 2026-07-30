package com.clipbridge.app

import android.Manifest
import android.app.ActivityManager
import android.app.Notification
import android.app.NotificationManager
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat
import androidx.room.Room
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.clipbridge.app.database.ClipBridgeDatabase
import com.clipbridge.app.database.HistoryEntity
import com.clipbridge.app.notification.ClipNotificationManager
import com.clipbridge.app.service.SyncForegroundService
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.util.UUID

@RunWith(AndroidJUnit4::class)
class PlatformComponentsInstrumentedTest {
    @Test
    fun loggedInAccountSeesOnlineDesktopDevice() = runBlocking {
        val application = InstrumentationRegistry.getInstrumentation()
            .targetContext.applicationContext as ClipBridgeApplication
        val tokens = application.container.auth.currentTokens()
        assertNotNull("真机尚未登录，无法验证公网设备列表", tokens)

        val ownId = application.container.auth.deviceIdentity().deviceId
        val devices = application.container.auth.devices()
        assertTrue("服务端设备列表中缺少当前 Android 设备", devices.any { it.id == ownId })
        assertTrue(
            "服务端设备列表中没有在线 Linux 桌面设备",
            devices.any { it.platform == "linux" && it.online && !it.revoked },
        )
    }

    @Test
    fun phoneSendsEncryptedTextAndReceivesDesktopAck() = runBlocking {
        val application = InstrumentationRegistry.getInstrumentation()
            .targetContext.applicationContext as ClipBridgeApplication
        assertNotNull(
            "真机尚未登录，无法执行手机到桌面的端到端测试",
            application.container.auth.currentTokens(),
        )

        val ownId = application.container.auth.deviceIdentity().deviceId
        val devices = application.container.auth.devices()
        val desktop = devices.firstOrNull {
            it.platform == "linux" && it.online && !it.revoked
        }
        assertNotNull("没有可用于端到端测试的在线 Linux 桌面设备", desktop)

        application.container.sync.updateDevices(devices.filter { it.id != ownId })
        application.container.sync.selectTarget(desktop!!.id)
        application.container.connectWebSocket()
        withTimeout(15_000) {
            application.container.webSocket.state.first { it == "已连接" }
        }

        val text = "ClipBridge phone-to-desktop E2E ${UUID.randomUUID()}"
        val serverResponse = async(start = CoroutineStart.UNDISPATCHED) {
            withTimeout(10_000) {
                application.container.webSocket.frames.first {
                    it.optString("type") == "message_queued" ||
                        it.optString("type") == "error"
                }
            }
        }
        application.container.sync.sendText(text)
        val response = serverResponse.await()
        assertEquals(
            "服务器拒绝了加密消息：${response.optString("code", "unknown")}",
            "message_queued",
            response.optString("type"),
        )
        val confirmed = withTimeout(20_000) {
            application.container.database.historyDao().observe(text).first { records ->
                records.any { it.content == text && it.local && it.sent }
            }
        }
        assertTrue("桌面没有返回消息 ACK", confirmed.any { it.content == text && it.sent })
    }

    @Test
    fun foregroundServiceStartsWithItsServiceNotification() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val serviceIntent = Intent(context, SyncForegroundService::class.java)
        val activityManager = context.getSystemService(ActivityManager::class.java)
        try {
            ContextCompat.startForegroundService(context, serviceIntent)
            val deadline = System.currentTimeMillis() + 2_000
            var foreground = false
            while (!foreground && System.currentTimeMillis() < deadline) {
                @Suppress("DEPRECATION")
                val services = activityManager.getRunningServices(Int.MAX_VALUE)
                foreground = services.any {
                    it.service.className == SyncForegroundService::class.java.name &&
                        it.foreground
                }
                if (!foreground) Thread.sleep(50)
            }
            assertTrue("SyncForegroundService 没有进入前台服务状态", foreground)
            assertEquals(
                ClipNotificationManager.SERVICE_CHANNEL,
                ClipNotificationManager(context).serviceNotification().channelId,
            )
        } finally {
            context.stopService(serviceIntent)
        }
    }

    @Test
    fun notificationAndShareEntryAreUsable() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        if (Build.VERSION.SDK_INT >= 33) {
            // 厂商系统可能禁止 shell 测试进程代授予权限，因此这里只验证用户已经
            // 通过正规的运行时权限弹窗授权，不尝试从测试代码改变安全设置。
            assertEquals(
                PackageManager.PERMISSION_GRANTED,
                ContextCompat.checkSelfPermission(
                    context,
                    Manifest.permission.POST_NOTIFICATIONS,
                ),
            )
        }

        val messageId = "platform-${UUID.randomUUID()}"
        val notificationId = messageId.hashCode()
        val systemNotifications = context.getSystemService(NotificationManager::class.java)
        try {
            ClipNotificationManager(context).showRemote(
                messageId,
                "真机测试设备",
                "ClipBridge notification test",
                sensitive = false,
            )
            val posted = systemNotifications.activeNotifications
                .firstOrNull { it.id == notificationId }
            assertNotNull("消息通知没有进入系统通知服务", posted)
            assertEquals(
                "ClipBridge notification test",
                posted!!.notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString(),
            )
            assertEquals(listOf("复制", "忽略"), posted.notification.actions.map { it.title })
        } finally {
            systemNotifications.cancel(notificationId)
        }

        val shareIntent = Intent(Intent.ACTION_SEND)
            .setType("text/plain")
            .putExtra(Intent.EXTRA_TEXT, "ClipBridge share test")
        val receivers = context.packageManager.queryIntentActivities(shareIntent, 0)
        assertTrue(
            "系统分享菜单中没有 ClipBridge 文本接收入口",
            receivers.any {
                it.activityInfo.packageName == context.packageName &&
                    it.activityInfo.name.endsWith("ShareReceiverActivity")
            },
        )
    }

    @Test
    fun roomPersistsAndDeduplicatesMessageIds() = runBlocking {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val database = Room.inMemoryDatabaseBuilder(
            context,
            ClipBridgeDatabase::class.java,
        ).build()
        try {
            val messageId = UUID.randomUUID().toString()
            val item = HistoryEntity(
                messageId = messageId,
                content = "Room 真机测试",
                contentHash = "test-hash",
                sourceDevice = "source",
                targetDevice = "target",
                createdAt = System.currentTimeMillis(),
                receivedAt = System.currentTimeMillis(),
            )
            assertTrue(database.historyDao().insert(item) > 0)
            assertTrue(database.historyDao().contains(messageId))
            assertEquals(-1L, database.historyDao().insert(item))
            database.historyDao().clear()
            assertTrue(!database.historyDao().contains(messageId))
        } finally {
            database.close()
        }
    }
}
