package com.clipbridge.app.service

import android.app.Service
import android.content.Intent
import android.os.IBinder
import android.os.PowerManager
import com.clipbridge.app.ClipBridgeApplication

class SyncForegroundService : Service() {
    private var wakeLock: PowerManager.WakeLock? = null

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
    }

    override fun onDestroy() {
        (application as ClipBridgeApplication).container.webSocket.close()
        wakeLock?.release()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    companion object {
        private const val FOREGROUND_ID = 1001
    }
}
