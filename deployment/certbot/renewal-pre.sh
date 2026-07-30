#!/usr/bin/env bash
set -Eeuo pipefail

# 首次证书使用 standalone 模式签发，因此续期时需要短暂释放 80 端口。
# 只停止 Nginx，中继服务仍在本机 8080 上运行，避免中断离线消息处理。
cd /opt/clipbridge/deployment
if ! hook_output="$(docker compose --profile tls stop nginx 2>&1)"; then
    printf '%s\n' "${hook_output}" >&2
    exit 1
fi
