package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// 测试辅助函数
func strSlicePtr(s []string) *[]string { return &s }
func strArrPtr(s ...string) *[]string  { return &s }

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
		Query:       "PostgreSQL",
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

func TestIndexPathTreeIntegration(t *testing.T) {
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

	projectName := "agent-mem-index-test"
	now := time.Now().UTC().Unix()
	tagsAlpha := []string{"alpha"}
	tagsBeta := []string{"beta"}
	pathAlpha := []string{"root", "alpha"}
	pathAlphaChild := []string{"root", "alpha", "child"}
	pathBeta := []string{"root", "beta"}
	entries := []IngestMemoryInput{
		{
			OwnerID:     "personal",
			ProjectName: projectName,
			ProjectKey:  projectName,
			ContentType: "development",
			Content:     "root alpha decision one",
			Tags:        &tagsAlpha,
			Axes:        &MemoryAxes{Domain: []string{"backend"}},
			IndexPath:   &pathAlpha,
			Ts:          now,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectName,
			ProjectKey:  projectName,
			ContentType: "development",
			Content:     "root alpha decision two",
			Tags:        &tagsAlpha,
			Axes:        &MemoryAxes{Domain: []string{"backend"}},
			IndexPath:   &pathAlphaChild,
			Ts:          now + 1,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectName,
			ProjectKey:  projectName,
			ContentType: "development",
			Content:     "root beta decision",
			Tags:        &tagsBeta,
			Axes:        &MemoryAxes{Domain: []string{"frontend"}},
			IndexPath:   &pathBeta,
			Ts:          now + 2,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectName,
			ProjectKey:  projectName,
			ContentType: "development",
			Content:     "other gamma decision",
			Tags: strArrPtr("gamma"),
			Axes:        &MemoryAxes{Domain: []string{"ops"}},
			IndexPath:   strArrPtr("other", "gamma"),
			Ts:          now + 3,
		},
	}

	for _, entry := range entries {
		result, err := app.IngestMemory(ctx, entry)
		if err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		if result.ID == "" {
			t.Fatalf("写入未返回 ID")
		}
	}

	duplicate, err := app.IngestMemory(ctx, entries[0])
	if err != nil {
		t.Fatalf("重复写入失败: %v", err)
	}
	if duplicate.Status != "duplicate" {
		t.Fatalf("重复写入状态错误: %s", duplicate.Status)
	}

	searchIndexPath := []string{"root", "alpha"}
	search := SearchInput{
		OwnerID:     "personal",
		ProjectName: projectName,
		ProjectKey:  projectName,
		Query:       "alpha",
		Scope:       "development",
		IndexPath:   &searchIndexPath,
		Limit:       5,
	}
	searchResp, err := app.SearchMemories(ctx, search)
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(searchResp.Results) == 0 {
		t.Fatalf("检索未返回结果")
	}

	indexResp, err := app.Index(ctx, IndexInput{
		OwnerID:       "personal",
		ProjectName:   projectName,
		ProjectKey:    projectName,
		IndexPath:     strArrPtr("root"),
		Limit:         20,
		PathTreeDepth: 2,
		PathTreeWidth: 10,
	})
	if err != nil {
		t.Fatalf("索引失败: %v", err)
	}
	if len(indexResp.PathTree) == 0 {
		t.Fatalf("索引树为空")
	}
	if indexResp.PathTree[0].Name != "alpha" && indexResp.PathTree[0].Name != "beta" {
		t.Fatalf("索引树根节点异常: %+v", indexResp.PathTree)
	}
}

