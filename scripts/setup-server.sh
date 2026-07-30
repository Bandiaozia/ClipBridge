#!/usr/bin/env bash
set -Eeuo pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
deploy_dir="${project_dir}/deployment"
env_file="${deploy_dir}/.env"

fail() { echo "错误：$*" >&2; exit 1; }
info() { echo "==> $*"; }

command -v docker >/dev/null || fail "未安装 Docker：https://docs.docker.com/engine/install/ubuntu/"
docker compose version >/dev/null 2>&1 || fail "未安装 Docker Compose 插件"
docker info >/dev/null 2>&1 || fail "Docker daemon 未运行或当前用户无访问权限"
command -v curl >/dev/null || fail "缺少 curl"
command -v ss >/dev/null || fail "缺少 iproute2（ss）"
[[ -f "${env_file}" ]] || fail "请先复制 deployment/.env.example 为 deployment/.env 并填写"

set -a
# shellcheck disable=SC1090
source "${env_file}"
set +a

[[ -n "${CLIPBRIDGE_DOMAIN:-}" ]] || fail "CLIPBRIDGE_DOMAIN 不能为空"
[[ "${CLIPBRIDGE_DOMAIN}" != "clipbridge.example.com" ]] || fail "请替换示例域名"
[[ "${#CLIPBRIDGE_JWT_SECRET}" -ge 32 ]] || fail "CLIPBRIDGE_JWT_SECRET 至少 32 字符"
[[ "${CLIPBRIDGE_JWT_SECRET}" != replace-* ]] || fail "请替换示例 JWT Secret"

if ! getent ahosts "${CLIPBRIDGE_DOMAIN}" | awk '{print $1}' | grep -qxE '([0-9a-fA-F:.]+)'; then
    fail "域名无法解析：${CLIPBRIDGE_DOMAIN}"
fi

mkdir -p "${deploy_dir}/data" "${deploy_dir}/certbot-www"
chmod 600 "${env_file}"
if [[ "$(stat -c '%u:%g' "${deploy_dir}/data")" != "10001:10001" ]]; then
    if [[ "${EUID}" -eq 0 ]]; then
        chown 10001:10001 "${deploy_dir}/data"
    elif command -v sudo >/dev/null && sudo -n true 2>/dev/null; then
        sudo chown 10001:10001 "${deploy_dir}/data"
    else
        fail "数据目录必须归容器用户 10001:10001；请运行 sudo chown 10001:10001 deployment/data"
    fi
fi
chmod 700 "${deploy_dir}/data"

http_port="${CLIPBRIDGE_HTTP_PORT:-8080}"
running_relay="$(cd "${deploy_dir}" && docker compose ps -q relay 2>/dev/null || true)"
if [[ -z "${running_relay}" ]] && ss -H -ltn "sport = :${http_port}" | grep -q .; then
    fail "本机端口 ${http_port} 已被占用，请修改 CLIPBRIDGE_HTTP_PORT"
fi

info "校验 Compose 配置"
(cd "${deploy_dir}" && docker compose config --quiet)
info "构建并启动 relay"
(cd "${deploy_dir}" && docker compose up -d --build relay)

for attempt in $(seq 1 30); do
    if curl --fail --silent --max-time 2 "http://127.0.0.1:${http_port}/ready" >/dev/null; then
        info "ClipBridge relay 已就绪"
        exit 0
    fi
    sleep 2
done

(cd "${deploy_dir}" && docker compose logs --tail=100 relay)
fail "服务在 60 秒内未通过健康检查"
