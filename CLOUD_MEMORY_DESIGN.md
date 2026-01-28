# 云记忆中心设计规划文档

> **版本**：v2.0
> **日期**：2026-01-26
> **状态**：已实施（与当前代码一致）

---

## 📋 目录

1. [架构概览](#1-架构概览)
2. [核心设计决策](#2-核心设计决策)
3. [数据模型](#3-数据模型)
4. [API 接口规范](#4-api-接口规范)
5. [MCP 工具定义](#5-mcp-工具定义)
6. [提示词规范](#6-提示词规范)
7. [工作流示例](#7-工作流示例)
8. [实施计划](#8-实施计划)
9. [常见问题](#9-常见问题)

---

## 1. 架构概览

### 1.1 整体设计

```
┌─────────────────────┐
│  Codex/Gemini/Claude │
│   (AI Agent)         │
└──────────┬──────────┘
           │ MCP 调用
           ▼
┌─────────────────────────────────┐
│      agent-mem 云中心            │
│  ┌──────────────────────────┐   │
│  │ mem.ingest_memory        │   │
│  │ mem.search               │   │  两阶段检索
│  │ mem.get                  │   │  (search→get)
│  │ mem.timeline             │   │
│  │ mem.list_projects        │   │
│  └──────────────────────────┘   │
│                                  │
│  ┌──────────────────────────┐   │
│  │ 内部处理（AI 无感知）      │   │
│  │ • 片段切分               │   │
│  │ • 向量化                 │   │
│  │ • 摘要/标签              │   │
│  │ • 混合检索               │   │
│  │ • 语义仲裁               │   │
│  └──────────────────────────┘   │
└──────────────┬───────────────────┘
               │
               ▼
        ┌──────────────┐
        │  PostgreSQL  │
        │  + pgvector  │
        └──────────────┘
```

### 1.2 核心原则

- **极简接口**：只传必填字段，服务端完成分析与入库
- **强约束**：检索默认限定 owner_id，可选限定 project_key
- **两阶段检索**：search 返回 snippet，get 再取完整内容
- **服务端自动化**：摘要、标签、向量化、仲裁全在服务端
- **AI 无负担**：提示词流程固定，LLM 只按指令调用
- **P0 唯一真理**：同主题更新直接覆盖旧记忆，不保留旧版本

---

## 2. 核心设计决策

### 2.1 个人标识

使用 `owner_id` 作为个人唯一标识，所有机器共享同一份记忆。

### 2.2 会话模型

一个 `project_key` = 一个会话：

```
唯一会话键 = owner_id + project_key
示例：personal::agent-mem
```

### 2.3 检索策略（P0）

固定两阶段流程（不可跳过）：

```
第 1 阶段：mem.search
  输入：query, owner_id, project_key?, scope, limit
  输出：snippet 列表（200字以内）

第 2 阶段：mem.get
  输入：ids（来自 search）
  输出：完整 content
```

### 2.4 片段切分

固定 tokens + 重叠：

```yaml
chunking:
  strategy: fixed_tokens
  chunk_size: 500
  overlap: 50
```

### 2.5 冲突检测与更新

**P0 规则**：同主题内容要替换旧记忆，旧版本进入历史表保留。

两层冲突检测：
1) **向量粗筛**：`memories.avg_embedding`（memory 级向量）
2) **LLM 仲裁**：比较新旧摘要，输出 REPLACE / KEEP_BOTH / SKIP

> 关键说明：不再使用 `content_hash` 做“完全重复”判断，hash 只用于追踪，不参与冲突判定。

### 2.6 内容类型（统一五类）

```
requirement  → 需求与约束
plan         → 计划/排期/里程碑
development  → 实现方案/代码细节/架构实现
testing      → 测试/验证/回归
insight      → 经验/教训/模式/决策
```

> 冲突检测 **不按 content_type 过滤**，跨类型更新允许替换。

---

## 3. 数据模型

### 3.1 Projects 表

```sql
CREATE TABLE projects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id TEXT NOT NULL,
  project_key TEXT NOT NULL,
  project_name TEXT NOT NULL,
  machine_name TEXT,
  project_path TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(owner_id, project_key)
);
```

### 3.2 Memories 表

```sql
CREATE TABLE memories (
  id TEXT PRIMARY KEY,                     -- mem_xxx
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  content_type TEXT NOT NULL,              -- 五类枚举
  content TEXT NOT NULL,
  content_hash TEXT,                       -- 仅追踪，不做去重
  ts BIGINT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW(),

  -- 服务端自动生成
  summary TEXT,
  tags JSONB,
  chunk_count INT DEFAULT 1,
  embedding_done BOOLEAN DEFAULT false,
  avg_embedding VECTOR(1536)               -- memory 级向量
);

CREATE INDEX idx_memories_avg_embedding ON memories USING hnsw (avg_embedding vector_cosine_ops);
```

### 3.3 Fragments 表

```sql
CREATE TABLE fragments (
  id TEXT PRIMARY KEY,
  memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  chunk_index INT NOT NULL,
  content TEXT NOT NULL,
  embedding VECTOR(1536),
  ts TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(memory_id, chunk_index)
);

CREATE INDEX idx_fragments_embedding ON fragments USING hnsw (embedding vector_cosine_ops);
```

### 3.4 Memory Versions 表（历史版本）

```sql
CREATE TABLE memory_versions (
  id BIGSERIAL PRIMARY KEY,
  memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  content_type TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT,
  ts BIGINT NOT NULL,
  summary TEXT,
  tags JSONB,
  chunk_count INT DEFAULT 1,
  avg_embedding VECTOR(1536),
  created_at TIMESTAMPTZ,
  replaced_at TIMESTAMPTZ DEFAULT NOW()
);
```

### 3.5 Memory Arbitrations 表（仲裁日志）

```sql
CREATE TABLE memory_arbitrations (
  id BIGSERIAL PRIMARY KEY,
  owner_id TEXT NOT NULL,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  candidate_memory_id TEXT,
  new_memory_id TEXT,
  action TEXT NOT NULL,
  similarity DOUBLE PRECISION,
  old_summary TEXT,
  new_summary TEXT,
  model TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
```

---

## 4. API 接口规范

### 4.1 写入：POST /ingest/memory

**请求字段（必填）**：
- project_name（或 project_key）
- content_type（五类枚举）
- content

**请求字段（可选）**：
- owner_id（不传默认使用服务端配置）
- project_key（跨机器稳定项目标识）
- machine_name / project_path（仅记录来源）
- ts（Unix 秒，不传默认当前时间）

**响应**：
- status: created / updated / skipped
- id: 记忆 ID（skipped 时为已存在 ID）

### 4.2 检索：GET /memories/search

**请求字段**：
- owner_id（可选）
- project_key / project_name（可选，不传则全局检索）
- query（必填）
- scope（可选，默认 all；枚举同 content_type）
- limit（可选，默认 20）

**返回**：snippet 列表 + `next_action=use_ids_to_call_mem_get`

### 4.3 取全文：GET /memories

**请求字段**：
- ids（逗号分隔的 memory_id）

### 4.4 其他
- GET /projects
- GET /memories/timeline

---

## 5. MCP 工具定义

### 5.1 mem.ingest_memory

必填字段与 HTTP 一致：

```json
{
  "owner_id": "personal",
  "project_name": "agent-mem",
  "content_type": "development",
  "content": "# 设计决策...",
  "ts": 1768912345
}
```

### 5.2 mem.search

```json
{
  "owner_id": "personal",
  "project_name": "agent-mem",
  "query": "为什么用 pgvector",
  "scope": "development",
  "limit": 10
}
```

### 5.3 mem.get

```json
{
  "ids": ["mem_xxx", "mem_yyy"]
}
```

---

## 6. 提示词规范

- **必须固定流程**：search → get → 生成回答
- **content_type 必须五选一**：requirement | plan | development | testing | insight
- **禁止传 source**（已废弃）

详细提示词参考：`docs/LLM_PROMPTS.md`

---

## 7. 工作流示例

### 7.1 语义更新（跨类型允许）

1) 写入需求：`content_type=requirement`
2) 后续实现细节更新：`content_type=development`
3) 冲突检测命中，LLM 仲裁为 REPLACE
4) 旧记忆被新内容覆盖（content_type 也更新）

### 7.2 两阶段检索

```
mem.search(query="为什么使用 pgvector", scope="development")
mem.get(ids=["mem_abc"])  -> 返回完整内容
```

---

## 8. 实施计划

- [x] Go MCP 实现（唯一入口）
- [x] 冲突检测改为 memory 级向量 + LLM 仲裁
- [x] 跨类型更新策略
- [x] 两阶段检索
- [x] 移除 Watcher 与 source 字段
- [ ] 文档和测试持续完善

---

## 9. 常见问题

**Q1：为什么不使用 content_hash 去重？**  
A：hash 只能判断“字节完全相同”，无法表达语义更新；P0 要求用语义仲裁替换旧知识。

**Q2：为什么允许跨类型更新？**  
A：类型只是标签，语义一致更重要；仲裁由 LLM 决定是否替换。

**Q3：Watcher 还需要吗？**  
A：不需要。入口统一为 MCP 调用，触发由 LLM 提示词控制。
