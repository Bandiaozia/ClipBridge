#!/usr/bin/env bash
set -Eeuo pipefail

# 无论 Certbot 是否真的更新了证书，恢复 Nginx 都是安全且幂等的。
cd /opt/clipbridge/deployment
if ! hook_output="$(docker compose --profile tls start nginx 2>&1)"; then
    printf '%s\n' "${hook_output}" >&2
    exit 1
fi
