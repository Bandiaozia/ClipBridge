package com.clipbridge.app.settings

import android.content.Context

class SettingsStore(context: Context) {
    private val preferences =
        context.getSharedPreferences("clipbridge_settings", Context.MODE_PRIVATE)

    var serverUrl: String
        get() = preferences.getString(
            "server_url",
            "https://clipbridge.ccttkx.xyz",
        ) ?: "https://clipbridge.ccttkx.xyz"
        set(value) = preferences.edit().putString("server_url", value.trimEnd('/')).apply()

    var autoCopyRemote: Boolean
        // ClipBridge 的核心承诺是两端剪贴板互通。非敏感内容默认自动落入系统
        // 剪贴板；敏感内容仍由同步协调器拦截，不会因为此默认值而自动写入。
        get() = preferences.getBoolean("auto_copy_remote", true)
        set(value) = preferences.edit().putBoolean("auto_copy_remote", value).apply()

    var foregroundService: Boolean
        // Android 会积极回收普通后台进程。ADB 桥接只解决系统剪贴板权限，
        // 前台服务负责让应用侧的 WSS 与桥接客户端持续存活。
        get() = preferences.getBoolean("foreground_service", true)
        set(value) = preferences.edit().putBoolean("foreground_service", value).apply()

    var connectionEnabled: Boolean
        get() = preferences.getBoolean("connection_enabled", true)
        set(value) = preferences.edit().putBoolean("connection_enabled", value).apply()

    var maxHistory: Int
        get() = preferences.getInt("max_history", 2000)
        set(value) = preferences.edit().putInt("max_history", value.coerceIn(10, 100_000)).apply()

    var retentionDays: Int
        get() = preferences.getInt("retention_days", 1)
        set(value) = preferences.edit().putInt("retention_days", value.coerceIn(1, 3650)).apply()

    var selectedDeviceIds: Set<String>
        get() = preferences.getString("selected_device_ids", "")
            ?.takeIf { it.isNotEmpty() }
            ?.split(",")
            ?.toSet() ?: emptySet()
        set(value) = preferences.edit().putString("selected_device_ids", value.joinToString(",")).apply()

    /**
     * 只保存稳定的主题标识，不保存具体色值。这样后续微调配色时，用户的选择仍然有效。
     */
    var themeColor: String
        get() = preferences.getString("theme_color", "purple") ?: "purple"
        set(value) = preferences.edit().putString("theme_color", value).apply()
}
