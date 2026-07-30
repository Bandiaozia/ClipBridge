# REST API v1

请求与响应均为 `application/json`，请求体上限 64 KiB。除注册、登录、刷新和健康检查外，
均使用 `Authorization: Bearer <access-token>`。成功响应直接返回资源；错误统一为：

```json
{"error":{"code":"invalid_request","message":"请求无效","request_id":"uuid"}}
```

接口：

- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/logout`
- `POST /api/v1/auth/change-password`
- `GET /api/v1/account`
- `DELETE /api/v1/account`
- `GET /api/v1/devices`
- `DELETE /api/v1/devices/{deviceId}`
- `POST /api/v1/pairing/create`
- `POST /api/v1/pairing/accept`
- `POST /api/v1/pairing/reject`
- `GET /health`
- `GET /ready`

注册/登录可携带设备描述与 X25519、Ed25519 公钥。Refresh Token 仅在注册、登录或刷新
时返回；刷新采用轮换策略，旧 Token 在事务内立即撤销。

