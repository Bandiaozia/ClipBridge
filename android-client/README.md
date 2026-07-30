# ClipBridge Android

Android 10（API 29）及以上。当前客户端包含：

- Compose / Material 3 登录、注册、首页、设备、扫码配对、历史和设置页面
- 首页仅展示中继服务器、目标电脑与本机后台互通的实时状态
- 设备页可直接输入文字，收到目标设备 ACK 后提示成功并清空输入框
- 蓝、绿、紫、橙和中性五种可持久保存的主题颜色
- OkHttp REST 与 WebSocket、心跳、指数退避、Token 刷新、ACK 和重发
- Room 历史、消息 ID 去重、WorkManager 定期清理
- Android Keystore AES-GCM 包装令牌和设备私钥
- libsodium X25519、XChaCha20-Poly1305 与 Ed25519 端到端加密
- 前台剪贴板监听、系统分享入口、快捷设置磁贴和可选前台服务
- 用户通过桌面 ADB 启动的自有后台文字剪贴板桥接
- 远端通知的“复制”“忽略”操作，敏感内容默认隐藏

构建与测试：

```bash
export JAVA_HOME=/path/to/jdk-17-or-newer
export ANDROID_HOME=/path/to/android-sdk
./gradlew testDebugUnitTest assembleDebug assembleDebugAndroidTest
```

调试 APK 位于 `app/build/outputs/apk/debug/app-debug.apk`。连接 Android 设备后可运行：

```bash
adb install -r app/build/outputs/apk/debug/app-debug.apk
./gradlew connectedDebugAndroidTest
```

## 后台自动互通

Android 10 起，普通应用只有在当前获得焦点或作为默认输入法时才能读取剪贴板。常规模式
严格遵守该限制，只在 Activity 前台监听；后台仍可使用系统分享或快捷设置磁贴。

私人、自行安装场景可以主动启用 ADB 桥接模式：

1. 在电脑安装 Android Platform Tools。
2. 使用数据线连接手机并允许 USB 调试。
3. 小米/HyperOS 还需在开发者选项同时开启“USB 调试”和
   “USB 调试（安全设置）”。
4. 在桌面 ClipBridge 点击“一键恢复”。
5. 保持 ClipBridge 的前台服务和系统自启动/后台运行权限开启。

该模式不 Root、不修改系统分区，也不需要安装 Shizuku。独立进程以用户已授权的 ADB
shell 身份访问系统剪贴板，并通过带 256 位随机令牌认证的本机回环通道把文字交给普通
应用进程加密发送。Android 10～16 的
`IClipboard` 参数会在运行时适配，不硬编码 Binder 事务编号。轮询线程可正常停止，
默认每 350 ms 检查一次，只在文本变化时回传。

手机重启后 ADB shell 桥接需要重新激活；此时 ClipBridge 会明确显示
“ADB 后台桥接未启动”，而不是错误显示后台同步正常。桥接权限意味着应用能够看到复制的
文字，务必只给可信、自行构建的 APK 授权。敏感内容检测仍默认阻止自动发送，但它不是
百分之百可靠。

收到远端内容默认发送通知；开启后台互通后会同时启用“自动写入手机剪贴板”。疑似敏感
内容不会自动复制或写入历史。

桌面配对二维码是五分钟有效的一次性令牌，只包含服务器地址、设备 ID、公钥与过期时间。
首次使用时，先在一端注册账户，另一端登录同一私人账户，再通过设备页选择目标。
