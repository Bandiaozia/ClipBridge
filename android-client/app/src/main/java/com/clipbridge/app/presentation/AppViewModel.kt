package com.clipbridge.app.presentation

import android.app.Application
import android.content.Intent
import androidx.core.content.ContextCompat
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.clipbridge.app.ClipBridgeApplication
import com.clipbridge.app.database.HistoryEntity
import com.clipbridge.app.domain.model.Device
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import org.json.JSONObject
import com.clipbridge.app.service.SyncForegroundService
import com.clipbridge.app.clipboard.PrivilegedClipboardState

enum class Screen { AUTH, HOME, DEVICES, HISTORY, PAIRING, SETTINGS, HELP }

data class AppUiState(
    val screen: Screen = Screen.AUTH,
    val register: Boolean = false,
    val loading: Boolean = false,
    val connection: String = "未连接",
    val error: String? = null,
    val devices: List<Device> = emptyList(),
    val selectedDeviceIds: Set<String> = emptySet(),
    val pendingText: String = "",
    val clipboardMode: String = PrivilegedClipboardState.ADB_STOPPED.label,
    val themeColor: String = "purple",
    val sendingText: Boolean = false,
)

class AppViewModel(application: Application) : AndroidViewModel(application) {
    private val container = (application as ClipBridgeApplication).container
    private val historyDao = container.database.historyDao()
    private val _state = MutableStateFlow(
        AppUiState(
            screen = if (container.auth.currentTokens() == null) Screen.AUTH else Screen.HOME,
            themeColor = container.settings.themeColor,
        ),
    )
    val state = _state.asStateFlow()
    val history: StateFlow<List<HistoryEntity>> = historyDao.observe("")
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), emptyList())

    init {
        viewModelScope.launch {
            container.webSocket.state.collect { connection ->
                _state.value = _state.value.copy(connection = connection)
            }
        }
        viewModelScope.launch {
            container.sync.status.collect { message ->
                if (message != null) _state.value = _state.value.copy(error = message)
            }
        }
        viewModelScope.launch {
            container.clipboard.privileged.state.collect { mode ->
                _state.value = _state.value.copy(clipboardMode = mode.label)
            }
        }
        viewModelScope.launch {
            // 在线状态由中继实时推送；界面直接消费服务端事件，避免用静态文案假装在线。
            container.webSocket.frames.collect { frame ->
                val deviceId = frame.optString("device_id")
                when (frame.optString("type")) {
                    "auth_ok" -> refreshDevices()
                    "device_online", "device_offline" -> {
                        val online = frame.optString("type") == "device_online"
                        _state.value = _state.value.copy(
                            devices = _state.value.devices.map { device ->
                                if (device.id == deviceId) device.copy(online = online) else device
                            },
                        )
                    }
                    "device_revoked" -> {
                        val ids = container.settings.selectedDeviceIds - deviceId
                        container.settings.selectedDeviceIds = ids
                        _state.value = _state.value.copy(
                            devices = _state.value.devices.filterNot { it.id == deviceId },
                            selectedDeviceIds = ids,
                        )
                    }
                }
            }
        }
        if (container.auth.currentTokens() != null) {
            container.connectWebSocket()
            refreshDevices()
        }
    }

    fun setScreen(screen: Screen) {
        _state.value = _state.value.copy(screen = screen, error = null)
    }

    fun setRegister(register: Boolean) {
        _state.value = _state.value.copy(register = register, error = null)
    }

    fun authenticate(email: String, password: String) {
        if (email.isBlank() || password.length < 10) {
            _state.value = _state.value.copy(error = "请输入有效邮箱和至少 10 位密码")
            return
        }
        viewModelScope.launch {
            _state.value = _state.value.copy(loading = true, error = null)
            runCatching {
                container.auth.authenticate(email, password, _state.value.register)
            }.onSuccess {
                _state.value = _state.value.copy(loading = false, screen = Screen.HOME)
                container.settings.connectionEnabled = true
                container.connectWebSocket()
                refreshDevices()
            }.onFailure {
                _state.value = _state.value.copy(loading = false, error = it.message)
            }
        }
    }

    fun refreshDevices() {
        viewModelScope.launch {
            runCatching { container.auth.devices() }
                .onSuccess { devices ->
                    val self = container.auth.deviceIdentity().deviceId
                    val peers = devices.filter { it.id != self && !it.revoked }
                    container.sync.updateDevices(peers)
                    // 过滤掉已不存在的设备 ID，首次打开自动选第一个在线设备
                    val validIds = peers.map { it.id }.toSet()
                    val selected = container.settings.selectedDeviceIds
                        .filter { it in validIds }.toSet()
                        .ifEmpty {
                            peers.firstOrNull { it.online }?.id?.let { setOf(it) } ?: emptySet()
                        }
                    container.settings.selectedDeviceIds = selected
                    _state.value = _state.value.copy(
                        devices = peers,
                        selectedDeviceIds = selected,
                    )
                }
                .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    fun toggleDevice(id: String) {
        container.sync.toggleTarget(id)
        val updated = container.settings.selectedDeviceIds
        _state.value = _state.value.copy(selectedDeviceIds = updated)
    }

    fun sendCurrentClipboard() {
        if (!container.clipboard.requestCurrent()) {
            _state.value = _state.value.copy(
                error = "系统未允许读取剪贴板，请保持应用在前台或使用系统分享",
            )
        }
    }

    fun updatePendingText(text: String) {
        // 与协议正文限制保持一致，避免输入框成为绕过剪贴板长度校验的入口。
        _state.value = _state.value.copy(pendingText = text.take(131_072))
    }

    fun sendPendingText() {
        val text = _state.value.pendingText
        if (text.isBlank()) {
            _state.value = _state.value.copy(error = "请输入要发送的文字")
            return
        }
        if (_state.value.selectedDeviceIds.isEmpty()) {
            _state.value = _state.value.copy(error = "请先选择接收设备")
            return
        }
        viewModelScope.launch {
            _state.value = _state.value.copy(sendingText = true, error = null)
            val messageId = container.sync.sendText(text)
            if (messageId == null) {
                _state.value = _state.value.copy(sendingText = false)
                return@launch
            }
            if (container.webSocket.awaitAck(messageId)) {
                _state.value = _state.value.copy(
                    sendingText = false,
                    pendingText = "",
                )
            } else {
                _state.value = _state.value.copy(
                    sendingText = false,
                    error = "发送超时，文字已保留，请检查设备连接后重试",
                )
            }
        }
    }

    fun enablePrivilegedClipboard() {
        // ADB 桥接只负责系统剪贴板权限；前台服务仍负责 WSS 和进程保活。
        setAutoCopy(true)
        setForegroundService(true)
        if (!container.clipboard.privileged.requestPermissionOrConnect()) {
            _state.value = _state.value.copy(
                error = "请用 USB 连接电脑，并在桌面 ClipBridge 点击“一键恢复”",
            )
        }
    }

    fun copyHistory(text: String) = container.clipboard.copyRemote(text)

    fun toggleFavorite(item: HistoryEntity) {
        viewModelScope.launch { historyDao.setFavorite(item.id, !item.favorite) }
    }

    fun deleteHistory(item: HistoryEntity) {
        viewModelScope.launch { historyDao.delete(item.id) }
    }

    fun clearHistory() {
        viewModelScope.launch { historyDao.clear() }
    }

    fun updateServer(url: String) {
        if (!url.startsWith("https://")) {
            _state.value = _state.value.copy(error = "服务器地址必须使用 HTTPS")
            return
        }
        val normalized = url.trimEnd('/')
        if (normalized == container.settings.serverUrl) return
        container.settings.serverUrl = normalized
        container.webSocket.close()
        container.connectWebSocket()
        refreshDevices()
    }

    fun setAutoCopy(enabled: Boolean) {
        container.settings.autoCopyRemote = enabled
    }

    fun setForegroundService(enabled: Boolean) {
        val application = getApplication<ClipBridgeApplication>()
        container.settings.foregroundService = enabled
        if (enabled) {
            ContextCompat.startForegroundService(
                application,
                Intent(application, SyncForegroundService::class.java),
            )
        } else {
            application.stopService(Intent(application, SyncForegroundService::class.java))
        }
    }

    fun disconnect() {
        container.settings.connectionEnabled = false
        container.webSocket.close()
        _state.value = _state.value.copy(error = "已手动断开，不会自动重连")
    }

    fun reconnect() {
        container.settings.connectionEnabled = true
        container.connectWebSocket()
        _state.value = _state.value.copy(error = "正在重新连接")
    }

    fun setThemeColor(color: String) {
        if (color !in setOf("blue", "green", "purple", "orange", "neutral")) return
        container.settings.themeColor = color
        _state.value = _state.value.copy(themeColor = color)
    }

    fun autoCopyEnabled(): Boolean = container.settings.autoCopyRemote
    fun foregroundServiceEnabled(): Boolean = container.settings.foregroundService
    fun serverUrl(): String = container.settings.serverUrl
    fun deviceName(): String = container.identity.loadOrCreate().name

    fun logout() {
        viewModelScope.launch {
            container.settings.connectionEnabled = false
            container.webSocket.close()
            container.auth.logout()
            _state.value = AppUiState(
                screen = Screen.AUTH,
                themeColor = container.settings.themeColor,
            )
        }
    }

    fun acceptPairingQr(contents: String) {
        viewModelScope.launch {
            runCatching {
                val json = JSONObject(contents)
                require(json.getInt("version") == 1) { "不支持的二维码协议版本" }
                require(json.getLong("expires_at") > System.currentTimeMillis()) {
                    "配对二维码已过期"
                }
                val server = json.getString("server").trimEnd('/')
                require(server == container.settings.serverUrl.trimEnd('/')) {
                    "二维码服务器与当前服务器不一致"
                }
                val token = json.getString("token")
                require(token.length in 40..128) { "配对令牌格式无效" }
                val access = requireNotNull(container.tokenStore.load()).accessToken
                container.api.acceptPairing(access, token)
            }.onSuccess {
                _state.value = _state.value.copy(screen = Screen.DEVICES, error = "配对成功")
                refreshDevices()
            }.onFailure {
                _state.value = _state.value.copy(error = it.message)
            }
        }
    }

}
