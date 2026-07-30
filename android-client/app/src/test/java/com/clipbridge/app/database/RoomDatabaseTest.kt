package com.clipbridge.app.database

import android.app.Application
import android.content.Context
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class)
class RoomDatabaseTest {
    private lateinit var database: ClipBridgeDatabase

    @Before
    fun setUp() {
        database = Room.inMemoryDatabaseBuilder(
            ApplicationProvider.getApplicationContext<Context>(),
            ClipBridgeDatabase::class.java,
        ).allowMainThreadQueries().build()
    }

    @After
    fun tearDown() {
        database.close()
    }

    @Test
    fun storesSearchesAndDeduplicatesHistory() = runBlocking {
        val item = HistoryEntity(
            messageId = "11111111-1111-4111-8111-111111111111",
            content = "Room history",
            contentHash = "hash",
            sourceDevice = "source",
            targetDevice = "target",
            createdAt = 1,
            receivedAt = 1,
        )
        assertTrue(database.historyDao().insert(item) > 0)
        assertEquals(-1, database.historyDao().insert(item))
        assertTrue(database.historyDao().contains(item.messageId))
        assertEquals(1, database.historyDao().observe("Room").first().size)
    }
}
