package com.clipbridge.app.service

import android.app.Service
import android.content.Intent
import android.os.IBinder
import com.clipbridge.app.ClipBridgeApplication

class SyncForegroundService : Service() {
    override fun onCreate() {
        super.onCreate()
        val container = (application as ClipBridgeApplication).container
        startForeground(FOREGROUND_ID, container.notifications.serviceNotification())
        container.clipboard.privileged.requestPermissionOrConnect()
        container.connectWebSocket()
    }

    override fun onDestroy() {
        (application as ClipBridgeApplication).container.webSocket.close()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    companion object {
        private const val FOREGROUND_ID = 1001
    }
}
