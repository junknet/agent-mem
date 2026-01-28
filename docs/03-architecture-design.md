# 架构设计

## 模块划分
- **Ingester**：入库流水线控制器。
- **Embedder**：片段向量化与 memory 级向量。
- **Arbiter**：LLM 仲裁（REPLACE / KEEP_BOTH / SKIP）。
- **Searcher**：混合检索（向量 + 关键词 + BM25 + RRF）。

## 数据流

LLM/Client Trigger → Ingester → Chunking → Embedder → Arbiter → DB

检索：Search → Get
