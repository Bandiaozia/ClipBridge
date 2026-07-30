# ClipBridge

端到端加密的跨平台剪贴板同步系统。在你的设备之间安全地共享剪贴板内容，中继服务器只存储密文，无法接触到明文。

End-to-end encrypted cross-platform clipboard sync. Share clipboard content securely across your devices — the relay server never sees plaintext.

## 架构 Architecture

```
┌─────────────────┐     WebSocket / REST      ┌─────────────────┐
│  Desktop Client  │ ◄──────────────────────► │   Relay Server   │
│  (Qt6 / C++20)  │        E2E Encrypted      │   (Go 1.26)     │
└────────┬────────┘                           └────────┬────────┘
         │                                             │
         │ ADB USB                                     │ WebSocket / REST
         │ (loopback)                                  │
┌────────┴────────┐                                    │
│  Android Client  │ ◄─────────────────────────────────┘
│  (Kotlin/Compose)│              E2E Encrypted
└─────────────────┘
```

## 组件 Components

| 目录 | 技术栈 | 说明 |
|------|--------|------|
| `relay-server/` | Go 1.26 · SQLite · WebSocket | 消息中继，只存密文不接触明文 |
| `desktop-client/` | C++20 · Qt 6 · libsodium | 桌面剪贴板同步，系统托盘，ADB 桥接 |
| `android-client/` | Kotlin · Compose · Room | Android 剪贴板同步，前台服务，快捷磁贴 |
| `protocol/` | 文档 | E2EE 加密协议、REST API、WebSocket 规范 |
| `deployment/` | Docker · Nginx · systemd | 生产环境部署配置 |
| `scripts/` | Shell · PowerShell | 构建、部署、冒烟测试脚本 |

## 安全设计 Security

- **密钥交换**: X25519 ECDH，每设备独立密钥对
- **加密**: XChaCha20-Poly1305 AEAD，每消息独立 nonce
- **签名**: Ed25519 设备身份签名，防篡改
- **密码哈希**: Argon2id
- **令牌**: JWT HS256 访问令牌 + SHA-256 摘要刷新令牌
- **配对**: 一次性 256 位随机令牌，仅存 SHA-256 摘要
- **传输**: TLS 保护客户端到中继通道
- **存储**: Windows Credential Manager / Linux `crypto_secretbox` / Android Keystore AES-GCM
- 服务端拒绝接收任何 `plaintext`/`content`/`text` 字段
- 完整协议见 [`protocol/crypto.md`](protocol/crypto.md)

## 快速开始 Quick Start

### 中继服务器

```bash
cp deployment/.env.example deployment/.env
# 修改 JWT_SECRET（至少 32 字节随机值）
./scripts/setup-server.sh
cd deployment && docker compose up -d
curl http://127.0.0.1:8080/health
```

开发环境：

```bash
cd relay-server
go test ./...
go build ./cmd/server
CLIPBRIDGE_JWT_SECRET='replace-with-at-least-32-random-bytes' ./server
```

### 桌面客户端

```bash
# Ubuntu / Debian
sudo apt install qt6-base-dev qt6-websockets-dev libsodium-dev libqrencode-dev
cd desktop-client && mkdir build && cd build
cmake .. && make -j$(nproc)
```

### Android 客户端

```bash
cd android-client
./gradlew testDebugUnitTest assembleDebug
# APK: app/build/outputs/apk/debug/app-debug.apk
```

## 冒烟测试 Smoke Test

```bash
./scripts/smoke-live.sh https://your-relay.example.com
```

创建随机临时账户，验证注册、认证和设备接口，退出时自动删除。

## 许可证 License

MIT — 详见 [LICENSE](LICENSE)