func TestIndexPathTreeDepthWidthIntegration(t *testing.T) {
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

	projectName := "agent-mem-tree-limit-test"
	now := time.Now().UTC().Unix()
	entries := []IngestMemoryInput{
		{
			OwnerID:     "personal",
			ProjectName: projectName,
			ProjectKey:  projectName,
			ContentType: "development",
			Content:     "root a x",
			IndexPath:   strArrPtr("root", "a", "x"),
			Ts:          now,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectName,
			ProjectKey:  projectName,
			ContentType: "development",
			Content:     "root a y",
			IndexPath:   strArrPtr("root", "a", "y"),
			Ts:          now + 1,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectName,
			ProjectKey:  projectName,
			ContentType: "development",
			Content:     "root b z",
			IndexPath:   strArrPtr("root", "b", "z"),
			Ts:          now + 2,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectName,
			ProjectKey:  projectName,
			ContentType: "development",
			Content:     "root c t",
			IndexPath:   strArrPtr("root", "c", "t"),
			Ts:          now + 3,
		},
	}

	for _, entry := range entries {
		if _, err := app.IngestMemory(ctx, entry); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	indexResp, err := app.Index(ctx, IndexInput{
		OwnerID:       "personal",
		ProjectName:   projectName,
		ProjectKey:    projectName,
		IndexPath:     strArrPtr("root"),
		Limit:         50,
		PathTreeDepth: 1,
		PathTreeWidth: 2,
	})
	if err != nil {
		t.Fatalf("索引失败: %v", err)
	}
	if len(indexResp.PathTree) != 2 {
		t.Fatalf("宽度裁剪失败: %+v", indexResp.PathTree)
	}
	for _, node := range indexResp.PathTree {
		if len(node.Children) != 0 {
			t.Fatalf("深度裁剪失败: %+v", node)
		}
	}
}

func TestConflictKnowledgeAcrossProjectsIntegration(t *testing.T) {
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

	alpha := "proj-alpha"
	beta := "proj-beta"
	gamma := "proj-gamma"
	now := time.Now().UTC().Unix()
	entries := []IngestMemoryInput{
		{
			OwnerID:     "personal",
			ProjectName: alpha,
			ProjectKey:  alpha,
			ContentType: "insight",
			Content:     "数据库选择 PostgreSQL，性能优先，适合核心系统。",
			Tags: strArrPtr("db", "postgresql"),
			Axes:        &MemoryAxes{Domain: []string{"backend"}, Problem: []string{"performance"}},
			IndexPath:   strArrPtr("decisions", "db", "primary"),
			SkipLLM:     true,
			Ts:          now,
		},
		{
			OwnerID:     "personal",
			ProjectName: alpha,
			ProjectKey:  alpha,
			ContentType: "insight",
			Content:     "数据库选择 MySQL，兼容优先，便于迁移。",
			Tags: strArrPtr("db", "mysql"),
			Axes:        &MemoryAxes{Domain: []string{"backend"}, Problem: []string{"compatibility"}},
			IndexPath:   strArrPtr("decisions", "db", "primary"),
			SkipLLM:     true,
			Ts:          now + 1,
		},
		{
			OwnerID:     "personal",
			ProjectName: beta,
			ProjectKey:  beta,
			ContentType: "insight",
			Content:     "移动端数据库选择 SQLite，离线优先。",
			Tags: strArrPtr("db", "sqlite"),
			Axes:        &MemoryAxes{Domain: []string{"mobile"}},
			IndexPath:   strArrPtr("decisions", "db", "edge"),
			SkipLLM:     true,
			Ts:          now + 2,
		},
		{
			OwnerID:     "personal",
			ProjectName: gamma,
			ProjectKey:  gamma,
			ContentType: "insight",
			Content:     "缓存使用 Redis，数据库不用于缓存层。",
			Tags: strArrPtr("cache", "redis"),
			Axes:        &MemoryAxes{Domain: []string{"infra"}},
			IndexPath:   strArrPtr("decisions", "cache"),
			SkipLLM:     true,
			Ts:          now + 3,
		},
	}

	for _, entry := range entries {
		if _, err := app.IngestMemory(ctx, entry); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	fullMode := "full"
	searchAll, err := app.SearchMemories(ctx, SearchInput{
		OwnerID: "personal",
		Query:   "数据库",
		Scope:   "all",
		Mode:    &fullMode,
		Limit:   20,
	})
	if err != nil {
		t.Fatalf("全量检索失败: %v", err)
	}
	if len(searchAll.Results) < 2 {
		t.Fatalf("全量检索结果过少: %d", len(searchAll.Results))
	}
	projectSet := map[string]bool{}
	for _, result := range searchAll.Results {
		projectSet[result.ProjectKey] = true
	}
	if len(projectSet) < 2 {
		t.Fatalf("多工程冲突样本未覆盖: %+v", projectSet)
	}

	searchAlpha, err := app.SearchMemories(ctx, SearchInput{
		OwnerID:     "personal",
		ProjectName: alpha,
		ProjectKey:  alpha,
		Query:       "数据库",
		Scope:       "all",
		Mode:        &fullMode,
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("项目内检索失败: %v", err)
	}
	if len(searchAlpha.Results) < 2 {
		t.Fatalf("项目内冲突结果过少: %d", len(searchAlpha.Results))
	}
	hasPostgres := false
	hasMySQL := false
	for _, result := range searchAlpha.Results {
		if strings.Contains(result.Snippet, "PostgreSQL") {
			hasPostgres = true
		}
		if strings.Contains(result.Snippet, "MySQL") {
			hasMySQL = true
		}
	}
	if !hasPostgres || !hasMySQL {
		t.Fatalf("冲突语义未同时命中: postgres=%v mysql=%v", hasPostgres, hasMySQL)
	}

	prefixIndexPath := []string{"decisions", "db"}
	searchPrefix, err := app.SearchMemories(ctx, SearchInput{
		OwnerID:   "personal",
		Query:     "数据库",
		Scope:     "all",
		Mode:      &fullMode,
		IndexPath: &prefixIndexPath,
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("前缀检索失败: %v", err)
	}
	for _, result := range searchPrefix.Results {
		if !hasIndexPathPrefix(result.IndexPath, []string{"decisions", "db"}) {
			t.Fatalf("前缀过滤失败: %+v", result.IndexPath)
		}
	}
}

func hasIndexPathPrefix(path, prefix []string) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

func TestRealChatLogsIntegration(t *testing.T) {
	if os.Getenv("AGENT_MEM_INTEGRATION") == "" {
		t.Skip("未设置 AGENT_MEM_INTEGRATION，跳过集成测试")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("无法获取用户目录: %v", err)
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

	samples := collectChatSamples(home)
	if len(samples) == 0 {
		t.Skip("未找到可用的对话记录样本")
	}

	now := time.Now().UTC().Unix()
	for idx, sample := range samples {
		content, err := readFileSample(sample.Path, 120*1024)
		if err != nil {
			t.Fatalf("读取样本失败: %v", err)
		}
		marker := "SOURCE " + strings.ToUpper(sample.Label)
		payload := IngestMemoryInput{
			OwnerID:     "personal",
			ProjectName: sample.ProjectKey,
			ProjectKey:  sample.ProjectKey,
			ContentType: "insight",
			Content:     marker + "\n" + content,
			Tags: strArrPtr("chatlog", sample.Label),
			Axes:        &MemoryAxes{Domain: []string{"assistant"}},
			IndexPath:   strArrPtr("dialogs", sample.Label),
			SkipLLM:     true,
			Ts:          now + int64(idx),
		}
		if _, err := app.IngestMemory(ctx, payload); err != nil {
			t.Fatalf("写入样本失败: %v", err)
		}
	}

	modeFullPtr := "full"
	for _, sample := range samples {
		marker := "SOURCE " + strings.ToUpper(sample.Label)
		sampleIndexPath := []string{"dialogs", sample.Label}
		searchResp, err := app.SearchMemories(ctx, SearchInput{
			OwnerID:   "personal",
			Query:     marker,
			Scope:     "all",
			Mode:      &modeFullPtr,
			IndexPath: &sampleIndexPath,
			Limit:     5,
		})
		if err != nil {
			t.Fatalf("检索失败: %v", err)
		}
		if len(searchResp.Results) == 0 {
			t.Fatalf("样本检索为空: %s", sample.Label)
		}
		if searchResp.Results[0].ProjectKey != sample.ProjectKey {
			t.Fatalf("检索项目不匹配: %s != %s", searchResp.Results[0].ProjectKey, sample.ProjectKey)
		}
	}
}

func TestMixedChatLogsSameProjectIntegration(t *testing.T) {
	if os.Getenv("AGENT_MEM_INTEGRATION") == "" {
		t.Skip("未设置 AGENT_MEM_INTEGRATION，跳过集成测试")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("无法获取用户目录: %v", err)
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

	samples := collectChatSamples(home)
	if len(samples) < 2 {
		t.Skip("可用样本不足，跳过混合测试")
	}

	projectKey := "chat-mixed"
	now := time.Now().UTC().Unix()
	for idx, sample := range samples {
		content, err := readFileSample(sample.Path, 80*1024)
		if err != nil {
			t.Fatalf("读取样本失败: %v", err)
		}
		marker := "MIXED SOURCE " + strings.ToUpper(sample.Label)
		payload := IngestMemoryInput{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     marker + "\n" + content,
			Tags: strArrPtr("chatlog", "mixed", sample.Label),
			Axes:        &MemoryAxes{Domain: []string{"assistant"}},
			IndexPath:   strArrPtr("dialogs", "mixed"),
			SkipLLM:     true,
			Ts:          now + int64(idx),
		}
		if _, err := app.IngestMemory(ctx, payload); err != nil {
			t.Fatalf("写入样本失败: %v", err)
		}
	}

	modeFull := "full"
	searchResp, err := app.SearchMemories(ctx, SearchInput{
		OwnerID:     "personal",
		ProjectName: projectKey,
		ProjectKey:  projectKey,
		Query:       "MIXED SOURCE",
		Scope:       "all",
		Mode:        &modeFull,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("混合检索失败: %v", err)
	}
	if len(searchResp.Results) < 2 {
		t.Fatalf("混合检索结果过少: %d", len(searchResp.Results))
	}
}

func TestMixedConflictChatIntegration(t *testing.T) {
	if os.Getenv("AGENT_MEM_INTEGRATION") == "" {
		t.Skip("未设置 AGENT_MEM_INTEGRATION，跳过集成测试")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("无法获取用户目录: %v", err)
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

	samples := collectChatSamples(home)
	if len(samples) < 2 {
		t.Skip("可用样本不足，跳过混合冲突测试")
	}

	projectKey := "chat-mixed-conflict"
	baseTs := time.Now().UTC().Unix() - int64(len(samples)) - 20

	conflicts := []IngestMemoryInput{
		{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     "冲突样本: 数据库选择 PostgreSQL，性能优先。",
			Tags: strArrPtr("db", "postgresql"),
			Axes:        &MemoryAxes{Domain: []string{"backend"}},
			IndexPath:   strArrPtr("dialogs", "mixed", "conflict"),
			SkipLLM:     true,
			Ts:          baseTs,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     "冲突样本: 数据库选择 MySQL，兼容优先。",
			Tags: strArrPtr("db", "mysql"),
			Axes:        &MemoryAxes{Domain: []string{"backend"}},
			IndexPath:   strArrPtr("dialogs", "mixed", "conflict"),
			SkipLLM:     true,
			Ts:          baseTs + 1,
		},
	}
	for _, entry := range conflicts {
		if _, err := app.IngestMemory(ctx, entry); err != nil {
			t.Fatalf("写入冲突样本失败: %v", err)
		}
	}

	for idx, sample := range samples {
		content, err := readFileSample(sample.Path, 60*1024)
		if err != nil {
			t.Fatalf("读取样本失败: %v", err)
		}
		marker := "MIXED CONFLICT SOURCE " + strings.ToUpper(sample.Label)
		payload := IngestMemoryInput{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     marker + "\n" + content,
			Tags: strArrPtr("chatlog", "mixed", sample.Label),
			Axes:        &MemoryAxes{Domain: []string{"assistant"}},
			IndexPath:   strArrPtr("dialogs", "mixed", "logs"),
			SkipLLM:     true,
			Ts:          baseTs + 10 + int64(idx),
		}
		if _, err := app.IngestMemory(ctx, payload); err != nil {
			t.Fatalf("写入样本失败: %v", err)
		}
	}

	modeFull := "full"
	mixedIndexPath := []string{"dialogs", "mixed"}
	searchResp, err := app.SearchMemories(ctx, SearchInput{
		OwnerID:     "personal",
		ProjectName: projectKey,
		ProjectKey:  projectKey,
		Query:       "数据库",
		Scope:       "all",
		Mode:        &modeFull,
		IndexPath:   &mixedIndexPath,
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("混合冲突检索失败: %v", err)
	}
	if len(searchResp.Results) < 2 {
		t.Fatalf("混合冲突检索结果过少: %d", len(searchResp.Results))
	}
	hasPostgres := false
	hasMySQL := false
	for _, result := range searchResp.Results {
		if strings.Contains(result.Snippet, "PostgreSQL") {
			hasPostgres = true
		}
		if strings.Contains(result.Snippet, "MySQL") {
			hasMySQL = true
		}
	}
	if !hasPostgres || !hasMySQL {
		t.Fatalf("冲突语义未同时命中: postgres=%v mysql=%v", hasPostgres, hasMySQL)
	}
}

func TestIndexStatsEvolutionIntegration(t *testing.T) {
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

	projectKey := "agent-mem-stats-evo"
	baseTs := time.Now().UTC().Unix() - 100
	initial := []IngestMemoryInput{
		{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     "root alpha one",
			IndexPath:   strArrPtr("root", "alpha"),
			SkipLLM:     true,
			Ts:          baseTs,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     "root alpha child",
			IndexPath:   strArrPtr("root", "alpha", "child"),
			SkipLLM:     true,
			Ts:          baseTs + 1,
		},
	}
	for _, entry := range initial {
		if _, err := app.IngestMemory(ctx, entry); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	first, err := app.Index(ctx, IndexInput{
		OwnerID:       "personal",
		ProjectName:   projectKey,
		ProjectKey:    projectKey,
		IndexPath:     strArrPtr("root"),
		Limit:         50,
		PathTreeDepth: 3,
		PathTreeWidth: 10,
	})
	if err != nil {
		t.Fatalf("索引失败: %v", err)
	}
	if first.Stats.TotalMemories != 2 {
		t.Fatalf("初始总量错误: %d", first.Stats.TotalMemories)
	}
	if first.Stats.MaxPathDepth < 1 {
		t.Fatalf("初始深度错误: %d", first.Stats.MaxPathDepth)
	}
	branchesBefore := len(first.PathTree)

	more := []IngestMemoryInput{
		{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     "root beta",
			IndexPath:   strArrPtr("root", "beta"),
			SkipLLM:     true,
			Ts:          baseTs + 2,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     "root gamma child",
			IndexPath:   strArrPtr("root", "gamma", "child"),
			SkipLLM:     true,
			Ts:          baseTs + 3,
		},
	}
	for _, entry := range more {
		if _, err := app.IngestMemory(ctx, entry); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	second, err := app.Index(ctx, IndexInput{
		OwnerID:       "personal",
		ProjectName:   projectKey,
		ProjectKey:    projectKey,
		IndexPath:     strArrPtr("root"),
		Limit:         50,
		PathTreeDepth: 3,
		PathTreeWidth: 10,
	})
	if err != nil {
		t.Fatalf("索引失败: %v", err)
	}
	if second.Stats.TotalMemories != 4 {
		t.Fatalf("二次总量错误: %d", second.Stats.TotalMemories)
	}
	if len(second.PathTree) <= branchesBefore {
		t.Fatalf("分支数量未提升: before=%d after=%d", branchesBefore, len(second.PathTree))
	}
}

func TestMetricsIntegration(t *testing.T) {
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

	projectKey := "agent-mem-metrics-test"
	baseTs := time.Now().UTC().Unix() - 30
	entries := []IngestMemoryInput{
		{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     "metrics alpha",
			IndexPath:   strArrPtr("root", "alpha"),
			SkipLLM:     true,
			Ts:          baseTs,
		},
		{
			OwnerID:     "personal",
			ProjectName: projectKey,
			ProjectKey:  projectKey,
			ContentType: "insight",
			Content:     "metrics beta",
			IndexPath:   strArrPtr("root", "beta"),
			SkipLLM:     true,
			Ts:          baseTs + 1,
		},
	}
	for _, entry := range entries {
		if _, err := app.IngestMemory(ctx, entry); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	metrics, err := app.Metrics(ctx, IndexInput{
		OwnerID:       "personal",
		ProjectName:   projectKey,
		ProjectKey:    projectKey,
		IndexPath:     strArrPtr("root"),
		Limit:         20,
		PathTreeDepth: 2,
		PathTreeWidth: 10,
	})
	if err != nil {
		t.Fatalf("指标获取失败: %v", err)
	}
	if metrics.Content == "" {
		t.Fatalf("指标输出为空")
	}
	if !strings.Contains(metrics.Content, "agent_mem_total_memories") {
		t.Fatalf("指标缺少总量: %s", metrics.Content)
	}
	if !strings.Contains(metrics.Content, "agent_mem_depth_distribution") {
		t.Fatalf("指标缺少深度分布: %s", metrics.Content)
	}
}

func TestMetricsCacheIntegration(t *testing.T) {
	if os.Getenv("AGENT_MEM_INTEGRATION") == "" {
		t.Skip("未设置 AGENT_MEM_INTEGRATION，跳过集成测试")
	}
	settings := defaultSettings()
	settings.Storage.DatabaseURL = envOrDefault("DATABASE_URL", settings.Storage.DatabaseURL)
	settings.Embedding.Provider = "mock"
	settings.Embedding.Dimension = 1536
	settings.Chunking = ChunkingConfig{ChunkSize: 500, Overlap: 50, ApproxCharsPerToken: 4}

	os.Setenv("AGENT_MEM_LLM_MODE", "mock")
	os.Setenv("AGENT_MEM_METRICS_CACHE_TTL", "60")
	app, err := NewApp(settings)
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	defer app.Close()

	ctx := context.Background()
	if err := app.EnsureSchema(ctx, true); err != nil {
		t.Fatalf("初始化表结构失败: %v", err)
	}

	projectKey := "agent-mem-metrics-cache-test"
	baseTs := time.Now().UTC().Unix() - 20
	if _, err := app.IngestMemory(ctx, IngestMemoryInput{
		OwnerID:     "personal",
		ProjectName: projectKey,
		ProjectKey:  projectKey,
		ContentType: "insight",
		Content:     "metrics cache sample",
		IndexPath:   strArrPtr("root", "cache"),
		SkipLLM:     true,
		Ts:          baseTs,
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	input := IndexInput{
		OwnerID:       "personal",
		ProjectName:   projectKey,
		ProjectKey:    projectKey,
		IndexPath:     strArrPtr("root"),
		Limit:         20,
		PathTreeDepth: 2,
		PathTreeWidth: 10,
	}
	first, err := app.Metrics(ctx, input)
	if err != nil {
		t.Fatalf("首次指标失败: %v", err)
	}
	second, err := app.Metrics(ctx, input)
	if err != nil {
		t.Fatalf("缓存指标失败: %v", err)
	}
	if first.Content != second.Content {
		t.Fatalf("缓存内容不一致")
	}
}

func TestIndexPathIndexesIntegration(t *testing.T) {
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

	indexNames := []string{"idx_memories_index_path_l1", "idx_memories_index_path_l2", "idx_memories_index_path_l3"}
	for _, name := range indexNames {
		var exists bool
		row := app.store.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname='public' AND indexname=$1)`, name)
		if err := row.Scan(&exists); err != nil {
			t.Fatalf("查询索引失败: %v", err)
		}
		if !exists {
			t.Fatalf("索引不存在: %s", name)
		}
	}
}

type chatSample struct {
	Label      string
	Path       string
	ProjectKey string
}

func collectChatSamples(home string) []chatSample {
	var samples []chatSample
	codexPath := filepath.Join(home, ".codex", "history.jsonl")
	if fileExists(codexPath) {
		samples = append(samples, chatSample{Label: "codex", Path: codexPath, ProjectKey: "chat-codex"})
	}
	claudePath := filepath.Join(home, ".claude", "history.jsonl")
	if fileExists(claudePath) {
		samples = append(samples, chatSample{Label: "claude", Path: claudePath, ProjectKey: "chat-claude"})
	}
	if geminiPath := findLatestGeminiChat(home); geminiPath != "" {
		samples = append(samples, chatSample{Label: "gemini", Path: geminiPath, ProjectKey: "chat-gemini"})
	}
	return samples
}

func findLatestGeminiChat(home string) string {
	pattern := filepath.Join(home, ".gemini", "tmp", "*", "chats", "session-*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	latest := ""
	var latestTime time.Time
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		if latest == "" || info.ModTime().After(latestTime) {
			latest = match
			latestTime = info.ModTime()
		}
	}
	return latest
}

func readFileSample(path string, maxBytes int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	reader := io.LimitReader(file, maxBytes)
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	content := string(data)
	if !utf8.ValidString(content) {
		content = strings.ToValidUTF8(content, "")
	}
	return content, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
