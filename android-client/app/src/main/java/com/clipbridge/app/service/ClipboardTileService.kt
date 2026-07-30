package com.clipbridge.app.service

import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import android.widget.Toast
import com.clipbridge.app.ClipBridgeApplication

class ClipboardTileService : TileService() {
    override fun onStartListening() {
        super.onStartListening()
        qsTile?.state = Tile.STATE_ACTIVE
        qsTile?.updateTile()
    }

    override fun onClick() {
        super.onClick()
        val clipboard = (application as ClipBridgeApplication).container.clipboard
        if (!clipboard.requestCurrent()) {
            Toast.makeText(
                this,
                "Android 未允许读取当前剪贴板，请打开 ClipBridge 或使用系统分享",
                Toast.LENGTH_LONG,
            ).show()
        } else {
            Toast.makeText(this, "已请求发送剪贴板", Toast.LENGTH_SHORT).show()
        }
    }
}

