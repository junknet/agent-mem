package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestValidateIngestInput(t *testing.T) {
	input := IngestMemoryInput{
		OwnerID:     "personal",
		ProjectName: "",
		ProjectKey:  "",
		ContentType: "development",
		Content:     "hello",
		Ts:          time.Now().UTC().Unix(),
	}
	if err := validateIngestInput(input); err == nil {
		t.Fatalf("期望 project_name 为空时报错")
	}

	input.ProjectName = "test-project"
	input.ProjectKey = "test-project"
	input.ProjectPath = "relative/path"
	if err := validateIngestInput(input); err == nil {
		t.Fatalf("期望 project_path 非绝对路径时报错")
	}

	input.ProjectPath = "/test/project"
	input.ContentType = "invalid"
	if err := validateIngestInput(input); err == nil {
		t.Fatalf("期望 content_type 无效时报错")
	}
}

func TestChunkingFixedTokens(t *testing.T) {
	cfg := ChunkingConfig{ChunkSize: 500, Overlap: 50, ApproxCharsPerToken: 4}
	content := strings.Repeat("a", 2100)
	chunks := chunkContent(content, cfg)
	if len(chunks) != 2 {
		t.Fatalf("期望切分为 2 个片段，实际为 %d", len(chunks))
	}
	if len([]rune(chunks[0])) != 2000 {
		t.Fatalf("首片段长度错误: %d", len([]rune(chunks[0])))
	}
}

func TestEmbedderMockDeterministic(t *testing.T) {
	settings := defaultSettings()
	settings.Embedding.Provider = "mock"
	settings.Embedding.Dimension = 32
	embedder := NewEmbedder(settings)

	vector1, err := embedder.EmbedQuery("hello")
	if err != nil {
		t.Fatalf("向量化失败: %v", err)
	}
	vector2, err := embedder.EmbedQuery("hello")
	if err != nil {
		t.Fatalf("向量化失败: %v", err)
	}
	if len(vector1.Slice()) != 32 {
		t.Fatalf("向量维度错误")
	}
	for i, v := range vector1.Slice() {
		if v != vector2.Slice()[i] {
			t.Fatalf("向量结果不一致")
		}
	}
}

func TestNormalizeDatabaseURL(t *testing.T) {
	value := "postgresql+psycopg://user:pass@localhost:5432/db"
	normalized := normalizeDatabaseURL(value)
	if normalized != "postgresql://user:pass@localhost:5432/db" {
		t.Fatalf("数据库地址未归一化: %s", normalized)
	}
}

func TestValidationRejectNullContent(t *testing.T) {
	input := IngestMemoryInput{
		OwnerID:     "personal",
		ProjectName: "test-project",
		ProjectKey:  "test-project",
		ProjectPath: "/test",
		ContentType: "requirement",
		Content:     "abc\u0000def",
		Ts:          time.Now().UTC().Unix(),
	}
	if err := validateIngestInput(input); err == nil {
		t.Fatalf("期望 content 包含空字节时报错")
	}
}

func TestSearchQueryTooShort(t *testing.T) {
	input := SearchInput{
		OwnerID:     "personal",
		ProjectName: "test-project",
		ProjectKey:  "test-project",
		Query:       "a",
		Scope:       "all",
		Limit:       5,
	}
	if err := validateSearchInput(input); err == nil {
		t.Fatalf("期望 query 太短时报错")
	}
}

func TestSearchQueryNoMeaning(t *testing.T) {
	input := SearchInput{
		OwnerID:     "personal",
		ProjectName: "test-project",
		ProjectKey:  "test-project",
		Query:       "???!!!",
		Scope:       "all",
		Limit:       5,
	}
	if err := validateSearchInput(input); err == nil {
		t.Fatalf("期望 query 无有效内容时报错")
	}
}

func TestValidateProjectPathWindows(t *testing.T) {
	input := IngestMemoryInput{
		OwnerID:     "personal",
		ProjectName: "test-project",
		ProjectKey:  "test-project",
		ProjectPath: "C:\\Users\\test\\project",
		ContentType: "requirement",
		Content:     "ok",
		Ts:          time.Now().UTC().Unix(),
	}
	if err := validateIngestInput(input); err != nil {
		t.Fatalf("期望 Windows 路径通过校验: %v", err)
	}
}

func TestIntegrationEnabledFlag(t *testing.T) {
	if os.Getenv("AGENT_MEM_INTEGRATION") == "" {
		t.Skip("未设置 AGENT_MEM_INTEGRATION，跳过集成测试")
	}
}

func TestMockArbitrateIdentical(t *testing.T) {
	result := mockArbitrate("we chose PostgreSQL as database", "we chose PostgreSQL as database")
	if result != ArbitrateSkip {
		t.Fatalf("相同内容应返回 SKIP，实际: %s", result)
	}
}

func TestMockArbitrateHighOverlap(t *testing.T) {
	// mockArbitrate 使用 strings.Fields 分词，需要用空格分隔的文本
	oldSummary := "we decided to use PostgreSQL as database"
	newSummary := "we decided to use PostgreSQL as database with pgvector extension"
	result := mockArbitrate(newSummary, oldSummary)
	if result != ArbitrateReplace {
		t.Fatalf("高重叠内容应返回 REPLACE，实际: %s", result)
	}
}

func TestMockArbitrateLowOverlap(t *testing.T) {
	oldSummary := "frontend uses React framework for UI"
	newSummary := "backend uses Go language for API server"
	result := mockArbitrate(newSummary, oldSummary)
	if result != ArbitrateKeepBoth {
		t.Fatalf("低重叠内容应返回 KEEP_BOTH，实际: %s", result)
	}
}

func TestL2Normalize(t *testing.T) {
	vec := []float32{3, 4}
	normalized := l2Normalize(vec)
	// 3^2 + 4^2 = 25, sqrt(25) = 5
	// normalized: [0.6, 0.8]
	if len(normalized) != 2 {
		t.Fatalf("归一化后长度错误")
	}
	epsilon := float32(0.0001)
	if normalized[0] < 0.6-epsilon || normalized[0] > 0.6+epsilon {
		t.Fatalf("归一化结果错误: %v", normalized)
	}
	if normalized[1] < 0.8-epsilon || normalized[1] > 0.8+epsilon {
		t.Fatalf("归一化结果错误: %v", normalized)
	}
}

func TestL2NormalizeEmptyVector(t *testing.T) {
	vec := []float32{}
	normalized := l2Normalize(vec)
	if len(normalized) != 0 {
		t.Fatalf("空向量归一化应返回空向量")
	}
}

func TestAverageEmbedding(t *testing.T) {
	embeddings := [][]float32{
		{1, 2, 3},
		{3, 4, 5},
	}
	avg := averageEmbedding(embeddings, 3)
	if len(avg) != 3 {
		t.Fatalf("平均向量长度错误")
	}
	// (1+3)/2=2, (2+4)/2=3, (3+5)/2=4
	if avg[0] != 2 || avg[1] != 3 || avg[2] != 4 {
		t.Fatalf("平均向量计算错误: %v", avg)
	}
}

func TestDistanceToSimilarity(t *testing.T) {
	// distance=0 -> similarity=1
	if distanceToSimilarity(0) != 1 {
		t.Fatalf("距离0应转为相似度1")
	}
	// distance=1 -> similarity=0
	if distanceToSimilarity(1) != 0 {
		t.Fatalf("距离1应转为相似度0")
	}
	// distance=0.5 -> similarity=0.5
	if distanceToSimilarity(0.5) != 0.5 {
		t.Fatalf("距离0.5应转为相似度0.5")
	}
}
