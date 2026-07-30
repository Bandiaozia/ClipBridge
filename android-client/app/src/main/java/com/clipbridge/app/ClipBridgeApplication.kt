package com.clipbridge.app

import android.app.Application
import android.content.Intent
import androidx.core.content.ContextCompat
import androidx.room.Room
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import com.clipbridge.app.clipboard.ClipboardController
import com.clipbridge.app.crypto.DeviceIdentity
import com.clipbridge.app.crypto.CryptoEngine
import com.clipbridge.app.crypto.SecurePreferences
import com.clipbridge.app.data.auth.AuthRepository
import com.clipbridge.app.data.auth.TokenStore
import com.clipbridge.app.database.ClipBridgeDatabase
import com.clipbridge.app.database.HistoryCleanupWorker
import com.clipbridge.app.network.RelayApi
import com.clipbridge.app.network.RelayWebSocket
import com.clipbridge.app.notification.ClipNotificationManager
import com.clipbridge.app.settings.SettingsStore
import com.clipbridge.app.domain.ClipboardSyncCoordinator
import com.clipbridge.app.service.SyncForegroundService
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

class ClipBridgeApplication : Application() {
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
        if (container.settings.foregroundService) {
            ContextCompat.startForegroundService(
                this,
                Intent(this, SyncForegroundService::class.java),
            )
        }
        val cleanup = PeriodicWorkRequestBuilder<HistoryCleanupWorker>(1, TimeUnit.DAYS)
            .setConstraints(Constraints.Builder().setRequiresBatteryNotLow(true).build())
            .build()
        WorkManager.getInstance(this).enqueueUniquePeriodicWork(
            "clipbridge-history-cleanup",
            ExistingPeriodicWorkPolicy.UPDATE,
            cleanup,
        )
    }
}

class AppContainer(application: Application) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    val settings = SettingsStore(application)
    private val secure = SecurePreferences(application)
    val identity = DeviceIdentity(application, secure)
    val tokenStore = TokenStore(secure)
    private val http = RelayApi.defaultClient()
    val api = RelayApi(http) { settings.serverUrl }
    val auth = AuthRepository(api, identity, tokenStore)
    val webSocket = RelayWebSocket(http) { settings.serverUrl }
    val clipboard = ClipboardController(application)
    val notifications = ClipNotificationManager(application)
    val database = Room.databaseBuilder(
        application,
        ClipBridgeDatabase::class.java,
        "clipbridge-history.db",
    ).build()
    val sync = ClipboardSyncCoordinator(
        clipboard,
        CryptoEngine(identity.loadOrCreate()),
        webSocket,
        database.historyDao(),
        notifications,
        settings,
    )

    init {
        clipboard.initialize()
        scope.launch {
            tokenStore.load()?.let {
                runCatching { api.devices(it.accessToken) }.onSuccess(sync::updateDevices)
                connectWebSocket()
            }
        }
        scope.launch {
            while (isActive) {
                delay(30_000)
                val tokens = tokenStore.load() ?: continue
                if (tokens.expiresAt <= System.currentTimeMillis() + 60_000) {
                    runCatching { auth.refresh() }.onSuccess {
                        webSocket.updateToken(it.accessToken)
                    }
                }
            }
        }
    }

    fun connectWebSocket() {
        if (!settings.connectionEnabled) return
        tokenStore.load()?.let {
            webSocket.connect(it.accessToken, identity.loadOrCreate().deviceId)
        }
    }
}
