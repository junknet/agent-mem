#!/usr/bin/env bash
set -euo pipefail

log() {
  printf '[deploy] %s\n' "$*"
}

die() {
  printf '[deploy] %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    die "缺少命令: $1"
  fi
}

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

REMOTE_TARGET="${1:-${AGENT_MEM_REMOTE_SSH:-}}"
REMOTE_DIR="${AGENT_MEM_REMOTE_DIR:-/opt/memory-mcp}"
REMOTE_PROGRAM="${AGENT_MEM_REMOTE_PROGRAM:-memory-mcp}"
REMOTE_SERVICE="${AGENT_MEM_REMOTE_SERVICE:-memory-mcp}"
REMOTE_PORT="${AGENT_MEM_REMOTE_PORT:-8787}"
REMOTE_SUPERVISOR_DIR="${AGENT_MEM_REMOTE_SUPERVISOR_DIR:-/etc/supervisor/conf.d}"
REMOTE_SUPERVISOR_FILE="${REMOTE_SUPERVISOR_DIR}/${REMOTE_SERVICE}.conf"

LOCAL_OUT_DIR="${REPO_ROOT}/out"
LOCAL_BINARY="${LOCAL_OUT_DIR}/${REMOTE_PROGRAM}"
LOCAL_SUPERVISOR_CONF="${REPO_ROOT}/deploy/memory-mcp.supervisor.conf"

if [[ -z "${REMOTE_TARGET}" ]]; then
  cat >&2 <<'EOF'
用法:
  scripts/deploy_remote.sh <user@host>

可选环境变量:
  AGENT_MEM_REMOTE_SSH              SSH 目标，如 root@47.110.255.240
  AGENT_MEM_REMOTE_DIR              远端项目目录，默认 /opt/memory-mcp
  AGENT_MEM_REMOTE_PROGRAM          远端二进制名，默认 memory-mcp
  AGENT_MEM_REMOTE_SERVICE          Supervisor 服务名，默认 memory-mcp
  AGENT_MEM_REMOTE_PORT             服务端口，默认 8787
  AGENT_MEM_REMOTE_SUPERVISOR_DIR   Supervisor 配置目录，默认 /etc/supervisor/conf.d
EOF
  exit 1
fi

need_cmd go
need_cmd ssh
need_cmd scp
need_cmd curl

[[ -f "${LOCAL_SUPERVISOR_CONF}" ]] || die "未找到 Supervisor 配置: ${LOCAL_SUPERVISOR_CONF}"

mkdir -p "${LOCAL_OUT_DIR}"

log "编译本地二进制"
(
  cd "${REPO_ROOT}/mcp-go"
  go build -o "${LOCAL_BINARY}" ./cmd/agent-mem-mcp
)

TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
REMOTE_TMP_BINARY="${REMOTE_DIR}/out/${REMOTE_PROGRAM}.new"
REMOTE_TMP_SUPERVISOR="${REMOTE_SUPERVISOR_FILE}.new"

log "检查远端目录"
ssh -o BatchMode=yes "${REMOTE_TARGET}" \
  "test -d '${REMOTE_DIR}' && test -d '${REMOTE_DIR}/out' && test -d '${REMOTE_SUPERVISOR_DIR}'"

log "上传二进制与 Supervisor 配置"
scp -q "${LOCAL_BINARY}" "${REMOTE_TARGET}:${REMOTE_TMP_BINARY}"
scp -q "${LOCAL_SUPERVISOR_CONF}" "${REMOTE_TARGET}:${REMOTE_TMP_SUPERVISOR}"

log "远端切换版本并重启服务"
ssh -o BatchMode=yes "${REMOTE_TARGET}" "REMOTE_DIR='${REMOTE_DIR}' REMOTE_PROGRAM='${REMOTE_PROGRAM}' REMOTE_SERVICE='${REMOTE_SERVICE}' REMOTE_PORT='${REMOTE_PORT}' REMOTE_SUPERVISOR_FILE='${REMOTE_SUPERVISOR_FILE}' TIMESTAMP='${TIMESTAMP}' bash -s" <<'EOF'
set -euo pipefail

BINARY_PATH="${REMOTE_DIR}/out/${REMOTE_PROGRAM}"
TMP_BINARY="${BINARY_PATH}.new"
TMP_SUPERVISOR="${REMOTE_SUPERVISOR_FILE}.new"

if [[ -f "${BINARY_PATH}" ]]; then
  cp "${BINARY_PATH}" "${BINARY_PATH}.bak.${TIMESTAMP}"
fi
mv "${TMP_BINARY}" "${BINARY_PATH}"
chmod 755 "${BINARY_PATH}"

if [[ -f "${REMOTE_SUPERVISOR_FILE}" ]]; then
  cp "${REMOTE_SUPERVISOR_FILE}" "${REMOTE_SUPERVISOR_FILE}.bak.${TIMESTAMP}"
fi
mv "${TMP_SUPERVISOR}" "${REMOTE_SUPERVISOR_FILE}"

if [[ -f /etc/systemd/system/agent-mem-mcp.service ]] || [[ -f /usr/lib/systemd/system/agent-mem-mcp.service ]]; then
  systemctl disable --now agent-mem-mcp.service >/dev/null 2>&1 || true
fi

supervisorctl reread >/dev/null
supervisorctl update >/dev/null || true
if ! supervisorctl restart "${REMOTE_SERVICE}" >/dev/null 2>&1; then
  supervisorctl start "${REMOTE_SERVICE}" >/dev/null
fi

sleep 4
supervisorctl status "${REMOTE_SERVICE}"
ss -ltn "( sport = :${REMOTE_PORT} )"

ENV_FILE="${REMOTE_DIR}/.env"
TOKEN=""
if [[ -f "${ENV_FILE}" ]]; then
  TOKEN_LINE="$(grep -m1 '^AGENT_MEM_HTTP_TOKEN=' "${ENV_FILE}" || true)"
  TOKEN="${TOKEN_LINE#AGENT_MEM_HTTP_TOKEN=}"
  TOKEN="${TOKEN%\"}"
  TOKEN="${TOKEN#\"}"
  TOKEN="${TOKEN%\'}"
  TOKEN="${TOKEN#\'}"
fi

HEALTH_URL="http://127.0.0.1:${REMOTE_PORT}/health"
if [[ -n "${TOKEN}" ]]; then
  HEALTH_URL="${HEALTH_URL}?token=${TOKEN}"
fi

curl -fsS --max-time 10 "${HEALTH_URL}"
EOF

log "部署完成"
