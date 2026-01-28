#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

echo "=========================================="
echo "  Agent Memory - Starting Services"
echo "=========================================="

if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    echo "[1/3] 启动数据库（Docker Compose）..."
    docker compose up -d
  else
    echo "[1/3] 未检测到 docker compose，跳过数据库启动"
  fi
else
  echo "[1/3] 未检测到 Docker，跳过数据库启动"
fi

echo "[2/3] 编译 MCP 服务..."
mkdir -p out
(cd mcp-go && go build -o ../out/agent-mem-mcp ./cmd/agent-mem-mcp)

echo "[3/3] 启动 MCP 服务..."
AGENT_MEM_HOST="${AGENT_MEM_HOST:-127.0.0.1}"
AGENT_MEM_PORT="${AGENT_MEM_PORT:-8787}"
AGENT_MEM_TRANSPORT="${AGENT_MEM_TRANSPORT:-http}"

./out/agent-mem-mcp --host "$AGENT_MEM_HOST" --port "$AGENT_MEM_PORT" --transport "$AGENT_MEM_TRANSPORT"
