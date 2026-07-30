package com.clipbridge.app

import android.Manifest
import android.os.Build
import android.os.Bundle
import android.content.Intent
import android.os.PowerManager
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.core.content.ContextCompat
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.width
import androidx.compose.ui.unit.sp
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.Send
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material.icons.rounded.CloudDone
import androidx.compose.material.icons.rounded.Computer
import androidx.compose.material.icons.rounded.ContentCopy
import androidx.compose.material.icons.rounded.Delete
import androidx.compose.material.icons.rounded.Devices
import androidx.compose.material.icons.rounded.Favorite
import androidx.compose.material.icons.rounded.FavoriteBorder
import androidx.compose.material.icons.rounded.History
import androidx.compose.material.icons.rounded.Home
import androidx.compose.material.icons.rounded.Info
import androidx.compose.material.icons.rounded.QrCodeScanner
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material.icons.rounded.Security
import androidx.compose.material.icons.rounded.Settings
import androidx.compose.material.icons.rounded.Smartphone
import androidx.compose.material.icons.rounded.Sync
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ElevatedCard
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.spring
import androidx.compose.runtime.LaunchedEffect
import kotlinx.coroutines.delay
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import com.clipbridge.app.database.HistoryEntity
import com.clipbridge.app.domain.model.Device
import com.clipbridge.app.presentation.AppUiState
import com.clipbridge.app.presentation.AppViewModel
import com.clipbridge.app.presentation.AuthFormViewModel
import com.clipbridge.app.presentation.Screen
import com.clipbridge.app.presentation.theme.BridgeBlue
import com.clipbridge.app.presentation.theme.BridgeGreen
import com.clipbridge.app.presentation.theme.BridgeIndigo
import com.clipbridge.app.presentation.theme.BridgeOrange
import com.clipbridge.app.presentation.theme.ClipBridgeTheme
import com.clipbridge.app.service.SyncForegroundService
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

class MainActivity : ComponentActivity() {
    private val viewModel: AppViewModel by viewModels()
    private val container get() = (application as ClipBridgeApplication).container

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (Build.VERSION.SDK_INT >= 33) {
            registerForActivityResult(ActivityResultContracts.RequestPermission()) {}
                .launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        if (Build.VERSION.SDK_INT >= 23) {
            val pm = getSystemService(POWER_SERVICE) as PowerManager
            if (!pm.isIgnoringBatteryOptimizations(packageName)) {
                startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
                    data = android.net.Uri.parse("package:$packageName")
                })
            }
        }
        setContent {
            val state by viewModel.state.collectAsStateWithLifecycle()
            ClipBridgeTheme(themeColor = state.themeColor) {
                ClipBridgeApp(viewModel)
            }
        }
        handleAdbBridgeIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleAdbBridgeIntent(intent)
    }

    override fun onStart() {
        super.onStart()
        container.clipboard.startForegroundListening()
    }

    override fun onStop() {
        container.clipboard.stopForegroundListening()
        super.onStop()
    }

    private fun handleAdbBridgeIntent(intent: Intent?) {
        val token = intent?.getStringExtra("clipbridge_adb_token") ?: return
        val port = intent.getIntExtra("clipbridge_adb_port", 39_471)
        if (container.clipboard.privileged.activate(token, port)) {
            // 用户点击桌面端“一键恢复”即明确要求后台互通。这里同时开启自动
            // 写入和前台服务，避免桥接有权限但应用进程退后台后不再转发。
            container.settings.autoCopyRemote = true
            container.settings.foregroundService = true
            ContextCompat.startForegroundService(
                this,
                Intent(this, SyncForegroundService::class.java),
            )
        }
    }
}

private data class NavDestination(
    val label: String,
    val screen: Screen,
    val icon: ImageVector,
)

private val navDestinations = listOf(
    NavDestination("首页", Screen.HOME, Icons.Rounded.Home),
    NavDestination("设备", Screen.DEVICES, Icons.Rounded.Devices),
    NavDestination("历史", Screen.HISTORY, Icons.Rounded.History),
    NavDestination("设置", Screen.SETTINGS, Icons.Rounded.Settings),
)

