# CLAUDE.md

本文件提供给 Claude Code 的仓库使用说明。

## 项目概览

Agent Memory（Project Cortex）是纯 Go 实现的 MCP 记忆中枢，提供：
- LLM 触发入库与两阶段检索
- 语义更新（替换旧记忆，单一真相）
- 摘要/标签/向量化/仲裁服务端自动完成

## 架构要点

```
LLM/Client → Ingest Pipeline → [Summarize → Tag → Chunk → Embed → Arbitrate] → PostgreSQL
                                                                                  ↓
User (via MCP) ← Server ← Searcher ←----------------------------------------- pgvector
```

**核心目录**（均在 `mcp-go/cmd/agent-mem-mcp/`）：
- `app.go`：应用组装与 MCP Server
- `ingest.go`：入库与冲突仲裁
- `search.go`：混合检索（向量/关键词/BM25）
- `db.go`：数据库 + 向量索引
- `llm.go`：Qwen 摘要/标签/仲裁
- `embedder.go`：向量生成
- `chunking.go`：切分逻辑
- `validation.go`：入参校验

## 构建与运行

```bash
# 启动 PostgreSQL（可选）
docker compose up -d

# 编译
cd mcp-go && go build -o ../agent-mem ./cmd/agent-mem-mcp && cd ..

# HTTP 模式（含 /sse 与 /mcp）
./agent-mem --transport http --host 127.0.0.1 --port 8787

# STDIO 模式（MCP 客户端）
./agent-mem --transport stdio

# 重建数据库（清空）
./agent-mem --reset-db --reset-only
```

## 内容类型（严格互斥）

| 类型 | 英文 | 定义 |
|:---:|:---:|:---|
| 需求功能 | `requirement` | PRD、功能描述、业务规则 |
| 计划任务 | `plan` | 任务清单、TODO、里程碑 |
| 开发 | `development` | 架构设计、API定义、技术方案 |
| 测试验收 | `testing` | 测试计划、用例、验收报告 |
| 经验沉淀 | `insight` | 踩坑记录、最佳实践、注意事项 |

## 配置与环境变量

- `DATABASE_URL`：PostgreSQL 连接串
- `DASHSCOPE_API_KEY`：Qwen API Key
- `AGENT_MEM_HTTP_TOKEN`：HTTP/SSE/MCP 访问令牌（可选）

## 测试

```bash
# 单元测试
cd mcp-go/cmd/agent-mem-mcp && go test -v

# E2E
python scripts/e2e_test_go.py
```
