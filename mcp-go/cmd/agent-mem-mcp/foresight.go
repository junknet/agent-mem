package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
)

const foresightMinContentLen = 200

// ForesightRow 前瞻记忆行
type ForesightRow struct {
	ID             string  `json:"id"`
	SourceMemoryID string  `json:"source_memory_id"`
	ProjectID      string  `json:"project_id"`
	Prediction     string  `json:"prediction"`
	RelevanceScore float64 `json:"relevance_score"`
	ExpiresAt      int64   `json:"expires_at"`
}

// ForesightInput mem.foresights 工具的输入
type ForesightInput struct {
	OwnerID    string `json:"owner_id"`
	MemoryID   string `json:"memory_id,omitempty"`
	ProjectKey string `json:"project_key,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

// ForesightResponse mem.foresights 工具的输出
type ForesightResponse struct {
	Foresights []ForesightRow `json:"foresights"`
	Metadata   SearchMetadata `json:"metadata"`
}

// GenerateForesights 基于一条记忆生成前瞻预测，异步调用不阻塞 ingest
func (a *App) GenerateForesights(ctx context.Context, memoryID string, projectID string, content string, summary string, contentType string) error {
	// 内容太短不生成前瞻
	if len([]rune(strings.TrimSpace(content))) <= foresightMinContentLen {
		return nil
	}

	// 调用 LLM 生成前瞻预测
	predictions := a.llm.PredictForesights(content, summary, contentType)
	if len(predictions) == 0 {
		return nil
	}

	// 对每条预测做 embedding 并写入数据库
	for _, prediction := range predictions {
		prediction = strings.TrimSpace(prediction)
		if prediction == "" {
			continue
		}

		foresightID := newForesightID()
		validDays := 14
		relevanceScore := 0.8

		// 生成 embedding
		embeddings, err := a.embedder.EmbedBatch(ctx, []string{prediction})
		if err != nil {
			log.Printf("[WARN] 前瞻 embedding 生成失败: %v", err)
			continue
		}
		if len(embeddings) == 0 || len(embeddings[0]) == 0 {
			continue
		}

		if err := a.store.InsertForesight(ctx, foresightID, memoryID, projectID, prediction, relevanceScore, validDays, embeddings[0]); err != nil {
			log.Printf("[WARN] 前瞻写入失败: %v", err)
			continue
		}
	}

	return nil
}

// newForesightID 生成前瞻 ID
func newForesightID() string {
	return "fore_" + newID()
}

// QueryForesights 查询前瞻记忆 (MCP 工具入口)
func (a *App) QueryForesights(ctx context.Context, input ForesightInput) (ForesightResponse, error) {
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

	memoryID := strings.TrimSpace(input.MemoryID)
	projectKey := strings.TrimSpace(input.ProjectKey)

	if memoryID == "" && projectKey == "" {
		return ForesightResponse{}, fmt.Errorf("memory_id 或 project_key 至少提供一个")
	}

	var rows []ForesightRow
	var err error

	if memoryID != "" {
		rows, err = a.store.FetchForesightsByMemory(ctx, memoryID, limit)
	} else {
		projectID, findErr := a.store.FindProjectIDByKey(ctx, ownerID, projectKey)
		if findErr != nil {
			return ForesightResponse{}, findErr
		}
		if projectID == "" {
			return ForesightResponse{
				Foresights: []ForesightRow{},
				Metadata:   SearchMetadata{Total: 0, Returned: 0},
			}, nil
		}
		rows, err = a.store.FetchForesightsByProject(ctx, projectID, limit)
	}

	if err != nil {
		return ForesightResponse{}, err
	}
	if rows == nil {
		rows = []ForesightRow{}
	}

	return ForesightResponse{
		Foresights: rows,
		Metadata: SearchMetadata{
			Total:    len(rows),
			Returned: len(rows),
		},
	}, nil
}

// SearchForesightsForQuery 在搜索流程中搜索前瞻记忆，返回命中的 source_memory_id
func (a *App) SearchForesightsForQuery(ctx context.Context, vector pgvector.Vector, projectID string, limit int) []string {
	// lazy cleanup: 每次搜索时顺便清理过期前瞻
	if cleaned, err := a.store.CleanExpiredForesights(ctx); err == nil && cleaned > 0 {
		log.Printf("[INFO] 清理过期前瞻: %d 条", cleaned)
	}

	rows, err := a.store.SearchForesightVectors(ctx, vector, projectID, limit)
	if err != nil {
		log.Printf("[WARN] 前瞻向量搜索失败: %v", err)
		return nil
	}

	// 去重收集 source_memory_id
	seen := map[string]bool{}
	var memoryIDs []string
	for _, row := range rows {
		if seen[row.SourceMemoryID] {
			continue
		}
		seen[row.SourceMemoryID] = true
		memoryIDs = append(memoryIDs, row.SourceMemoryID)
	}
	return memoryIDs
}

// SearchForesightsForQueryByOwner 按 owner 搜索前瞻记忆
func (a *App) SearchForesightsForQueryByOwner(ctx context.Context, vector pgvector.Vector, ownerID string, limit int) []string {
	// lazy cleanup
	if cleaned, err := a.store.CleanExpiredForesights(ctx); err == nil && cleaned > 0 {
		log.Printf("[INFO] 清理过期前瞻: %d 条", cleaned)
	}

	rows, err := a.store.SearchForesightVectorsByOwner(ctx, vector, ownerID, limit)
	if err != nil {
		log.Printf("[WARN] 前瞻向量搜索(owner)失败: %v", err)
		return nil
	}

	seen := map[string]bool{}
	var memoryIDs []string
	for _, row := range rows {
		if seen[row.SourceMemoryID] {
			continue
		}
		seen[row.SourceMemoryID] = true
		memoryIDs = append(memoryIDs, row.SourceMemoryID)
	}
	return memoryIDs
}

// foresightExpiresAt 根据 validDays 计算过期时间
func foresightExpiresAt(validDays int) time.Time {
	if validDays <= 0 {
		validDays = 14
	}
	return time.Now().UTC().Add(time.Duration(validDays) * 24 * time.Hour)
}
