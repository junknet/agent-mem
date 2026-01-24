# Project Cortex 架构设计

> **AI Agent 认知资产管理系统 (纯 Go 版)**
> 自动入库、语义检索、版本演进、对话炼金

## 1. 项目定位

Project Cortex（agent-mem）是一个本地优先的知识管理系统，解决「上下文遗忘」问题：
- 文档与对话自动入库
- 语义搜索与意图路由
- 新旧版本替换与历史保留
- 语义连接图（关联需求/设计/实现/复盘）

## 2. 技术栈

- **Core**: Go 1.25+ (Watcher / Ingester / Server)
- **数据库**：PostgreSQL 16 + pgvector
- **向量化**：千问 Embedding（OpenAI 兼容接口）
- **LLM**：Qwen 全家桶（提炼 / 仲裁 / 路由 / 关系抽取）
- **监控**：fsnotify (系统级文件事件)
- **协议**: Model Context Protocol (MCP)

## 3. 数据流（完整版）

1. **Watcher (Go)**：实时监听文件变更 (Create/Write)
2. **Classifier**：推断 doc_type / knowledge_type
3. **Distill**：调用 LLM 将对话提炼为结构化干货
4. **Extract Relations**：抽取引用关系并检索匹配
5. **Embed**：生成向量
6. **Semantic Replace**：基于向量相似度 + LLM 仲裁，执行去重或替换
7. **Save**：写入 PostgreSQL

## 4. 分类体系

**doc_type（文档类型）**
- background / requirements / architecture / design / implementation / progress / testing / deployment / delivery

**knowledge_type（知识类型）**
- doc / insight / dialogue_extract

**insight_type（洞见类型）**
- solution / lesson / pattern / decision

## 5. 语义连接图

通过 `related_ids` 建立弱连接：
- based_on / references / implements / validates / supersedes

例：
- 架构文档基于需求文档（based_on）
- 复盘文档引用故障报告（references）

## 6. 意图路由

系统先判断用户意图，再选择检索策略：
- 进度问题 → progress / issue，限制最近 3 天
- 决策问题 → architecture / insight / background
- 部署问题 → deployment / delivery（必须最新）

## 7. 关键文件结构

- `mcp-go/cmd/agent-mem-mcp/`：核心服务入口
- `mcp-go/cmd/agent-mem-mcp/watcher.go`：文件监控逻辑
- `mcp-go/cmd/agent-mem-mcp/ingest.go`：入库流水线
- `mcp-go/cmd/agent-mem-mcp/llm.go`：LLM Prompt 工程
- `mcp-go/cmd/agent-mem-mcp/db.go`：数据库与向量操作