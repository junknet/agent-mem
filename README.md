# Agent Memory (Project Cortex)

> **赋予 AI Agent 长期记忆与自我进化能力**
>
> 一个基于 **Go** 实现的轻量级、高性能知识库中间件。支持 LLM 触发入库、智能语义更新与 MCP 协议。

---

## 核心特性

*   **极速架构**: 纯 Go 实现，单二进制文件，资源占用极低
*   **LLM 触发入库**: 由端上 LLM/客户端在合适时机调用 `mem.ingest_memory`
*   **认知智能**:
    *   **查询扩展**: 自动扩展搜索关键词提升召回率
    *   **LLM 仲裁**: 两层冲突检测（向量筛选 + LLM 判断），智能决策 REPLACE/KEEP_BOTH/SKIP
    *   **单一真相**: 同主题新知识自动替换旧知识
*   **标准接口**: 原生支持 **Model Context Protocol (MCP)**，无缝对接 Claude Desktop, Cursor, Gemini CLI
*   **极客哲学**: 硬删除模式，旧知识直接物理抹除，拒绝数据膨胀

## 架构概览

```mermaid
graph LR
    User["LLM / Client"] --> Ingester["Ingest Pipeline"]

    subgraph CoreLogic ["Core Logic"]
        Ingester --> Summarize["Summarize"]
        Summarize --> Tag["Extract Tags"]
        Tag --> Chunk["Chunk"]
        Chunk --> Embed["Vectorize"]
        Embed --> Arbiter["LLM Arbitrate"]
    end

    Arbiter --> DB[("PostgreSQL + pgvector")]

    User["Claude / Cursor"] -- MCP Protocol --> Server["MCP Server"]
    Server --> DB
```

## 快速开始

### 1. 依赖准备

确保已安装 [Docker](https://www.docker.com/) 和 [Go 1.25+](https://go.dev/)。

```bash
# 启动 PostgreSQL (带 pgvector 扩展)
docker-compose up -d
```

### 2. 配置环境

```bash
cp .env.example .env
vim .env
```

```env
DASHSCOPE_API_KEY=sk-xxxxxxxxxxxx
DATABASE_URL=postgresql://cortex:cortex_password_secure@localhost:5440/cortex_knowledge
AGENT_MEM_HTTP_TOKEN=your-strong-token
AGENT_MEM_OWNER_ID=personal
```

### 3. 编译与运行

```bash
# 编译
cd mcp-go && go build -o ../agent-mem ./cmd/agent-mem-mcp && cd ..

# HTTP 模式
./agent-mem --transport http --host 127.0.0.1 --port 8787

# STDIO 模式（MCP 客户端）
./agent-mem --transport stdio

# 重置数据库（首次或升级时）
./agent-mem --reset-db --reset-only
```

## PATH 工具（开箱即用）

一键安装到 PATH（默认 `/usr/local/bin`）：

```bash
./bin/agent-mem-install
```

安装后可直接使用：

```bash
agent-mem              # 启动 MCP 服务（前台）
agent-mem-reset-db     # 清空并重建数据库
agent-mem-status       # 查看端口状态
agent-mem-stop         # 停止服务
```

## 内容类型（严格互斥）

| 类型 | 英文 | 定义 | 写入时机 |
|:---:|:---:|:---|:---|
| 需求功能 | `requirement` | PRD、功能描述、业务规则 | 讨论/确认需求后 |
| 计划任务 | `plan` | 任务清单、TODO、里程碑 | 制定开发计划后 |
| 开发 | `development` | 架构设计、API定义、技术方案 | 设计/实现确定后 |
| 测试验收 | `testing` | 测试计划、用例、验收报告 | 测试/验收完成后 |
| 经验沉淀 | `insight` | 踩坑记录、最佳实践、注意事项 | 遇到问题并解决后 |

## MCP 工具

| 工具 | 说明 | 返回状态 |
|:---|:---|:---|
| `mem.ingest_memory` | 写入记忆 | `created`(新ID) / `updated`(旧ID) / `skipped`(已存在ID) |
| `mem.search` | 语义检索 | 片段列表 |
| `mem.get` | 获取全文 | 完整内容 |
| `mem.timeline` | 时间线查询 | 按时间排序 |
| `mem.list_projects` | 项目列表 | 项目摘要 |

## HTTP 接口

- `POST /ingest/memory` - 写入记忆
- `GET /memories/search` - 语义检索
- `GET /memories` - 获取全文
- `GET /memories/timeline` - 时间线
- `GET /projects` - 项目列表
- `/sse` - SSE 传输（MCP）
- `/mcp` - Streamable HTTP（MCP）

## 冲突检测机制

```
新内容入库
    │
    ▼
┌──────────────────┐
│ 向量相似度筛选   │ ── 找出语义相似的已有知识
└────────┬─────────┘
         │ similarity > 0.85
         ▼
┌──────────────────┐
│  LLM 智能仲裁    │ ── 比较新旧摘要，判断关系
└────────┬─────────┘
         │
    ┌────┼────┬────┐
    ▼    ▼    ▼    ▼
 REPLACE  KEEP   SKIP
 (替换)  _BOTH  (跳过)
        (保留)
```

- **REPLACE**: 同主题更新，替换旧知识
- **KEEP_BOTH**: 不同主题，保留两者
- **SKIP**: 完全重复，跳过写入

## 升级与迁移

### 从旧版本升级

系统启动时会自动执行迁移脚本：
- 自动添加 `avg_embedding` 字段
- 自动添加 `summary`、`tags` 字段（如缺失）
- 已有索引不受影响

**注意**：旧数据（无 `avg_embedding`）不会参与冲突检测，建议：
1. 使用 `--reset-db --reset-only` 重建数据库（推荐，数据量小时）
2. 或手动 backfill 旧数据的向量

### 首次部署

```bash
./agent-mem --reset-db --reset-only
```

## 客户端接入

### Claude Desktop / Code

编辑 `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "agent-mem": {
      "command": "/absolute/path/to/agent-mem",
      "args": ["--transport", "stdio"]
    }
  }
}
```

### 访问令牌（可选）

如果设置了 `AGENT_MEM_HTTP_TOKEN`，HTTP/SSE/MCP 请求必须携带令牌：
- Header：`Authorization: Bearer <token>`
- 或 Header：`X-Agent-Mem-Token: <token>`
- 或 URL：`/sse?token=<token>`、`/mcp?token=<token>`、`/memories/search?token=<token>`

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

如启用 token，把 `url` 改为：`http://127.0.0.1:8787/sse?token=<token>`。

### Cursor (Beta)

- **Type**: SSE
- **URL**: `http://127.0.0.1:8787/sse`

## 全局提示词

详见 `docs/LLM_PROMPTS.md`，包含：
- 完整的提示词模板
- 5 个最佳实践示例
- 类型选择决策树

## 配置说明

`config/settings.yaml`:

```yaml
versioning:
  semantic_similarity_threshold: 0.85  # 触发仲裁的相似度阈值

embedding:
  provider: qwen
  model: text-embedding-v4
  dimension: 1536

chunking:
  chunk_size: 500
  overlap: 50
```

## 开发指南

```bash
# 运行单元测试
cd mcp-go/cmd/agent-mem-mcp && go test -v

# 运行 E2E 测试
python scripts/e2e_test_go.py

# 集成测试（需要数据库和 API Key）
AGENT_MEM_INTEGRATION=1 go test -v
```

## License

MIT
