package main

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type App struct {
	settings Settings
	store    *Store
	llm      *LLMClient
	embedder *Embedder
	searcher *Searcher
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
		Instructions: "这是云记忆中心 MCP 服务。严格流程：mem.search -> mem.get，写入使用 mem.ingest_memory。",
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.ingest_memory",
		Description: "写入记忆",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in IngestMemoryInput) (*mcp.CallToolResult, IngestMemoryOutput, error) {
		output, err := app.IngestMemoryTool(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.search",
		Description: "片段检索（第一阶段）",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchResponse, error) {
		output, err := app.SearchMemories(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.get",
		Description: "获取完整内容（第二阶段）",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetMemoriesInput) (*mcp.CallToolResult, GetMemoriesResponse, error) {
		output, err := app.GetMemories(ctx, in.IDs)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.timeline",
		Description: "时间线查询",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in TimelineInput) (*mcp.CallToolResult, TimelineResponse, error) {
		output, err := app.Timeline(ctx, in)
		return nil, output, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "mem.list_projects",
		Description: "项目列表",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ListProjectsInput) (*mcp.CallToolResult, ListProjectsResponse, error) {
		output, err := app.ListProjects(ctx, in)
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