@Composable
private fun ClipBridgeApp(viewModel: AppViewModel) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val history by viewModel.history.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    LaunchedEffect(state.error) {
        state.error?.let { snackbar.showSnackbar(it) }
    }

    if (state.screen == Screen.AUTH) {
        AuthScreen(state, viewModel)
        return
    }

    Scaffold(
        contentWindowInsets = WindowInsets(0),
        containerColor = MaterialTheme.colorScheme.background,
        topBar = { AppHeader(state) },
        bottomBar = { AppNavigation(state, viewModel) },
        snackbarHost = { SnackbarHost(snackbar) },
    ) { padding ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            when (state.screen) {
                Screen.HOME -> HomeScreen(state, viewModel)
                Screen.DEVICES -> DevicesScreen(state, viewModel)
                Screen.HISTORY -> HistoryScreen(history, viewModel)
                Screen.PAIRING -> PairingScreen(viewModel)
                Screen.SETTINGS -> SettingsScreen(state, viewModel)
                Screen.HELP -> HelpScreen(viewModel)
                Screen.AUTH -> Unit
            }
        }
    }
}

@Composable
private fun AppHeader(state: AppUiState) {
    val title = when (state.screen) {
        Screen.HOME -> "ClipBridge"
        Screen.DEVICES -> "我的设备"
        Screen.HISTORY -> "剪贴板历史"
        Screen.PAIRING -> "添加设备"
        Screen.SETTINGS -> "设置"
        Screen.HELP -> "使用说明"
        Screen.AUTH -> ""
    }
    Surface(
        color = MaterialTheme.colorScheme.background.copy(alpha = 0.96f),
        tonalElevation = 1.dp,
    ) {
        Row(
            modifier = Modifier
                .statusBarsPadding()
                .fillMaxWidth()
                .height(64.dp)
                .padding(horizontal = 20.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(
                    modifier = Modifier
                        .size(32.dp)
                        .clip(RoundedCornerShape(8.dp))
                        .background(MaterialTheme.colorScheme.primary),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "C",
                        color = Color.White,
                        fontWeight = FontWeight.Black,
                        style = MaterialTheme.typography.titleMedium,
                    )
                }
                Spacer(Modifier.size(10.dp))
                Text(
                    title,
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.Bold,
                )
            }
            ConnectionBadge(state.connection)
        }
    }
}

