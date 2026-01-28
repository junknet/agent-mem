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
- ts（不传默认当前时间）

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
