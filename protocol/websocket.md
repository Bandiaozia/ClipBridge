# WebSocket 协议 v1

## 连接

地址为 `wss://host/api/v1/ws`，子协议 `clipbridge.v1`。连接建立后客户端必须在 10 秒内
发送 `auth`；Access Token 不放在 URL，以免进入代理日志。

```json
{"type":"auth","access_token":"...","device_id":"uuid","last_sequence":0}
```

成功返回 `auth_ok`，包含服务端时间、心跳间隔和当前连接序号；失败返回 `auth_error` 后以
策略码 4001 关闭。Token 即将过期时客户端通过 REST 刷新，再发送新的 `auth`。

## 通用信封

```json
{
  "version":1,
  "type":"clipboard_text",
  "message_id":"uuid",
  "sender_device_id":"uuid",
  "recipient_device_id":"uuid",
  "created_at":1710000000000,
  "expires_at":1710000600000,
  "nonce":"base64url",
  "ciphertext":"base64url",
  "signature":"base64url"
}
```

中继只接受 `clipboard_text` 密文消息；出现 `text`、`content`、`plaintext` 字段时拒绝。
消息 ID 由发送方生成。创建时间允许与服务器相差最多五分钟；有效期最大一小时，默认十分钟。

## 类型

- `auth` / `auth_ok` / `auth_error`
- `heartbeat` / `heartbeat_ack`：每 25 秒，连续两次无响应则重连
- `clipboard_text`：端到端加密正文
- `message_ack`：`{"type":"message_ack","message_id":"uuid","status":"processed"}`
- `device_online` / `device_offline`
- `pair_request` / `pair_accept` / `pair_reject`：配对状态通知，公开资料仍由 REST 事务产生
- `device_revoked`
- `error`：包含稳定错误码和 `request_id`，不回显敏感输入

## 投递、确认与去重

服务端先以 `(recipient_device_id,message_id)` 插入离线表，再尝试在线发送，因此进程崩溃
不会造成已接受消息丢失。收件人完成验证和本地事务后发送 ACK；服务端核对 ACK 设备确为
收件人后立即删除密文，并向发送方发送同一 ACK。

网络异常时发送方使用同一消息 ID 指数退避重发（1、2、4、8、16、30 秒，上限 30 秒并加入
0–20% 抖动）。唯一约束使重发幂等。客户端至少保存最近 4096 个消息 ID；重复消息不再次
写剪贴板，但重发 ACK。

服务端为每条入队消息分配单调递增 `sequence`。重连时 `auth.last_sequence` 只用于诊断；
服务端仍投递该设备所有未 ACK 且未过期消息，按 sequence 升序。端到端语义是“至少一次、
按中继接收顺序”，不同发送设备之间不保证因果顺序。

## 心跳、重连与网络切换

WebSocket Ping/Pong 与应用层 heartbeat 同时使用。客户端在断网、网络切换或非正常关闭后
指数退避重连，退避上限 60 秒；恢复网络时可以立即尝试一次。正常注销使用 1000 关闭码并
撤销 Refresh Token。所有写操作设置 10 秒截止时间，REST 请求默认 15 秒超时。

## 离线保存与限额

有效期可选 1 分钟、10 分钟、1 小时；“不保存”要求接收方不在线时返回
`recipient_offline`，不入库。每用户默认最多 1000 条或 10 MiB 密文，先达到者生效。
清理任务至少每分钟删除过期记录。ACK、设备撤销和账户删除立即删除相应记录。

## 关闭码

- `4001`：认证失败或 Token 过期
- `4003`：设备已撤销或无权限
- `4008`：速率/配额超限
- `4009`：同一设备的新连接替换旧连接
- `4010`：协议错误

