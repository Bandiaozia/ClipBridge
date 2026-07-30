#!/usr/bin/env bash
set -Eeuo pipefail

base_url="${1:-https://clipbridge.ccttkx.xyz}"
command -v curl >/dev/null || { echo "缺少 curl" >&2; exit 1; }
command -v jq >/dev/null || { echo "缺少 jq" >&2; exit 1; }
command -v openssl >/dev/null || { echo "缺少 openssl" >&2; exit 1; }

response_file="$(mktemp)"
email="smoke-$(date +%s)-$RANDOM@example.invalid"
password="$(openssl rand -base64 24)"
device_id="$(tr -d '\n' </proc/sys/kernel/random/uuid)"
x_public="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')"
sign_public="$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')"
access_token=""

cleanup() {
    if [[ -n "${access_token}" ]]; then
        curl --noproxy '*' --silent --show-error --max-time 15 \
            -X DELETE "${base_url}/api/v1/account" \
            -H "Authorization: Bearer ${access_token}" \
            -H 'Content-Type: application/json' \
            --data "$(jq -cn --arg password "${password}" '{password:$password}')" \
            >/dev/null || true
    fi
    rm -f "${response_file}"
}
trap cleanup EXIT

payload="$(jq -cn \
    --arg email "${email}" \
    --arg password "${password}" \
    --arg id "${device_id}" \
    --arg x "${x_public}" \
    --arg sign "${sign_public}" \
    '{email:$email,password:$password,device:{
      id:$id,name:"Live smoke test",platform:"test",
      x25519_public_key:$x,ed25519_public_key:$sign}}')"

status="$(curl --noproxy '*' --silent --show-error --max-time 20 \
    -o "${response_file}" -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    --data "${payload}" "${base_url}/api/v1/auth/register")"
[[ "${status}" == "201" ]] || {
    echo "注册冒烟测试失败，HTTP ${status}" >&2
    exit 1
}
access_token="$(jq -er '.tokens.access_token' "${response_file}")"

status="$(curl --noproxy '*' --silent --show-error --max-time 15 \
    -o "${response_file}" -w '%{http_code}' \
    -H "Authorization: Bearer ${access_token}" \
    "${base_url}/api/v1/devices")"
[[ "${status}" == "200" ]] || {
    echo "设备接口冒烟测试失败，HTTP ${status}" >&2
    exit 1
}
jq -e --arg id "${device_id}" \
    '.devices | any(.id == $id)' "${response_file}" >/dev/null

echo "ClipBridge 公网注册、认证和设备接口冒烟测试通过"
