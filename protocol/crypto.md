# ClipBridge 端到端加密协议

## 1. 目标与威胁模型

协议保护剪贴板正文不被中继、数据库备份或被动网络观察者读取。中继仍能看到完成路由所需
的用户、设备、时间、消息类型、大小和在线状态。已解锁且被攻陷的终端、系统剪贴板管理器
以及用户主动复制到其他应用的内容不在保护范围内。

所有整数在签名/认证数据中使用十进制 JSON 数字，字符串使用 UTF-8。规范化数据必须按
下面固定字段顺序生成，禁止直接依赖语言运行时的 map 序列化顺序。

## 2. 密钥生成与保存

每台设备首次启动生成两组独立密钥：

- X25519 密钥对：只用于设备间共享秘密协商。
- Ed25519 密钥对：只用于签署设备身份和配对证明。

随机数必须来自操作系统 CSPRNG。桌面端使用 libsodium；Android 使用受审计密码库并由
Android Keystore 中不可导出的 AES-GCM 包装本地私钥。桌面端优先使用 Windows Credential
Manager 或 Linux Secret Service；不可用时，以操作系统凭据派生/保存的包装密钥加密配置，
文件权限必须是 `0600`。私钥、共享秘密和恢复材料不得进入日志、二维码或服务端。

设备公钥采用无填充 Base64URL。服务端仅保存公钥，用于配对目录和撤销判断。

## 3. 设备配对

1. 已登录桌面端调用 `/pairing/create`，中继生成 256 位一次性随机令牌，只保存
   `SHA-256(token)`，默认五分钟过期。
2. 二维码包含协议版本、服务器 HTTPS 地址、令牌、发起设备 ID、两种公钥及过期时间。
   不包含密码、私钥、Access Token 或 Refresh Token。
3. 已登录 Android 扫码，检查版本、HTTPS 地址、过期时间和字段长度，再通过 TLS 调用
   `/pairing/accept`，提交令牌、本设备 ID 和公钥。
4. 服务端在单个事务中核对令牌摘要、用户、发起设备、过期和未使用状态，随后立即标记
   `used_at`，并返回双方公开资料。并发的第二次接受必然失败。
5. 双方使用 Ed25519 签署
   `clipbridge-pair-v1 || token_hash || initiator_id || acceptor_id || x25519_public_keys`。
   客户端必须验证对方签名，验证失败则丢弃结果。

拒绝配对同样原子地消费令牌，避免之后被再次接受。

## 4. 密钥派生

双方执行 X25519 得到 `shared_secret`，随后使用 HKDF-SHA256：

```text
salt = SHA-256("ClipBridge pairing v1" || min(device_id) || max(device_id))
prk  = HKDF-Extract(salt, shared_secret)
send_key(A->B) = HKDF-Expand(prk, "ClipBridge message v1" || A || B, 32)
send_key(B->A) = HKDF-Expand(prk, "ClipBridge message v1" || B || A, 32)
```

方向密钥彼此独立。共享秘密在派生后立即从内存清零；落盘只保存经安全存储包装的派生密钥
和对方公钥。不同协议用途必须使用不同 `info`，不得复用消息密钥加密配置或数据库。

## 5. 加密

正文结构为 UTF-8 JSON：

```json
{"text":"example","content_sha256":"base64url","sensitive":false}
```

先构造认证元数据（AAD），字段顺序固定：

```text
version\nmessage_id\nsender_device_id\nrecipient_device_id\ntype\ncreated_at\nexpires_at
```

使用 XChaCha20-Poly1305-IETF、32 字节方向密钥和每条消息新生成的 24 字节随机 nonce 加密。
nonce 与密文使用无填充 Base64URL 传输。即使正文相同也必须生成新 nonce。客户端签署：

```text
Ed25519.Sign(sender_sign_private, SHA-256(AAD || nonce || ciphertext))
```

签名让收件人在设备目录公钥被恶意替换之外仍能验证发送设备身份；AEAD 标签保证正文和
路由元数据不可被篡改。服务端拒绝正文相关字段，无法获得内容哈希。

## 6. 解密与验证

收件人按以下顺序处理，任何一步失败都只记录脱敏错误：

1. 确认版本、收件设备、发送设备未撤销、时间范围和 UUID 格式。
2. 以消息 ID 查询本地去重表；已经成功处理则仅重发 ACK。
3. 使用固定规则重建 AAD，验证 Ed25519 签名。
4. Base64URL 解码并检查 nonce 恰为 24 字节、密文不超过配置上限。
5. 使用对应方向密钥执行 XChaCha20-Poly1305 解密；认证标签错误时丢弃且不 ACK。
6. 校验 UTF-8、JSON 字段和正文 SHA-256；成功后在本地事务中写历史和去重记录。
7. 提交事务后 ACK。写入系统剪贴板时设置本地抑制标志，避免回传。

过期消息不得解密展示，但可以发送终态 ACK 让服务端清理。

## 7. 密钥轮换

设备每 90 天或用户手动触发轮换。新公钥用旧 Ed25519 密钥签署，并由对端确认后切换；
轮换期间保留上一代解密密钥至所有旧消息过期，最长一小时。旧签名密钥仅用于验证已在途
消息，不再签署新消息。若旧密钥不可用，必须重新扫码配对，不能静默信任服务端下发的新键。

## 8. 设备撤销

撤销操作使设备状态立即变为 revoked，关闭其 WebSocket，撤销该设备的全部 Refresh
Token，并删除发给它或由它发送的暂存密文及未使用配对令牌。在线客户端收到
`device_revoked` 后删除与该设备的方向密钥；离线客户端下一次刷新设备列表时执行相同处理。
被撤销设备重新加入必须生成新设备 ID 和全新密钥并重新配对。撤销不能让攻击者解密此前
已取得的密文，因此敏感消息应使用短有效期。

