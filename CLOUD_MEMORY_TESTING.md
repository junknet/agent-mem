# 云记忆中心测试设计文档（攻击性测试）

> **版本**：v2.0
> **日期**：2026-01-26
> **目标**：覆盖 P0 级入库、冲突仲裁、更新替换、检索链路

---

## 1. 测试策略

- **真实模型优先**：LLM/Embedding 必须走真实 API
- **P0 场景优先**：入库冲突、更新替换、跨类型覆盖、检索正确性
- **两阶段检索必测**：search → get

---

## 2. P0 核心测试矩阵

| 编号 | 场景 | 预期 |
|---|---|---|
| P0-01 | 初次写入 | status=created |
| P0-02 | 相同主题二次写入 | status=updated 或 skipped（由 LLM 仲裁） |
| P0-03 | 语义更新（内容有新增） | status=updated |
| P0-04 | 跨 content_type 更新 | 仍可 updated，类型被替换 |
| P0-05 | search → get 链路 | search 返回 ID，get 返回完整内容 |

---

## 3. 真实测试执行步骤（推荐）

> **要求**：使用真实 `DASHSCOPE_API_KEY` 与 `DATABASE_URL`，不要启用 mock。

### 3.1 统一重建数据库

```
cd mcp-go
./cmd/agent-mem-mcp --reset-db --reset-only
```

### 3.2 启动服务

```
cd mcp-go
./cmd/agent-mem-mcp --host 127.0.0.1 --port 8787 --transport http
```

### 3.3 测试用数据（真实文本）

- 取本机真实日志/对话片段作为 content
- 保持项目路径固定，确保同项目内冲突判定

### 3.4 核心测试用例

#### P0-01 初次写入
- 写入 `content_type=development`
- 期望：created

#### P0-02 相同主题写入
- 相同主题再次写入
- 期望：updated 或 skipped

#### P0-03 语义更新
- 增加一条新结论/新增条目
- 期望：updated

#### P0-04 跨类型更新
- 同主题改为 `content_type=insight`
- 期望：仍可 updated，类型被替换

#### P0-05 search → get
- search 使用关键字检索
- get 取回完整 content

---

## 4. 边界与异常测试（可选）

- content 为空 / 超长 / 非 UTF-8
- owner_id / project_name 为空或过长
- query 为空（应返回 400）

---

## 5. 判定标准

- 入库必须返回明确的 `status` + `id`
- 更新覆盖必须命中旧记忆 ID（P0）
- search 返回 snippet，get 返回完整内容
- 所有接口错误必须返回可读中文错误信息
