package com.clipbridge.app.database

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

@Dao
interface HistoryDao {
    @Query(
        """SELECT * FROM clipboard_history
           WHERE content LIKE '%' || :query || '%'
           ORDER BY favorite DESC, created_at DESC""",
    )
    fun observe(query: String): Flow<List<HistoryEntity>>

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(item: HistoryEntity): Long

    @Query("SELECT EXISTS(SELECT 1 FROM clipboard_history WHERE message_id=:messageId)")
    suspend fun contains(messageId: String): Boolean

    @Query("UPDATE clipboard_history SET favorite=:favorite WHERE id=:id")
    suspend fun setFavorite(id: Long, favorite: Boolean)

    @Query("UPDATE clipboard_history SET sent=1 WHERE message_id=:messageId")
    suspend fun markSent(messageId: String)

    @Query("DELETE FROM clipboard_history WHERE id=:id")
    suspend fun delete(id: Long)

    @Query("DELETE FROM clipboard_history")
    suspend fun clear()

    @Query("DELETE FROM clipboard_history WHERE favorite=0 AND created_at<:cutoff")
    suspend fun deleteOlderThan(cutoff: Long)

    @Query(
        """DELETE FROM clipboard_history WHERE favorite=0 AND id NOT IN
           (SELECT id FROM clipboard_history ORDER BY favorite DESC, created_at DESC LIMIT :limit)""",
    )
    suspend fun trim(limit: Int)
}

