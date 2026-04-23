# MCP 服务（Agent Memory）

本项目提供 MCP 服务端，统一给 Claude / Gemini / Codex 等客户端使用，入口统一为 Go 实现。

## 启动方式

```bash
cd mcp-go
go build -o ../out/agent-mem-mcp ./cmd/agent-mem-mcp
../out/agent-mem-mcp --host 127.0.0.1 --port 8787 --transport http
```

说明：
- `http` 同时开启 `/sse` 与 `/mcp`
- `stdio` 适合本地调试：`../out/agent-mem-mcp --transport stdio`

## Supervisor 常驻部署

这台机器建议统一由 `supervisor` 托管，而不是混用 `systemd` 与交互式 shell：

```bash
sudo cp deploy/memory-mcp.supervisor.conf /etc/supervisor/conf.d/memory-mcp.conf
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl restart memory-mcp
```

关键点：
- `autostart=true`：机器重启或 `supervisord` 重启后自动拉起，避免服务长期离线
- `autorestart=true`：进程异常退出后自动恢复
- `stopasgroup=true` / `killasgroup=true`：确保整组进程一起退出，避免残留占端口
- `AGENT_TOOLS_ENV` 与 `AGENT_MEM_CONFIG`：统一读取 `.env` 和配置文件

## 一键远端部署

本地构建、上传、切换二进制、刷新 `supervisor`、重启并做健康检查：

```bash
scripts/deploy_remote.sh root@47.110.255.240
```

可选环境变量：
- `AGENT_MEM_REMOTE_DIR`：远端目录，默认 `/opt/memory-mcp`
- `AGENT_MEM_REMOTE_SERVICE`：Supervisor 服务名，默认 `memory-mcp`
- `AGENT_MEM_REMOTE_PORT`：服务端口，默认 `8787`

脚本会额外处理两件事：
- 自动备份远端旧二进制与旧 `memory-mcp.conf`
- 如果发现旧的 `agent-mem-mcp.service`，会停止并禁用，避免和 `supervisor` 抢同一个端口

## 工具说明

### mem.ingest_memory

写入记忆（服务端自动摘要、标签与向量化）。

必填字段：
- project_name（或 project_key）
- content_type（requirement|plan|development|testing|insight）
- content

可选字段：
- owner_id（不传默认使用服务端配置）
- project_key（跨机器稳定项目标识）
- machine_name / project_path（仅做来源记录）
- ts（不传默认当前时间；兼容秒/毫秒/微秒/纳秒输入）

### mem.search

语义检索（混合向量 + 关键词 + BM25），返回 snippet + id。

### mem.get

按 ID 批量拉取完整内容。

### mem.timeline

按时间窗口拉取摘要列表。

### mem.list_projects

列出当前 owner 下的项目。

## 客户端配置示例

### Claude Code（`~/.claude/mcp.json`）

```json
{
  "mcpServers": {
    "agent-mem": {
      "url": "http://127.0.0.1:8787/sse"
    }
  }
}
```

### Codex CLI（`~/.codex/config.toml`）

```toml
[mcp_servers.agent-mem]
url = "http://127.0.0.1:8787/sse"
```

### Gemini CLI（`~/.gemini/config.yaml`）

```yaml
mcpServers:
  agent-mem:
    url: http://127.0.0.1:8787/sse
```

如需使用 `streamable-http`，将 `url` 改为 `http://127.0.0.1:8787/mcp`。

## 依赖说明

确保：
1) PostgreSQL（`docker-compose up -d`）
2) 已配置 `DASHSCOPE_API_KEY` 与 `DATABASE_URL`（可放在 `~/.config/agent_tools.env`）

## Token 访问（可选）

如果设置 `AGENT_MEM_HTTP_TOKEN`，所有 HTTP/SSE/MCP 请求需携带 token：
- Header：`Authorization: Bearer <token>` 或 `X-Agent-Mem-Token: <token>`
- URL：`/sse?token=<token>` 或 `/mcp?token=<token>`
