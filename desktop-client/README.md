# ClipBridge Desktop

桌面端提供登录/注册、WSS 自动重连、X25519 + XChaCha20-Poly1305
端到端加密、剪贴板防回环同步、SQLite 历史、敏感内容确认和系统托盘。
主窗口采用适配高 DPI 的 Qt Widgets 双栏布局，设置中可选择蓝、绿、紫、橙或中性
主题色；主题选择会保存在本机。

Ubuntu 24.04 依赖：

```bash
sudo apt install cmake ninja-build g++ qt6-base-dev qt6-websockets-dev \
    libsodium-dev libqrencode-dev
../scripts/build-linux.sh
```

若当前账户没有 sudo，可将缺失的 Debian 包解压到仓库 `.deps/root`，构建脚本会自动发现。
Windows 使用 Qt 6.8+、Visual Studio 2022，并通过 vcpkg 安装 libsodium 与 libqrencode，
然后运行
`.\scripts\build-windows.ps1`。

首次启动会生成设备 ID、X25519 与 Ed25519 密钥。Windows 私钥保存到 Credential
Manager；Linux 在 Secret Service 不可用时使用仅当前用户可读写的加密配置文件。默认服务
地址是 `https://clipbridge.ccttkx.xyz`。

## USB 一键恢复手机互通

手机重启后，ADB shell 桥接会停止。使用数据线连接并在手机上允许 USB 调试后，
桌面首页的“USB 一键恢复手机互通”会：

1. 检测已授权的 ADB 手机；
2. 从已安装的 ClipBridge APK 启动自带的 shell 剪贴板桥接；
3. 使用一次性随机令牌连接手机 App，并恢复后台互通。

桌面端会从 `PATH`、`ANDROID_HOME`、`ANDROID_SDK_ROOT` 和常见 Android SDK
目录寻找 ADB。此功能不会绕过手机上的 USB 调试确认，也不需要 Root。若未安装 Platform
Tools，请从 Android 官方渠道安装后再点击。手机不需要安装 Shizuku，也不需要 Root。
