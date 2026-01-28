# Project Cortex 架构设计

> **AI Agent 认知资产管理系统（Go 版 MCP）**
> 目标：统一入库、语义更新、两阶段检索

## 1. 项目定位

Project Cortex（agent-mem）是一个本地优先的知识管理系统，解决「上下文遗忘」问题：
- LLM 通过 MCP 主动触发入库与检索
- 语义检索 + 关键词 + BM25 混合排序
- 语义更新覆盖旧记忆（P0 唯一真理）

## 2. 技术栈

- **Core**：Go 1.25+
- **数据库**：PostgreSQL 16 + pgvector
- **向量**：Qwen Embedding（OpenAI 兼容）
- **LLM**：Qwen（摘要 / 标签 / 仲裁 / 扩展 / 重排）
- **协议**：MCP（HTTP/SSE/stdio）

## 3. 数据流

1. LLM/客户端调用 `mem.ingest_memory`
2. 片段切分 + 向量化
3. 计算 memory 级 avg_embedding
4. 冲突检测（向量粗筛）
5. LLM 仲裁（REPLACE / KEEP_BOTH / SKIP）
6. 写入/替换 memories + fragments

检索链路：
- `mem.search` → 返回 snippet + id
- `mem.get` → 拉取完整内容

## 4. 分类体系

**content_type（五类）**：
- requirement / plan / development / testing / insight

> 冲突检测不按类型过滤，跨类型更新允许替换。

## 5. 关键文件结构

- `mcp-go/cmd/agent-mem-mcp/`：核心服务入口
- `mcp-go/cmd/agent-mem-mcp/ingest.go`：入库 + 冲突仲裁
- `mcp-go/cmd/agent-mem-mcp/db.go`：数据库 + 向量索引
- `mcp-go/cmd/agent-mem-mcp/search.go`：混合检索
