package com.clipbridge.app.notification

import android.app.NotificationManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import com.clipbridge.app.ClipBridgeApplication

class NotificationActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val id = intent.getIntExtra(EXTRA_NOTIFICATION_ID, 0)
        if (intent.action == ACTION_COPY) {
            intent.getStringExtra(EXTRA_TEXT)?.let {
                (context.applicationContext as ClipBridgeApplication)
                    .container.clipboard.copyRemote(it)
            }
        }
        context.getSystemService(NotificationManager::class.java).cancel(id)
    }

    companion object {
        const val ACTION_COPY = "com.clipbridge.app.COPY_REMOTE"
        const val ACTION_IGNORE = "com.clipbridge.app.IGNORE_REMOTE"
        const val EXTRA_TEXT = "text"
        const val EXTRA_NOTIFICATION_ID = "notification_id"
    }
}

