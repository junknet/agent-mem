# 实施路线图 (Implementation Plan)

> **状态**：Phase 4 进行中
> **架构**：Go MCP 单入口（2026-01-26）

## Phase 1: 基础设施（已完成）
- [x] PostgreSQL + pgvector 容器化部署
- [x] DB Schema 定义（memories + fragments + projects）

## Phase 2: 核心流水线（已完成）
- [x] Go Ingester（入库、向量化、切分）
- [x] LLM 仲裁（REPLACE / KEEP_BOTH / SKIP）
- [x] memory 级 avg_embedding 冲突检测

## Phase 3: MCP 服务（已完成）
- [x] MCP HTTP/SSE/stdio 接口
- [x] 两阶段检索（search → get）
- [x] Watcher 移除，入口统一为 MCP

## Phase 4: 生产级增强（进行中）
- [x] E2E 测试覆盖入库/检索/更新链路
- [x] Prompt 优化：五类 content_type
- [ ] 多机部署一致性策略
- [ ] 本地 Rerank 方案
