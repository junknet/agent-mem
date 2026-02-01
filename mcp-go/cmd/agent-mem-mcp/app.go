package main

import (
	"context"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type App struct {
	settings Settings
	store    *Store
	llm      *LLMClient
	embedder *Embedder
	searcher *Searcher
	metrics  *MetricsCache
}

func NewApp(settings Settings) (*App, error) {
	store, err := NewStore(settings.Storage.DatabaseURL)
	if err != nil {
		return nil, err
	}
	llm := NewLLMClient(settings)
	embedder := NewEmbedder(settings)
	searcher := NewSearcher(store, llm, embedder, settings)

	return &App{
		settings: settings,
		store:    store,
		llm:      llm,
		embedder: embedder,
		searcher: searcher,
		metrics:  NewMetricsCache(),
	}, nil
}

func (a *App) Close() {
	if a.store != nil {
		a.store.Close()
	}
}

func (a *App) EnsureSchema(ctx context.Context, reset bool) error {
	if err := a.store.EnsureSchema(ctx, a.settings.Embedding.Dimension, reset); err != nil {
		return err
	}
	return a.store.BackfillProjectIdentity(ctx, a.settings.Project.OwnerID)
}

func buildServer(app *App) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "agent-mem", Version: "1.0.0"}, &mcp.ServerOptions{
		Instructions: `云记忆中心 MCP 服务 - AI 知识库

## 核心流程
1. 检索：mem.search → mem.get（两阶段）
2. 写入：mem.ingest_memory
3. 时间线：mem.timeline（查最新）

## 何时检索（mem.search）
- 用户提到"之前/上次/我们讨论过"
- 问项目设计/决策/规范问题
- 问"最新/现在/最终方案"
- 开始开发任务前

## 何时写入（mem.ingest_memory）
- 确定需求、设计架构、架构变更
- 核心代码实现、开发规范、配置说明
- 测试策略、Bug记录、踩坑总结

## content_type 五种类型
- requirement: 需求、业务规则
- plan: 架构设计、技术方案
- development: 代码实现、API设计、规范
- testing: 测试策略、Bug记录
- insight: 经验总结、技术决策

## 必需参数
- owner_id: 固定 "personal"
- project_key: 从工作目录提取`,
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "mem.ingest_memory",
		Description: `写入记忆到知识库。

**何时调用**：
- 确定需求边界、业务规则
- 设计架构方案、架构变更
- 核心代码实现、API设计
- 开发规范、配置说明
- 测试策略、Bug记录
- 踩坑总结、技术决策

**content_type 选择**：
- requirement: 需求、业务规则、验收标准
- plan: 架构设计、技术方案、实现计划
- development: 代码实现、API设计、开发规范
- testing: 测试策略、Bug记录、性能报告
- insight: 经验总结、技术决策、复盘

**必需参数**：owner_id=personal, content_type, content`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in IngestMemoryInput) (*mcp.CallToolResult, IngestMemoryOutput, error) {
		output, err := app.IngestMemoryTool(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "mem.search",
		Description: `语义检索记忆（第一阶段，返回摘要）。

**何时调用**：
- 用户提到"之前/上次/我们讨论过"
- 问项目设计/决策/规范问题
- 问"最新/现在/最终方案"
- 开始新开发任务前

**流程**：mem.search → 拿到 ID → mem.get 获取完整内容

**参数**：
- owner_id: 固定 "personal"
- query: 搜索关键词
- scope: 可选，过滤 content_type
- limit: 返回数量，默认 20`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchResponse, error) {
		output, err := app.SearchMemories(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "mem.get",
		Description: `获取记忆完整内容（第二阶段）。

**调用时机**：在 mem.search 返回结果后，用 ID 获取完整内容。

**参数**：ids - 从 mem.search 结果中获取的记忆 ID 列表`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetMemoriesInput) (*mcp.CallToolResult, GetMemoriesResponse, error) {
		output, err := app.GetMemories(ctx, in.IDs)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "mem.timeline",
		Description: `按时间线查询最近记忆。

**何时调用**：用户问"最新/现在/最终/最近"相关问题。

**参数**：
- owner_id: 固定 "personal"
- project_key: 可选，限定项目
- days: 查询天数，默认 7
- limit: 返回数量，默认 20`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in TimelineInput) (*mcp.CallToolResult, TimelineResponse, error) {
		output, err := app.Timeline(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.list_projects",
		Description: "列出所有项目及其记忆统计。参数：owner_id=personal",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListProjectsInput) (*mcp.CallToolResult, ListProjectsResponse, error) {
		output, err := app.ListProjects(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.index",
		Description: "纵横索引概览（标签/轴/路径聚合），用于浏览知识结构",
		OutputSchema: map[string]any{
			"type": "object",
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in IndexInput) (*mcp.CallToolResult, IndexResponse, error) {
		output, err := app.Index(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.metrics",
		Description: "索引可观测指标（Prometheus 格式），用于系统监控",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in IndexInput) (*mcp.CallToolResult, MetricsResponse, error) {
		output, err := app.Metrics(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.arbitration_history",
		Description: "查询仲裁历史（记忆更新/替换的决策记录：REPLACE/KEEP_BOTH/SKIP）",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ArbitrationHistoryInput) (*mcp.CallToolResult, ArbitrationHistoryResponse, error) {
		output, err := app.ArbitrationHistory(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.memory_chain",
		Description: "查询记忆演进链（同一记忆的历史版本和仲裁记录）",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in MemoryChainInput) (*mcp.CallToolResult, MemoryChainResponse, error) {
		output, err := app.MemoryChain(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.rollback",
		Description: "回滚仲裁决策（撤销记忆替换，恢复旧版本）",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RollbackInput) (*mcp.CallToolResult, RollbackOutput, error) {
		output, err := app.Rollback(ctx, in)
		return nil, output, err
	})

	return server
}

func (a *App) IngestMemoryTool(ctx context.Context, input IngestMemoryInput) (IngestMemoryOutput, error) {
	normalized, err := normalizeIngestInput(input, a.settings, time.Now().UTC())
	if err != nil {
		return IngestMemoryOutput{}, err
	}
	result, err := a.IngestMemory(ctx, normalized)
	if err != nil {
		return IngestMemoryOutput{}, err
	}
	status := result.Status
	if status == "" {
		status = "created"
	}
	return IngestMemoryOutput{ID: result.ID, Status: status, Ts: normalized.Ts}, nil
}

func (a *App) SearchMemories(ctx context.Context, input SearchInput) (SearchResponse, error) {
	normalized, err := normalizeSearchInput(input, a.settings)
	if err != nil {
		return SearchResponse{}, err
	}
	if err := validateSearchInput(normalized); err != nil {
		return SearchResponse{}, err
	}
	return a.searcher.Search(ctx, normalized)
}

func (a *App) GetMemories(ctx context.Context, ids []string) (GetMemoriesResponse, error) {
	ids = uniqueStrings(ids)
	if len(ids) == 0 {
		return GetMemoriesResponse{Results: []MemoryRecord{}}, nil
	}
	rows, err := a.store.FetchMemories(ctx, ids)
	if err != nil {
		return GetMemoriesResponse{}, err
	}

	rowMap := make(map[string]MemoryRecord)
	for _, row := range rows {
		rowMap[row.ID] = MemoryRecord{
			ID:          row.ID,
			Content:     row.Content,
			ContentType: row.ContentType,
			Summary:     row.Summary,
			Tags:        row.Tags,
			Axes:        axesPtr(row.Axes),
			IndexPath:   row.IndexPath,
			Ts:          row.Ts,
		}
	}

	results := make([]MemoryRecord, 0, len(ids))
	for _, id := range ids {
		if row, ok := rowMap[id]; ok {
			results = append(results, row)
		}
	}

	return GetMemoriesResponse{Results: results}, nil
}

func (a *App) Timeline(ctx context.Context, input TimelineInput) (TimelineResponse, error) {
	normalized, err := normalizeTimelineInput(input, a.settings)
	if err != nil {
		return TimelineResponse{}, err
	}
	if err := validateTimelineInput(normalized); err != nil {
		return TimelineResponse{}, err
	}

	sinceTs := time.Now().UTC().Add(-time.Duration(normalized.Days) * 24 * time.Hour).Unix()
	if normalized.ProjectKey != "" {
		projectID, err := a.store.FindProjectIDByKey(ctx, normalized.OwnerID, normalized.ProjectKey)
		if err != nil {
			return TimelineResponse{}, err
		}
		if projectID == "" {
			return TimelineResponse{Results: []TimelineItem{}, Metadata: SearchMetadata{Total: 0, Returned: 0}}, nil
		}
		rows, err := a.store.FetchTimeline(ctx, projectID, sinceTs, normalized.Limit)
		if err != nil {
			return TimelineResponse{}, err
		}
		results := make([]TimelineItem, 0, len(rows))
		for _, row := range rows {
			results = append(results, TimelineItem{
				ID:          row.ID,
				ContentType: row.ContentType,
				Summary:     row.Summary,
				Ts:          row.Ts,
			})
		}
		return TimelineResponse{
			Results: results,
			Metadata: SearchMetadata{
				Total:    len(results),
				Returned: len(results),
			},
		}, nil
	}

	rows, err := a.store.FetchTimelineByOwner(ctx, normalized.OwnerID, sinceTs, normalized.Limit)
	if err != nil {
		return TimelineResponse{}, err
	}
	results := make([]TimelineItem, 0, len(rows))
	for _, row := range rows {
		results = append(results, TimelineItem{
			ID:          row.ID,
			ContentType: row.ContentType,
			Summary:     row.Summary,
			Ts:          row.Ts,
		})
	}
	return TimelineResponse{
		Results: results,
		Metadata: SearchMetadata{
			Total:    len(results),
			Returned: len(results),
		},
	}, nil
}

func (a *App) ListProjects(ctx context.Context, input ListProjectsInput) (ListProjectsResponse, error) {
	normalized, err := normalizeListProjectsInput(input, a.settings)
	if err != nil {
		return ListProjectsResponse{}, err
	}
	if err := validateListProjectsInput(normalized); err != nil {
		return ListProjectsResponse{}, err
	}
	rows, err := a.store.ListProjects(ctx, normalized.OwnerID, normalized.Limit)
	if err != nil {
		return ListProjectsResponse{}, err
	}
	return ListProjectsResponse{
		Results: rows,
		Metadata: SearchMetadata{
			Total:    len(rows),
			Returned: len(rows),
		},
	}, nil
}

func (a *App) Index(ctx context.Context, input IndexInput) (IndexResponse, error) {
	normalized, err := normalizeIndexInput(input, a.settings)
	if err != nil {
		return IndexResponse{}, err
	}
	if err := validateIndexInput(normalized); err != nil {
		return IndexResponse{}, err
	}

	projectScoped := strings.TrimSpace(normalized.ProjectKey) != ""
	projectID := ""
	if projectScoped {
		projectID, err = a.store.FindProjectIDByKey(ctx, normalized.OwnerID, normalized.ProjectKey)
		if err != nil {
			return IndexResponse{}, err
		}
		if projectID == "" {
			return IndexResponse{Axes: []IndexAxis{}, Paths: []IndexPathCount{}, Metadata: SearchMetadata{Total: 0, Returned: 0}}, nil
		}
	}

	limit := normalized.Limit
	var indexPath []string
	if normalized.IndexPath != nil {
		indexPath = *normalized.IndexPath
	}
	axes := []IndexAxis{}
	tagCounts, err := a.store.FetchTagCounts(ctx, projectID, normalized.OwnerID, limit, indexPath)
	if err != nil {
		return IndexResponse{}, err
	}
	if len(tagCounts) > 0 {
		axes = append(axes, IndexAxis{Axis: "tags", Values: tagCounts})
	}

	axisNames := []string{"domain", "stack", "problem", "lifecycle", "component"}
	for _, axis := range axisNames {
		values, err := a.store.FetchAxisCounts(ctx, projectID, normalized.OwnerID, axis, limit, indexPath)
		if err != nil {
			return IndexResponse{}, err
		}
		if len(values) > 0 {
			axes = append(axes, IndexAxis{Axis: axis, Values: values})
		}
	}

	paths, err := a.store.FetchIndexPaths(ctx, projectID, normalized.OwnerID, limit, indexPath)
	if err != nil {
		return IndexResponse{}, err
	}
	pathsForTree := paths
	if len(indexPath) > 0 {
		pathsForTree = trimIndexPathCounts(paths, indexPath)
	}
	pathTree := buildIndexPathTree(pathsForTree, normalized.PathTreeDepth, normalized.PathTreeWidth)

	counts, err := a.store.FetchMemoryCounts(ctx, projectID, normalized.OwnerID, indexPath)
	if err != nil {
		return IndexResponse{}, err
	}
	depthDist, err := a.store.FetchIndexPathDepthDistribution(ctx, projectID, normalized.OwnerID, indexPath)
	if err != nil {
		return IndexResponse{}, err
	}
	stats := buildIndexStats(counts, depthDist, pathTree, len(indexPath))

	total := len(axes) + len(paths)
	return IndexResponse{
		Axes:     axes,
		Paths:    paths,
		PathTree: pathTree,
		Stats:    stats,
		Metadata: SearchMetadata{
			Total:    total,
			Returned: total,
		},
	}, nil
}

// === 仲裁历史与回滚 ===

func (a *App) ArbitrationHistory(ctx context.Context, input ArbitrationHistoryInput) (ArbitrationHistoryResponse, error) {
	ownerID := strings.TrimSpace(input.OwnerID)
	if ownerID == "" {
		ownerID = a.settings.Project.OwnerID
	}
	if ownerID == "" {
		ownerID = defaultOwnerID
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	projectID := ""
	if input.ProjectKey != "" {
		var err error
		projectID, err = a.store.FindProjectIDByKey(ctx, ownerID, input.ProjectKey)
		if err != nil {
			return ArbitrationHistoryResponse{}, err
		}
	}

	records, err := a.store.FetchArbitrationHistory(ctx, ownerID, input.MemoryID, projectID, limit)
	if err != nil {
		return ArbitrationHistoryResponse{}, err
	}

	return ArbitrationHistoryResponse{
		Results: records,
		Metadata: SearchMetadata{
			Total:    len(records),
			Returned: len(records),
		},
	}, nil
}

func (a *App) MemoryChain(ctx context.Context, input MemoryChainInput) (MemoryChainResponse, error) {
	ownerID := strings.TrimSpace(input.OwnerID)
	if ownerID == "" {
		ownerID = a.settings.Project.OwnerID
	}
	if ownerID == "" {
		ownerID = defaultOwnerID
	}

	memoryID := strings.TrimSpace(input.MemoryID)
	if memoryID == "" {
		return MemoryChainResponse{}, newValidationError("invalid_request", "ERR_INVALID_MEMORY_ID", "memory_id 不能为空", 400)
	}

	// 获取当前记忆摘要
	current, err := a.store.FetchMemorySummary(ctx, memoryID)
	if err != nil {
		return MemoryChainResponse{}, err
	}

	// 获取历史版本
	versions, err := a.store.FetchMemoryVersions(ctx, memoryID)
	if err != nil {
		return MemoryChainResponse{}, err
	}

	// 获取相关仲裁记录
	arbitrations, err := a.store.FetchArbitrationHistory(ctx, ownerID, memoryID, "", 50)
	if err != nil {
		return MemoryChainResponse{}, err
	}

	return MemoryChainResponse{
		MemoryID:       memoryID,
		CurrentSummary: current.Summary,
		Versions:       versions,
		Arbitrations:   arbitrations,
	}, nil
}

func (a *App) Rollback(ctx context.Context, input RollbackInput) (RollbackOutput, error) {
	ownerID := strings.TrimSpace(input.OwnerID)
	if ownerID == "" {
		ownerID = a.settings.Project.OwnerID
	}
	if ownerID == "" {
		ownerID = defaultOwnerID
	}

	if input.ArbitrationID <= 0 {
		return RollbackOutput{}, newValidationError("invalid_request", "ERR_INVALID_ARBITRATION_ID", "arbitration_id 必须为正整数", 400)
	}

	// 获取仲裁记录
	arb, err := a.store.FetchArbitrationByID(ctx, input.ArbitrationID)
	if err != nil {
		return RollbackOutput{Status: "failed", Message: "仲裁记录不存在"}, nil
	}

	// 只有 REPLACE 操作才能回滚
	if arb.Action != "REPLACE" {
		return RollbackOutput{Status: "failed", Message: "只有 REPLACE 操作可以回滚"}, nil
	}

	memoryID := arb.CandidateMemoryID
	if memoryID == "" {
		return RollbackOutput{Status: "failed", Message: "无法确定要恢复的记忆 ID"}, nil
	}

	// 获取最新的历史版本
	version, err := a.store.FetchLatestVersion(ctx, memoryID)
	if err != nil {
		return RollbackOutput{Status: "failed", Message: "没有可恢复的历史版本"}, nil
	}

	// 执行恢复
	if err := a.store.RestoreMemoryFromVersion(ctx, version); err != nil {
		return RollbackOutput{Status: "failed", Message: err.Error()}, nil
	}

	return RollbackOutput{
		Status:           "success",
		RestoredMemoryID: memoryID,
		Message:          "已恢复到上一个版本",
	}, nil
}
