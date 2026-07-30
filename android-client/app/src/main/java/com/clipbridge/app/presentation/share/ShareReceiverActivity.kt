package com.clipbridge.app.presentation.share

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.widget.Toast
import com.clipbridge.app.ClipBridgeApplication

class ShareReceiverActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val text = if (intent?.action == Intent.ACTION_SEND) {
            intent.getStringExtra(Intent.EXTRA_TEXT)
        } else {
            null
        }
        val accepted = text?.let {
            (application as ClipBridgeApplication).container.clipboard.requestText(it)
        } == true
        Toast.makeText(
            this,
            if (accepted) "已交给 ClipBridge 发送" else "没有可发送的文本",
            Toast.LENGTH_SHORT,
        ).show()
        finish()
    }
}

