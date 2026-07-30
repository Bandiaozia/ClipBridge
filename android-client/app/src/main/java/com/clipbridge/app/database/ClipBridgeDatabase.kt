package com.clipbridge.app.database

import androidx.room.Database
import androidx.room.RoomDatabase

@Database(entities = [HistoryEntity::class], version = 1, exportSchema = true)
abstract class ClipBridgeDatabase : RoomDatabase() {
    abstract fun historyDao(): HistoryDao
}

