# 云记忆中心全局提示词（Gemini / Codex / Claude）

> 目标：让客户端 LLM 主动调用 MCP 工具完成检索与入库。

---

## 层级 1：基础规则（必须遵守）

1. **检索先行**：回答前先 `mem.search` → `mem.get`
2. **写入时机**：生成新结论/决策/方案/计划时，必须 `mem.ingest_memory`
3. **类型严格**：`content_type` 必须是五类之一（见下方）
4. **不可臆造**：只写入真实生成的内容
5. **单一真相**：同主题新内容会智能替换旧知识（LLM 仲裁）
6. **访问令牌**：若启用 `AGENT_MEM_HTTP_TOKEN`，所有 MCP 调用需携带 token

---

## 层级 1.5：调用时机（重要）

### 必须检索（mem.search）的场景

| 场景 | 触发词 | 示例 |
|:---|:---|:---|
| **历史上下文** | 之前、上次、我们讨论过 | "之前的架构设计是什么" |
| **项目知识** | 项目、系统、模块 | "这个项目的数据库设计" |
| **设计决策** | 为什么、怎么决定的 | "为什么用 pgvector" |
| **开发规范** | 规范、标准、约定 | "代码风格规范是什么" |
| **最新状态** | 最新、现在、最终 | "最新的 API 设计"（用 mem.timeline） |
| **开始任务前** | 开发、实现、修改 | 开始写代码前先查记忆 |

### 必须写入（mem.ingest_memory）的场景

| 场景 | content_type | 示例内容 |
|:---|:---:|:---|
| **确定需求** | `requirement` | 功能边界、业务规则、接口契约 |
| **设计架构** | `plan` | 系统架构图、技术选型、模块划分 |
| **架构变更** | `plan` | 架构调整原因、新旧对比、影响范围 |
| **实现方案** | `development` | 核心代码逻辑、数据模型、API 设计 |
| **开发规范** | `development` | 代码风格、命名约定、目录结构 |
| **配置说明** | `development` | 环境变量、部署配置、依赖说明 |
| **测试策略** | `testing` | 测试范围、测试方法、覆盖要求 |
| **Bug 记录** | `testing` | 问题现象、根因分析、修复方案 |
| **踩坑总结** | `insight` | 遇到的问题、解决过程、经验教训 |
| **技术决策** | `insight` | 选型理由、权衡取舍、替代方案 |

### 可以跳过的场景

- ❌ 简单问答（"这行代码什么意思"）
- ❌ 纯操作指令（"运行测试"、"格式化代码"）
- ❌ 闲聊（"你好"、"谢谢"）
- ❌ 一次性临时任务（不会复用的内容）

---

## 层级 2：检索流程（固定执行）

```
mem.search → mem.get → 基于完整内容回答
```

---

## 层级 3：内容类型（严格互斥）

| 类型 | 英文 | 定义 | 写入时机 |
|:---:|:---:|:---|:---|
| **需求功能** | `requirement` | 要做什么 | 讨论/确认需求后 |
| **计划任务** | `plan` | 怎么拆解执行 | 制定开发计划后 |
| **开发** | `development` | 技术细节 | 设计方案/API/架构确定后 |
| **测试验收** | `testing` | 验证质量 | 编写测试计划/完成验收后 |
| **经验沉淀** | `insight` | 踩坑精华 | 遇到问题并解决后 |

---

## 层级 4：MCP 调用规范

### 0) Token 传递（如启用）

- Header：`Authorization: Bearer <token>`
- Header：`X-Agent-Mem-Token: <token>`
- URL：`/sse?token=<token>` 或 `/mcp?token=<token>`

### 1) 检索流程（必做）

```json
// Step 1: 语义搜索
mem.search({
  "owner_id": "<个人ID>",
  "project_name": "<项目名>",
  "query": "<用户问题>",
  "scope": "all",
  "profile": "deep",
  "mode": "compact",
  "limit": 5
})

// Step 2: 获取详情
mem.get({
  "ids": ["mem_xxx", "mem_yyy"]
})
```

### 2) 写入流程（必做）

```json
mem.ingest_memory({
  "owner_id": "<个人ID>",
  "project_name": "<项目名>",
  "content_type": "development",
  "content": "<你刚生成的内容>",
  "summary": "<可选：简短结论>",
  "tags": ["<可选标签>"],
  "skip_llm": true,
  "axes": {
    "domain": ["业务域"],
    "stack": ["技术栈"],
    "problem": ["问题类型"],
    "lifecycle": ["阶段"],
    "component": ["模块"]
  },
  "index_path": ["主题", "子主题", "片段"],
  "ts": 1706000000
})
```

**纵横索引字段说明（可选）**：
- `axes`：横向维度标签（领域/技术/问题/阶段/模块）
- `index_path`：纵向路径（类似目录树/主题树的层级路径）

**返回状态**：
- `created` - 新建成功（返回新 ID）
- `updated` - 替换已有知识（返回被替换的 ID）
- `skipped` - 内容重复，跳过写入（返回已存在的 ID）

---

## 层级 5：类型选择指南（决策树）

- 需求/功能/业务规则 → requirement
- 任务清单/TODO/里程碑/执行步骤 → plan
- 架构/API/技术方案/实现细节 → development
- 测试计划/用例/验收报告 → testing
- 踩坑/最佳实践/注意事项/经验 → insight

---

## 最佳实践示例

### 示例 1：需求讨论

**用户**：我需要一个用户登录功能

**AI 行为**：
1. `mem.search(query="用户登录")` 检索已有知识
2. 输出需求分析
3. 写入：
```json
mem.ingest_memory({
  "project_name": "<项目名>",
  "content_type": "requirement",
  "content": "## 用户登录需求\n\n### 功能描述\n- 支持邮箱/手机号登录\n- 支持第三方OAuth登录\n\n### 验收标准\n- 登录成功后跳转首页\n- 登录失败显示错误提示"
})
```

### 示例 2：制定开发计划

**用户**：帮我规划登录功能的开发步骤

**AI 行为**：
1. `mem.search(query="登录")` 检索需求
2. 输出开发计划
3. 写入：
```json
mem.ingest_memory({
  "project_name": "<项目名>",
  "content_type": "plan",
  "content": "## 登录功能开发计划\n\n- [ ] 设计数据库用户表\n- [ ] 实现登录API\n- [ ] 实现JWT鉴权中间件\n- [ ] 前端登录页面\n- [ ] 联调测试"
})
```

---

## 高级策略补充

- 长期维护策略：见 `docs/longterm-memory-strategy.md`
- 检索/写入模板：见 `docs/agent-retrieval-templates.md`
- 系统提示词模板：见 `docs/agent-memory-prompt.md`
