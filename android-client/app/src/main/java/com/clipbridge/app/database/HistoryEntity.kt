package com.clipbridge.app.database

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

@Entity(
    tableName = "clipboard_history",
    indices = [
        Index(value = ["message_id"], unique = true),
        Index(value = ["created_at"]),
        Index(value = ["source_device"]),
    ],
)
data class HistoryEntity(
    @PrimaryKey(autoGenerate = true) val id: Long = 0,
    @ColumnInfo(name = "message_id") val messageId: String,
    val content: String,
    @ColumnInfo(name = "content_hash") val contentHash: String,
    @ColumnInfo(name = "source_device") val sourceDevice: String,
    @ColumnInfo(name = "target_device") val targetDevice: String,
    @ColumnInfo(name = "created_at") val createdAt: Long,
    @ColumnInfo(name = "received_at") val receivedAt: Long,
    val favorite: Boolean = false,
    @ColumnInfo(name = "is_read") val read: Boolean = false,
    val sent: Boolean = false,
    @ColumnInfo(name = "local_created") val local: Boolean = false,
    val expired: Boolean = false,
)

