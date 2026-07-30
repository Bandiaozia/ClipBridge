# ClipBridge Relay Server

Go 1.26 中继实现，提供账户、设备、一次性配对、WebSocket 密文转发、离线暂存与自动清理。

## 本地运行

```bash
export CLIPBRIDGE_JWT_SECRET="$(openssl rand -base64 48)"
go test ./...
go run ./cmd/server
```

默认监听 `:8080`，数据库为 `./data/clipbridge.db`。生产环境使用
`deployment/docker-compose.yml`，不要直接暴露 HTTP 端口。

## 关键环境变量

- `CLIPBRIDGE_LISTEN`：监听地址，默认 `:8080`
- `CLIPBRIDGE_DATABASE_PATH`：SQLite 路径
- `CLIPBRIDGE_JWT_SECRET`：必填，至少 32 字符
- `CLIPBRIDGE_ALLOWED_ORIGIN`：允许的浏览器 Origin；原生客户端不发 Origin
- `CLIPBRIDGE_MAX_QUEUED_MESSAGES`：每用户暂存条数，默认 1000
- `CLIPBRIDGE_MAX_QUEUED_BYTES`：每用户暂存密文字节数，默认 10 MiB
- `CLIPBRIDGE_LOG_LEVEL`：`debug/info/warn/error`

## 排错

- `/health` 只确认进程存活；`/ready` 同时检查数据库。
- `database is locked`：确认数据目录没有被多个 Compose 项目挂载；服务内部只开一个 SQLite
  连接并启用 WAL。
- WebSocket 4001：Access Token 过期或设备已撤销。
- WebSocket 4008：用户配额或连接发送队列已满。

日志为 JSON，设计上不记录密码、Token、公私钥、nonce、密文或剪贴板正文。

