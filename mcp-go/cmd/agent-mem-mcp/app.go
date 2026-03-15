package main

import (
	"context"
	"fmt"
	"log/slog"
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
	server := mcp.NewServer(&mcp.Implementation{Name: "agent-mem", Version: "2.0.0"}, &mcp.ServerOptions{
		Logger:    slog.Default(),
		KeepAlive: 30 * time.Second,
		Instructions: `云记忆中心 MCP 服务 - AI 知识库

## 核心流程
1. 检索：mem.search → mem.get（两阶段，先搜索再获取完整内容）
2. 写入：mem.ingest_memory（生成结论/方案/决策后立即写入）
3. 最新：优先 latest 路径；无结果再 mem.timeline
4. 关联：mem.link 创建记忆间关系，mem.relations 查询关联
5. 蒸馏：mem.distill 将零碎记忆浓缩为精华知识
6. 前瞻：写入时自动生成预测，mem.foresights 查看

## 必须规则
- owner_id 固定 "personal"
- mem.search 的 scope 固定 "all"
- 用户问"最新/现在/最终"时，优先 latest 路径（index_path=["dialogs","<主题>","latest"]）
- 新结论与旧结论冲突：index_path=["conflict","<主题>","..."] 且 tags=["conflict"]
- 发现记忆之间有因果/依赖关系时，用 mem.link 显式建立关联

## 何时必须检索（mem.search）

| 触发词 | 场景示例 |
|:---|:---|
| 之前/上次/讨论过 | "之前的架构设计是什么" |
| 项目/系统/模块 | "这个项目的数据库设计" |
| 为什么/怎么决定 | "为什么选择 pgvector" |
| 规范/标准/约定 | "代码风格规范是什么" |
| 最新/现在/最终 | "最新的 API 设计"（优先 latest 路径，无结果再 mem.timeline） |
| 开始任务前 | 写代码前先查记忆 |

## 何时必须写入（mem.ingest_memory）

| 场景 | content_type | 示例 |
|:---|:---:|:---|
| 确定需求 | requirement | 功能边界、业务规则、接口契约 |
| 设计架构 | plan | 系统架构、技术选型、模块划分 |
| 架构变更 | plan | 变更原因、新旧对比、影响范围 |
| 实现方案 | development | 核心代码、数据模型、API设计 |
| 开发规范 | development | 代码风格、命名约定、目录结构 |
| 测试策略 | testing | 测试范围、方法、覆盖要求 |
| Bug记录 | testing | 问题现象、根因、修复方案 |
| 踩坑总结 | insight | 遇到问题、解决过程、经验教训 |
| 技术决策 | insight | 选型理由、权衡取舍 |
| 逆向发现 | discovery | API签名、协议分析、hook技巧 |
| 市场研究 | research | 交易策略、数据源、市场结论 |
| 运维记录 | ops | 部署配置、服务器设置、监控 |

content_type 支持任意字符串，推荐用上述类型保持一致性。

## 记忆关联（mem.link / mem.relations）
- 发现两条记忆有因果关系时用 DERIVED_FROM
- 发现矛盾时用 CONTRADICTS
- 同一主题的补充信息用 SUPPORTS
- 前后步骤用 FOLLOWING
- 搜索结果自动附带 related_ids

## 记忆蒸馏（mem.distill）
- 一个项目积累了大量零碎记忆时，调用 mem.distill 浓缩
- 蒸馏结果带 tags=["distilled","auto-digest"]
- 适合阶段性总结：逆向完一个APP、策略回测一轮后

## 何时跳过
- 简单问答（"这行代码什么意思"）
- 纯操作指令（"运行测试"）
- 闲聊（"你好"、"谢谢"）
- 一次性临时任务

## 必需参数
- owner_id: 固定 "personal"
- project_key: 从工作目录名提取（如 /home/user/Desktop/my-project → "my-project"）`,
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "mem.ingest_memory",
		Description: `写入记忆到知识库(生成结论/方案/决策后立即调用)。

**何时调用**：确定需求、设计架构、核心实现、测试Bug、踩坑经验、逆向发现、市场研究、运维记录等。

**content_type**：支持任意字符串。推荐值：
requirement/plan/development/testing/insight/discovery/research/ops

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

	mcp.AddTool(server, &mcp.Tool{
		Name: "mem.link",
		Description: `创建记忆间关系边（显式关联两条记忆）。

**relation_type**：FOLLOWING / DERIVED_FROM / CONTRADICTS / SUPPORTS / RELATED
**strength**：可选，默认 1.0（0-1 范围）
**metadata**：可选，附加元数据`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in LinkInput) (*mcp.CallToolResult, LinkOutput, error) {
		output, err := app.LinkMemories(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "mem.relations",
		Description: `查询记忆的关联关系。

**direction**：outgoing / incoming / both（默认 both）
**relation_type**：可选，过滤关系类型
**limit**：返回数量，默认 20`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RelationsInput) (*mcp.CallToolResult, RelationsResponse, error) {
		output, err := app.QueryRelations(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "mem.foresights",
		Description: `查询前瞻记忆（系统自动生成的"接下来可能需要什么"预测）。

**参数**：
- owner_id: 固定 "personal"
- memory_id: 可选，查询某条记忆的前瞻
- project_key: 可选，查询某项目的前瞻
- limit: 返回数量，默认 20

memory_id 或 project_key 至少提供一个。`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ForesightInput) (*mcp.CallToolResult, ForesightResponse, error) {
		output, err := app.QueryForesights(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "mem.distill",
		Description: `记忆蒸馏（工作记忆→长期知识浓缩）。

**何时调用**：项目产生大量零碎记忆后，蒸馏为精华长期知识。

**参数**：
- owner_id: 固定 "personal"
- project_key: 必填，指定蒸馏哪个项目
- scope: 可选，限定 content_type（如 "development"）
- since_days: 可选，蒸馏最近 N 天的记忆，默认 7
- target_content_type: 可选，蒸馏结果的类型，默认 "insight"`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DistillInput) (*mcp.CallToolResult, DistillOutput, error) {
		output, err := app.DistillMemories(ctx, in)
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

// === 记忆间关系边 ===

// validRelationTypes 关系类型白名单
var validRelationTypes = map[string]bool{
	"FOLLOWING":    true,
	"DERIVED_FROM": true,
	"CONTRADICTS":  true,
	"SUPPORTS":     true,
	"RELATED":      true,
}

func (a *App) LinkMemories(ctx context.Context, input LinkInput) (LinkOutput, error) {
	sourceID := strings.TrimSpace(input.SourceID)
	targetID := strings.TrimSpace(input.TargetID)
	relationType := strings.TrimSpace(input.RelationType)

	if sourceID == "" {
		return LinkOutput{}, newValidationError("invalid_request", "ERR_INVALID_SOURCE_ID", "source_id 不能为空", 400)
	}
	if targetID == "" {
		return LinkOutput{}, newValidationError("invalid_request", "ERR_INVALID_TARGET_ID", "target_id 不能为空", 400)
	}
	if sourceID == targetID {
		return LinkOutput{}, newValidationError("invalid_request", "ERR_SELF_RELATION", "source_id 和 target_id 不能相同", 400)
	}
	if !validRelationTypes[relationType] {
		return LinkOutput{}, newValidationError("invalid_request", "ERR_INVALID_RELATION_TYPE", "relation_type 必须是 FOLLOWING/DERIVED_FROM/CONTRADICTS/SUPPORTS/RELATED", 400)
	}

	strength := input.Strength
	if strength <= 0 {
		strength = 1.0
	}
	if strength > 1.0 {
		strength = 1.0
	}

	id, err := a.store.InsertRelation(ctx, sourceID, targetID, relationType, strength, input.Metadata)
	if err != nil {
		return LinkOutput{}, fmt.Errorf("创建关系失败: %w", err)
	}
	return LinkOutput{ID: id, Status: "created"}, nil
}

func (a *App) QueryRelations(ctx context.Context, input RelationsInput) (RelationsResponse, error) {
	memoryID := strings.TrimSpace(input.MemoryID)
	if memoryID == "" {
		return RelationsResponse{}, newValidationError("invalid_request", "ERR_INVALID_MEMORY_ID", "memory_id 不能为空", 400)
	}

	direction := strings.TrimSpace(input.Direction)
	if direction == "" {
		direction = "both"
	}
	switch direction {
	case "outgoing", "incoming", "both":
		// valid
	default:
		return RelationsResponse{}, newValidationError("invalid_request", "ERR_INVALID_DIRECTION", "direction 必须是 outgoing/incoming/both", 400)
	}

	relationType := strings.TrimSpace(input.RelationType)
	if relationType != "" && !validRelationTypes[relationType] {
		return RelationsResponse{}, newValidationError("invalid_request", "ERR_INVALID_RELATION_TYPE", "relation_type 必须是 FOLLOWING/DERIVED_FROM/CONTRADICTS/SUPPORTS/RELATED", 400)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	relations, err := a.store.FetchRelations(ctx, memoryID, direction, relationType, limit)
	if err != nil {
		return RelationsResponse{}, err
	}
	if relations == nil {
		relations = []RelationRecord{}
	}

	return RelationsResponse{
		Relations: relations,
		Metadata: SearchMetadata{
			Total:    len(relations),
			Returned: len(relations),
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
