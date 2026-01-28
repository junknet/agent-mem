package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestIngestSearchFlow(t *testing.T) {
	if os.Getenv("AGENT_MEM_INTEGRATION") == "" {
		t.Skip("未设置 AGENT_MEM_INTEGRATION，跳过集成测试")
	}
	settings := defaultSettings()
	settings.Storage.DatabaseURL = envOrDefault("DATABASE_URL", settings.Storage.DatabaseURL)
	settings.Embedding.Provider = "mock"
	settings.Embedding.Dimension = 1536
	settings.Chunking = ChunkingConfig{ChunkSize: 500, Overlap: 50, ApproxCharsPerToken: 4}

	os.Setenv("AGENT_MEM_LLM_MODE", "mock")
	app, err := NewApp(settings)
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	if err := app.EnsureSchema(ctx, true); err != nil {
		t.Fatalf("初始化表结构失败: %v", err)
	}

	now := time.Now().UTC().Unix()
	input := IngestMemoryInput{
		OwnerID:     "personal",
		ProjectName: "agent-mem-test",
		ProjectKey:  "agent-mem-test",
		ContentType: "development",
		Content:     "我们决定使用 PostgreSQL + pgvector 作为主存储方案。",
		Ts:          now,
	}

	result, err := app.IngestMemory(ctx, input)
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if result.ID == "" {
		t.Fatalf("写入未返回 ID")
	}

	search := SearchInput{
		OwnerID:     "personal",
		ProjectName: "agent-mem-test",
		ProjectKey:  "agent-mem-test",
		Query:       "为什么选 PostgreSQL",
		Scope:       "development",
		Limit:       5,
	}
	searchResp, err := app.SearchMemories(ctx, search)
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(searchResp.Results) == 0 {
		t.Fatalf("检索未返回结果")
	}

	ids := []string{searchResp.Results[0].ID}
	getResp, err := app.GetMemories(ctx, ids)
	if err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	if len(getResp.Results) == 0 {
		t.Fatalf("获取未返回内容")
	}

	timelineResp, err := app.Timeline(ctx, TimelineInput{
		OwnerID:     "personal",
		ProjectName: "agent-mem-test",
		ProjectKey:  "agent-mem-test",
		Days:        7,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("时间线失败: %v", err)
	}
	if len(timelineResp.Results) == 0 {
		t.Fatalf("时间线为空")
	}

	projectsResp, err := app.ListProjects(ctx, ListProjectsInput{OwnerID: "personal", Limit: 10})
	if err != nil {
		t.Fatalf("项目列表失败: %v", err)
	}
	if len(projectsResp.Results) == 0 {
		t.Fatalf("项目列表为空")
	}
}
