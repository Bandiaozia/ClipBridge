package com.clipbridge.app.service

import android.app.Service
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.IBinder
import android.os.PowerManager
import com.clipbridge.app.ClipBridgeApplication

class SyncForegroundService : Service() {
    private var wakeLock: PowerManager.WakeLock? = null
    private var clipboardListener: ClipboardManager.OnPrimaryClipChangedListener? = null

    override fun onCreate() {
        super.onCreate()
        val container = (application as ClipBridgeApplication).container
        val powerManager = getSystemService(POWER_SERVICE) as PowerManager
        wakeLock = powerManager.newWakeLock(
            PowerManager.PARTIAL_WAKE_LOCK, "ClipBridge:background"
        ).apply { acquire() }
        startForeground(FOREGROUND_ID, container.notifications.serviceNotification())
        container.clipboard.privileged.requestPermissionOrConnect()
        container.connectWebSocket()

        // 前台服务常驻剪切板监听，退到后台也不断
        val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        clipboardListener = ClipboardManager.OnPrimaryClipChangedListener {
            cm.primaryClip?.getItemAt(0)?.text?.toString()?.let { text ->
                if (text.isNotBlank()) container.clipboard.handleChangedText(text)
            }
        }
        cm.addPrimaryClipChangedListener(clipboardListener!!)
    }

    override fun onDestroy() {
        val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        clipboardListener?.let { cm.removePrimaryClipChangedListener(it) }
        (application as ClipBridgeApplication).container.webSocket.close()
        wakeLock?.release()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    companion object {
        private const val FOREGROUND_ID = 1001
    }
}
