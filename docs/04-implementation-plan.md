# 实施路线图 (Implementation Plan)

> **状态**: Phase 4 进行中
> **架构**: Pure Go Transition Completed (2026-01-24)

## Phase 1: 基础设施 (已完成)
- [x] PostgreSQL + pgvector 容器化部署
- [x] DB Schema 定义 (Knowledge Block)

## Phase 2: 核心流水线 (已完成 - Python Legacy)
- [x] Python 版 Watcher (watchdog)
- [x] Python 版 Ingester & LLM SDK
- [x] *注：已归档至 src_legacy/*

## Phase 3: Go 架构迁移 (已完成)
- [x] **Go Watcher**: 替换 watchdog，使用 fsnotify
- [x] **Go Ingester**: 移植完整入库逻辑与 Prompt
- [x] **Go Server**: 实现 MCP 协议 (HTTP/SSE)
- [x] **Hard Delete**: 实现物理删除的“唯一真理”模式

## Phase 4: 生产级增强 (进行中)
- [x] **E2E 测试**: 覆盖 Watcher/Ingester 全链路
- [x] **Prompt 优化**: 消除幻觉，精准分类
- [ ] **多机部署**: 探索分布式文件同步或集中式 Watcher 方案
- [ ] **Rerank 本地化**: 探索本地 Rerank 模型以降低 API 依赖
