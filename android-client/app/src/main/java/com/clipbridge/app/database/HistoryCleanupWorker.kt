package com.clipbridge.app.database

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import com.clipbridge.app.ClipBridgeApplication

class HistoryCleanupWorker(
    context: Context,
    parameters: WorkerParameters,
) : CoroutineWorker(context, parameters) {
    override suspend fun doWork(): Result {
        val container = (applicationContext as ClipBridgeApplication).container
        val cutoff = System.currentTimeMillis() -
            container.settings.retentionDays.toLong() * 86_400_000
        return runCatching {
            container.database.historyDao().deleteOlderThan(cutoff)
            container.database.historyDao().trim(container.settings.maxHistory)
            Result.success()
        }.getOrElse { Result.retry() }
    }
}

