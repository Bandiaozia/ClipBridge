package com.clipbridge.app.notification

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.clipbridge.app.MainActivity
import com.clipbridge.app.R

class ClipNotificationManager(private val context: Context) {
    private val manager = context.getSystemService(NotificationManager::class.java)

    init {
        manager.createNotificationChannel(
            NotificationChannel(
                MESSAGE_CHANNEL,
                context.getString(R.string.notification_channel_messages),
                NotificationManager.IMPORTANCE_HIGH,
            ),
        )
        manager.createNotificationChannel(
            NotificationChannel(
                SERVICE_CHANNEL,
                context.getString(R.string.notification_channel_service),
                NotificationManager.IMPORTANCE_LOW,
            ),
        )
    }

    fun showRemote(messageId: String, source: String, text: String, sensitive: Boolean) {
        if (
            Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) return
        val copy = PendingIntent.getBroadcast(
            context,
            messageId.hashCode(),
            Intent(context, NotificationActionReceiver::class.java)
                .setAction(NotificationActionReceiver.ACTION_COPY)
                .putExtra(NotificationActionReceiver.EXTRA_TEXT, text)
                .putExtra(NotificationActionReceiver.EXTRA_NOTIFICATION_ID, messageId.hashCode()),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val ignore = PendingIntent.getBroadcast(
            context,
            messageId.hashCode() xor 0x55aa,
            Intent(context, NotificationActionReceiver::class.java)
                .setAction(NotificationActionReceiver.ACTION_IGNORE)
                .putExtra(NotificationActionReceiver.EXTRA_NOTIFICATION_ID, messageId.hashCode()),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val open = PendingIntent.getActivity(
            context,
            0,
            Intent(context, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val body = if (sensitive) "疑似敏感内容，正文已隐藏" else text.take(160)
        val notification = NotificationCompat.Builder(context, MESSAGE_CHANNEL)
            .setSmallIcon(android.R.drawable.ic_dialog_email)
            .setContentTitle("来自 $source")
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setContentIntent(open)
            .setAutoCancel(true)
            .addAction(0, "复制", copy)
            .addAction(0, "忽略", ignore)
            .setVisibility(
                if (sensitive) NotificationCompat.VISIBILITY_PRIVATE
                else NotificationCompat.VISIBILITY_PUBLIC,
            )
            .build()
        manager.notify(messageId.hashCode(), notification)
    }

    fun serviceNotification() = NotificationCompat.Builder(context, SERVICE_CHANNEL)
        .setSmallIcon(android.R.drawable.stat_notify_sync)
        .setContentTitle("ClipBridge 后台互通运行中")
        .setContentText("通过已认证的 ADB 桥接监听并同步文字剪贴板")
        .setOngoing(true)
        .build()

    companion object {
        const val MESSAGE_CHANNEL = "clipbridge_messages"
        const val SERVICE_CHANNEL = "clipbridge_service"
    }
}