@Composable
private fun ConnectionBadge(connection: String) {
    val connected = connection == "已连接"
    Row(
        modifier = Modifier.padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .size(7.dp)
                .clip(CircleShape)
                .background(if (connected) BridgeGreen else MaterialTheme.colorScheme.error),
        )
        Spacer(Modifier.size(7.dp))
        Text(
            connection,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun AppNavigation(state: AppUiState, viewModel: AppViewModel) {
    NavigationBar(
        modifier = Modifier.navigationBarsPadding(),
        containerColor = MaterialTheme.colorScheme.surface,
        tonalElevation = 5.dp,
    ) {
        navDestinations.forEach { destination ->
            NavigationBarItem(
                selected = state.screen == destination.screen,
                onClick = { viewModel.setScreen(destination.screen) },
                icon = {
                    Icon(destination.icon, contentDescription = destination.label)
                },
                label = { Text(destination.label) },
                colors = NavigationBarItemDefaults.colors(
                    indicatorColor = Color.Transparent,
                    selectedIconColor = MaterialTheme.colorScheme.primary,
                    selectedTextColor = MaterialTheme.colorScheme.primary,
                ),
            )
        }
    }
}

@Composable
private fun AuthScreen(
    state: AppUiState,
    viewModel: AppViewModel,
    formViewModel: AuthFormViewModel = viewModel(),
) {
    val form by formViewModel.state.collectAsStateWithLifecycle()
    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .statusBarsPadding()
            .padding(24.dp),
        contentAlignment = Alignment.Center,
    ) {
        ElevatedCard(
            modifier = Modifier.fillMaxWidth(),
            shape = RoundedCornerShape(12.dp),
            elevation = CardDefaults.elevatedCardElevation(1.dp),
        ) {
            Column(
                modifier = Modifier.padding(26.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Box(
                    modifier = Modifier
                        .size(60.dp)
                        .clip(RoundedCornerShape(12.dp))
                        .background(MaterialTheme.colorScheme.primary),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        Icons.Rounded.Sync,
                        contentDescription = null,
                        tint = Color.White,
                        modifier = Modifier.size(36.dp),
                    )
                }
                Spacer(Modifier.height(18.dp))
                Text(
                    "ClipBridge",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Black,
                )
                Text(
                    if (state.register) "创建你的私人剪贴板桥梁" else "安全连接你的所有设备",
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Spacer(Modifier.height(26.dp))
                OutlinedTextField(
                    value = form.email,
                    onValueChange = formViewModel::updateEmail,
                    label = { Text("邮箱") },
                    singleLine = true,
                    shape = RoundedCornerShape(10.dp),
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(12.dp))
                OutlinedTextField(
                    value = form.password,
                    onValueChange = formViewModel::updatePassword,
                    label = { Text("密码（至少 10 位）") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    shape = RoundedCornerShape(10.dp),
                    modifier = Modifier.fillMaxWidth(),
                )
                state.error?.let {
                    Text(
                        it,
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodySmall,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(top = 10.dp),
                    )
                }
                Spacer(Modifier.height(18.dp))
                Button(
                    enabled = !state.loading,
                    onClick = {
                        if (formViewModel.validationError() == null) {
                            viewModel.authenticate(form.email, form.password)
                        }
                    },
                    shape = RoundedCornerShape(10.dp),
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(54.dp),
                ) {
                    Text(
                        if (state.loading) "正在连接…"
                        else if (state.register) "创建账户" else "登录",
                        fontWeight = FontWeight.Bold,
                    )
                }
                TextButton(
                    onClick = {
                        formViewModel.toggleMode()
                        viewModel.setRegister(!state.register)
                    },
                ) {
                    Text(if (state.register) "已有账户？登录" else "没有账户？立即注册")
                }
            }
        }
    }
}

@Composable
private fun HomeScreen(state: AppUiState, viewModel: AppViewModel) {
    val connected = state.connection == "已连接"
    val onlineDevices = state.devices.filter { it.online && !it.revoked }
    Box(modifier = Modifier.fillMaxSize()) {
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 18.dp, vertical = 22.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item {
                SectionTitle("服务器状态", "")
            }
            item {
                ElevatedCard(
                    shape = RoundedCornerShape(12.dp),
                    elevation = CardDefaults.elevatedCardElevation(1.dp),
                ) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(18.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Box(
                            modifier = Modifier
                                .size(12.dp)
                                .background(
                                    if (connected) Color(0xFF4CAF50) else Color(0xFFBDBDBD),
                                    CircleShape,
                                ),
                        )
                        Spacer(Modifier.width(14.dp))
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                if (connected) "已连接" else state.connection,
                                fontWeight = FontWeight.SemiBold,
                            )
                            Text(
                                "WSS 加密通道",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
            item { SectionTitle("在线设备", "") }
            if (onlineDevices.isEmpty()) {
                item {
                    Text(
                        "暂无在线设备",
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(vertical = 8.dp),
                    )
                }
            } else {
                items(onlineDevices, key = { it.id }) { device ->
                    ElevatedCard(
                        shape = RoundedCornerShape(12.dp),
                        elevation = CardDefaults.elevatedCardElevation(1.dp),
                    ) {
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(16.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Box(
                                modifier = Modifier
                                    .size(8.dp)
                                    .background(BridgeGreen, CircleShape),
                            )
                            Spacer(Modifier.width(12.dp))
                            Text(
                                device.name,
                                modifier = Modifier.weight(1f),
                            )
                        }
                    }
                }
            }
            item { Spacer(Modifier.height(72.dp)) }
        }
        Button(
            onClick = {
                if (connected) viewModel.disconnect() else viewModel.reconnect()
            },
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .padding(18.dp)
                .height(48.dp),
            shape = RoundedCornerShape(12.dp),
            contentPadding = PaddingValues(horizontal = 20.dp),
        ) {
            Icon(
                if (connected) Icons.Rounded.Sync else Icons.Rounded.Refresh,
                contentDescription = null,
                modifier = Modifier.size(20.dp),
            )
            Spacer(Modifier.width(8.dp))
            Text(if (connected) "断开" else "连接", fontSize = 16.sp)
        }
    }
}

@Composable
private fun RuntimeStatusCard(
    icon: ImageVector,
    title: String,
    status: String,
    detail: String,
    healthy: Boolean,
) {
    ElevatedCard(
        shape = RoundedCornerShape(12.dp),
        elevation = CardDefaults.elevatedCardElevation(1.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(18.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            DeviceGlyph(icon)
            Column(
                modifier = Modifier
                    .weight(1f)
                    .padding(horizontal = 14.dp),
            ) {
                Text(title, fontWeight = FontWeight.SemiBold)
                Text(
                    detail,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Column(horizontalAlignment = Alignment.End) {
                Box(
                    Modifier
                        .size(8.dp)
                        .clip(CircleShape)
                        .background(
                            if (healthy) BridgeGreen else MaterialTheme.colorScheme.outline,
                        ),
                )
                Spacer(Modifier.height(6.dp))
                Text(
                    status,
                    style = MaterialTheme.typography.labelMedium,
                    color = if (healthy) BridgeGreen
                    else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun DeviceGlyph(icon: ImageVector, color: Color = MaterialTheme.colorScheme.primary) {
    Box(
        modifier = Modifier
            .size(40.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(color.copy(alpha = 0.12f)),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = null, tint = color)
    }
}

@Composable
private fun SectionTitle(title: String, subtitle: String) {
    Column {
        Text(
            title,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Bold,
        )
        Text(
            subtitle,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun DevicesScreen(state: AppUiState, viewModel: AppViewModel) {
    LaunchedEffect(Unit) { viewModel.refreshDevices() }
    val onlineDevices = state.devices.filter { it.online }
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(18.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                SectionTitle(
                    "在线设备",
                    "共 ${onlineDevices.size} 台在线，点击选择接收目标（可多选）",
                )
                IconButton(onClick = viewModel::refreshDevices) {
                    Icon(Icons.Rounded.Refresh, contentDescription = "刷新设备")
                }
            }
        }
        if (onlineDevices.isEmpty()) {
            item {
                EmptyState(
                    icon = Icons.Rounded.Devices,
                    title = "还没有其他设备",
                    subtitle = "在电脑端登录同一账户，或扫描电脑上的配对二维码。",
                    action = "扫描二维码",
                    onAction = { viewModel.setScreen(Screen.PAIRING) },
                )
            }
        } else {
            items(onlineDevices, key = Device::id) { device ->
                DeviceCard(
                    device = device,
                    selected = device.id in state.selectedDeviceIds,
                    onClick = { viewModel.toggleDevice(device.id) },
                )
            }
        }
        item {
            Spacer(Modifier.height(4.dp))
            SectionTitle("发送文字", "端到端加密，同时发送到所有选中设备")
            Spacer(Modifier.height(10.dp))
            ElevatedCard(
                shape = RoundedCornerShape(12.dp),
                elevation = CardDefaults.elevatedCardElevation(1.dp),
            ) {
                Column(modifier = Modifier.padding(16.dp)) {
                    OutlinedTextField(
                        value = state.pendingText,
                        onValueChange = viewModel::updatePendingText,
                        modifier = Modifier.fillMaxWidth(),
                        label = { Text("输入要发送的文字") },
                        placeholder = { Text("例如：地址、链接或一段文字") },
                        minLines = 3,
                        maxLines = 6,
                        shape = RoundedCornerShape(10.dp),
                    )
                    Spacer(Modifier.height(12.dp))
                    Button(
                        onClick = viewModel::sendPendingText,
                        enabled = state.pendingText.isNotBlank() &&
                            state.selectedDeviceIds.isNotEmpty() &&
                            !state.sendingText,
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(50.dp),
                        shape = RoundedCornerShape(10.dp),
                    ) {
                        if (state.sendingText) {
                            CircularProgressIndicator(
                                modifier = Modifier.size(20.dp),
                                strokeWidth = 2.dp,
                                color = MaterialTheme.colorScheme.onPrimary,
                            )
                            Spacer(Modifier.width(10.dp))
                            Text("发送中…")
                        } else {
                            Icon(Icons.AutoMirrored.Rounded.Send, contentDescription = null)
                            Spacer(Modifier.size(8.dp))
                            Text("发送到 ${state.selectedDeviceIds.size} 台设备")
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun DeviceCard(device: Device, selected: Boolean, onClick: () -> Unit) {
    ElevatedCard(
        onClick = onClick,
        shape = RoundedCornerShape(12.dp),
        colors = CardDefaults.elevatedCardColors(
            containerColor = if (selected) {
                MaterialTheme.colorScheme.primaryContainer.copy(alpha = 0.68f)
            } else {
                MaterialTheme.colorScheme.surface
            },
        ),
        elevation = CardDefaults.elevatedCardElevation(1.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            DeviceGlyph(
                if (device.platform.contains("android", true)) {
                    Icons.Rounded.Smartphone
                } else {
                    Icons.Rounded.Computer
                },
                MaterialTheme.colorScheme.primary,
            )
            Column(
                modifier = Modifier
                    .weight(1f)
                    .padding(horizontal = 14.dp),
            ) {
                Text(device.name, fontWeight = FontWeight.Bold)
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Box(
                        Modifier
                            .size(7.dp)
                            .clip(CircleShape)
                            .background(if (device.online) BridgeGreen else Color.Gray),
                    )
                    Spacer(Modifier.size(6.dp))
                    Text(
                        "${if (device.online) "在线" else "离线"} · ${device.platform}",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            RadioButton(selected = selected, onClick = onClick)
        }
    }
}

@Composable
private fun HistoryScreen(history: List<HistoryEntity>, viewModel: AppViewModel) {
    var confirmClear by remember { mutableStateOf(false) }
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(18.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    "${history.size} 条记录",
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.labelLarge,
                )
                TextButton(
                    enabled = history.isNotEmpty(),
                    onClick = { confirmClear = true },
                ) {
                    Text("清空", color = MaterialTheme.colorScheme.error)
                }
            }
        }
        if (history.isEmpty()) {
            item {
                EmptyState(
                    icon = Icons.Rounded.History,
                    title = "还没有同步记录",
                    subtitle = "复制并同步的非敏感文字会显示在这里。",
                )
            }
        } else {
            items(history, key = HistoryEntity::id) { item ->
                HistoryCard(item, viewModel)
            }
        }
    }
    if (confirmClear) {
        AlertDialog(
            onDismissRequest = { confirmClear = false },
            title = { Text("清空剪贴板历史？") },
            text = { Text("所有记录都会从本机删除，此操作不可撤销。") },
            confirmButton = {
                TextButton(onClick = {
                    viewModel.clearHistory()
                    confirmClear = false
                }) { Text("清空", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = {
                TextButton(onClick = { confirmClear = false }) { Text("取消") }
            },
        )
    }
}

@Composable
private fun HistoryCard(item: HistoryEntity, viewModel: AppViewModel) {
    val time = remember(item.createdAt) {
        SimpleDateFormat("MM-dd HH:mm", Locale.getDefault()).format(Date(item.createdAt))
    }
    ElevatedCard(
        shape = RoundedCornerShape(12.dp),
        elevation = CardDefaults.elevatedCardElevation(1.dp),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable { viewModel.copyHistory(item.content) }
                .padding(16.dp),
            verticalAlignment = Alignment.Top,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    item.content,
                    maxLines = 3,
                    overflow = TextOverflow.Ellipsis,
                    style = MaterialTheme.typography.bodyMedium,
                )
                Spacer(Modifier.height(6.dp))
                Text(
                    time,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.width(8.dp))
            IconButton(
                onClick = { viewModel.toggleFavorite(item) },
                modifier = Modifier.size(32.dp),
            ) {
                Icon(
                    if (item.favorite) Icons.Rounded.Favorite else Icons.Rounded.FavoriteBorder,
                    contentDescription = null,
                    tint = if (item.favorite) BridgeOrange
                    else MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
    }
}

@Composable
private fun PairingScreen(viewModel: AppViewModel) {
    val scanner = rememberLauncherForActivityResult(ScanContract()) { result ->
        result.contents?.let(viewModel::acceptPairingQr)
    }
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(22.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(
            modifier = Modifier
                .size(88.dp)
                .clip(RoundedCornerShape(12.dp))
                .background(MaterialTheme.colorScheme.primaryContainer),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                Icons.Rounded.QrCodeScanner,
                contentDescription = null,
                modifier = Modifier.size(56.dp),
                tint = MaterialTheme.colorScheme.primary,
            )
        }
        Spacer(Modifier.height(24.dp))
        Text(
            "扫描电脑上的二维码",
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.Bold,
        )
        Text(
            "二维码使用五分钟有效的一次性令牌，不包含密码、私钥或长期访问令牌。",
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(vertical = 12.dp),
        )
        Button(
            onClick = {
                scanner.launch(
                    ScanOptions()
                        .setDesiredBarcodeFormats(ScanOptions.QR_CODE)
                        .setPrompt("扫描 ClipBridge 桌面端二维码")
                        .setBeepEnabled(false),
                )
            },
            modifier = Modifier
                .fillMaxWidth()
                .height(54.dp),
            shape = RoundedCornerShape(10.dp),
        ) {
            Icon(Icons.Rounded.QrCodeScanner, contentDescription = null)
            Spacer(Modifier.size(8.dp))
            Text("打开相机扫码")
        }
        TextButton(onClick = { viewModel.setScreen(Screen.DEVICES) }) { Text("返回设备列表") }
    }
}

@Composable
private fun SettingsScreen(state: AppUiState, viewModel: AppViewModel) {
    var server by remember { mutableStateOf(viewModel.serverUrl()) }
    var autoCopy by remember { mutableStateOf(viewModel.autoCopyEnabled()) }
    var foreground by remember { mutableStateOf(viewModel.foregroundServiceEnabled()) }
    var confirmLogout by remember { mutableStateOf(false) }
    val privileged = state.clipboardMode.contains("已启用")
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(18.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        item {
            SettingsCard("外观", "主题颜色会立即生效并保存在本机") {
                ThemeColorSelector(
                    selected = state.themeColor,
                    onSelected = viewModel::setThemeColor,
                )
            }
        }
        item {
            SettingsCard("连接", "中继服务器和后台互通") {
                OutlinedTextField(
                    value = server,
                    onValueChange = { server = it },
                    label = { Text("HTTPS 服务器地址") },
                    singleLine = true,
                    shape = RoundedCornerShape(10.dp),
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(10.dp))
                FilledTonalButton(
                    onClick = { viewModel.updateServer(server) },
                    modifier = Modifier.fillMaxWidth(),
                    shape = RoundedCornerShape(10.dp),
                ) { Text("保存服务器地址") }
                Spacer(Modifier.height(10.dp))
                if (state.connection == "已连接") {
                    OutlinedButton(
                        onClick = viewModel::disconnect,
                        modifier = Modifier.fillMaxWidth(),
                        shape = RoundedCornerShape(10.dp),
                    ) {
                        Text("断开连接")
                    }
                } else {
                    Button(
                        onClick = viewModel::reconnect,
                        modifier = Modifier.fillMaxWidth(),
                        shape = RoundedCornerShape(10.dp),
                    ) {
                        Text("重新连接")
                    }
                }
            }
        }
        item {
            SettingsCard("自动化", "控制收到内容后的行为") {
                SettingsSwitch(
                    title = "自动写入手机剪贴板",
                    subtitle = "远端非敏感文字到达后立即可粘贴",
                    checked = autoCopy,
                    onCheckedChange = {
                        autoCopy = it
                        viewModel.setAutoCopy(it)
                    },
                )
                HorizontalDivider()
                SettingsSwitch(
                    title = "保持后台连接",
                    subtitle = "使用前台服务维持 WSS 和 ADB 桥接连接",
                    checked = foreground,
                    onCheckedChange = {
                        foreground = it
                        viewModel.setForegroundService(it)
                    },
                )
            }
        }
        item {
            SettingsCard("ADB 后台桥接", state.clipboardMode) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    DeviceGlyph(
                        Icons.Rounded.Security,
                        if (privileged) BridgeGreen else MaterialTheme.colorScheme.primary,
                    )
                    Column(
                        modifier = Modifier
                            .weight(1f)
                            .padding(start = 12.dp),
                    ) {
                        Text(
                            if (privileged) "后台剪贴板权限正常" else "需要连接或授权",
                            fontWeight = FontWeight.Bold,
                        )
                        Text(
                            if (privileged) "ClipBridge shell 服务正在监听文字变化"
                            else "手机重启后请连接电脑并点击一键恢复",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                Spacer(Modifier.height(12.dp))
                OutlinedButton(
                    onClick = viewModel::enablePrivilegedClipboard,
                    modifier = Modifier.fillMaxWidth(),
                    shape = RoundedCornerShape(10.dp),
                ) { Text(if (privileged) "重新检查连接" else "查看连接状态") }
            }
        }
        item {
            SettingsCard("账户", "登录状态和本机身份") {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    DeviceGlyph(Icons.Rounded.Smartphone)
                    Column(
                        modifier = Modifier
                            .weight(1f)
                            .padding(start = 12.dp),
                    ) {
                        Text(viewModel.deviceName(), fontWeight = FontWeight.Bold)
                        Text(
                            "当前登录设备",
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
                Spacer(Modifier.height(14.dp))
                OutlinedButton(
                    onClick = { confirmLogout = true },
                    modifier = Modifier.fillMaxWidth(),
                    shape = RoundedCornerShape(10.dp),
                    colors = ButtonDefaults.outlinedButtonColors(
                        contentColor = MaterialTheme.colorScheme.error,
                    ),
                ) { Text("退出登录") }
            }
        }
        item {
            TextButton(
                onClick = { viewModel.setScreen(Screen.HELP) },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Icon(Icons.Rounded.Info, contentDescription = null)
                Spacer(Modifier.size(7.dp))
                Text("查看权限与后台运行说明")
            }
        }
    }
    if (confirmLogout) {
        AlertDialog(
            onDismissRequest = { confirmLogout = false },
            title = { Text("退出当前账户？") },
            text = { Text("本机 WebSocket 将断开，安全存储中的登录令牌会被撤销。") },
            confirmButton = {
                TextButton(
                    onClick = {
                        confirmLogout = false
                        viewModel.logout()
                    },
                ) { Text("退出", color = MaterialTheme.colorScheme.error) }
            },
            dismissButton = {
                TextButton(onClick = { confirmLogout = false }) { Text("取消") }
            },
        )
    }
}

@Composable
private fun SettingsCard(
    title: String,
    subtitle: String,
    content: @Composable ColumnScope.() -> Unit,
) {
    ElevatedCard(
        shape = RoundedCornerShape(12.dp),
        elevation = CardDefaults.elevatedCardElevation(1.dp),
    ) {
        Column(modifier = Modifier.padding(18.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(15.dp))
            content()
        }
    }
}

private data class ThemeChoice(
    val id: String,
    val label: String,
    val color: Color,
)

private val themeChoices = listOf(
    ThemeChoice("blue", "蓝色", BridgeBlue),
    ThemeChoice("green", "绿色", BridgeGreen),
    ThemeChoice("purple", "紫色", BridgeIndigo),
    ThemeChoice("orange", "橙色", BridgeOrange),
    ThemeChoice("neutral", "中性", Color(0xFF555B66)),
)

@Composable
private fun ThemeColorSelector(
    selected: String,
    onSelected: (String) -> Unit,
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        themeChoices.forEach { choice ->
            Column(
                modifier = Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(8.dp))
                    .clickable { onSelected(choice.id) }
                    .padding(vertical = 4.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(34.dp)
                        .clip(RoundedCornerShape(7.dp))
                        .background(choice.color),
                    contentAlignment = Alignment.Center,
                ) {
                    if (selected == choice.id) {
                        Icon(
                            Icons.Rounded.CheckCircle,
                            contentDescription = "已选择",
                            tint = Color.White,
                            modifier = Modifier.size(20.dp),
                        )
                    }
                }
                Spacer(Modifier.height(5.dp))
                Text(
                    choice.label,
                    style = MaterialTheme.typography.labelSmall,
                    color = if (selected == choice.id) {
                        MaterialTheme.colorScheme.primary
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                )
            }
        }
    }
}

@Composable
private fun SettingsSwitch(
    title: String,
    subtitle: String,
    checked: Boolean,
    onCheckedChange: (Boolean) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable { onCheckedChange(!checked) }
            .padding(vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(title, fontWeight = FontWeight.SemiBold)
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(checked = checked, onCheckedChange = onCheckedChange)
    }
}

@Composable
private fun HelpScreen(viewModel: AppViewModel) {
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(18.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item {
            HelpCard(
                Icons.Rounded.Security,
                "后台自动互通",
                "通过 USB 连接电脑，在桌面 ClipBridge 点击“一键恢复”。不需要安装 Shizuku，也不需要 Root。",
            )
        }
        item {
            HelpCard(
                Icons.Rounded.CloudDone,
                "保持在线",
                "允许 ClipBridge 自启动，将电池策略设为“不限制”，并保留前台服务通知。",
            )
        }
        item {
            HelpCard(
                Icons.Rounded.ContentCopy,
                "隐私保护",
                "剪贴板文字在本机加密后发送，服务器只能看到路由元数据和密文。疑似密码、令牌和私钥默认阻止自动发送。",
            )
        }
        item {
            HelpCard(
                Icons.AutoMirrored.Rounded.Send,
                "备用发送入口",
                "ADB 桥接未启动时，可以在其他应用选择文字后使用系统分享，或使用快捷设置磁贴手动发送。",
            )
        }
        item {
            OutlinedButton(
                onClick = { viewModel.setScreen(Screen.HOME) },
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(10.dp),
            ) { Text("返回首页") }
        }
    }
}

@Composable
private fun HelpCard(icon: ImageVector, title: String, text: String) {
    ElevatedCard(
        shape = RoundedCornerShape(12.dp),
        elevation = CardDefaults.elevatedCardElevation(1.dp),
    ) {
        Row(modifier = Modifier.padding(18.dp)) {
            DeviceGlyph(icon)
            Column(modifier = Modifier.padding(start = 14.dp)) {
                Text(title, fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(5.dp))
                Text(
                    text,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
        }
    }
}

@Composable
private fun EmptyState(
    icon: ImageVector,
    title: String,
    subtitle: String,
    action: String? = null,
    onAction: (() -> Unit)? = null,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 46.dp, horizontal = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        DeviceGlyph(icon)
        Spacer(Modifier.height(14.dp))
        Text(title, fontWeight = FontWeight.Bold, style = MaterialTheme.typography.titleMedium)
        Text(
            subtitle,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
        )
        if (action != null && onAction != null) {
            Spacer(Modifier.height(14.dp))
            FilledTonalButton(
                onClick = onAction,
                shape = RoundedCornerShape(10.dp),
            ) { Text(action) }
        }
    }
}
