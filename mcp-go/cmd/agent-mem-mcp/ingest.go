package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
)

type IngestResult struct {
	ID     string
	Status string
}

const defaultSemanticUpdateCandidates = 20

func (a *App) IngestMemory(ctx context.Context, input IngestMemoryInput) (IngestResult, error) {
	normalized, err := normalizeIngestInput(input, a.settings, time.Now().UTC())
	if err != nil {
		return IngestResult{}, err
	}
	if err := validateIngestInput(normalized); err != nil {
		return IngestResult{}, err
	}
	input = normalized

	project, err := a.store.UpsertProject(ctx, input.OwnerID, input.ProjectKey, input.ProjectName, input.MachineName, input.ProjectPath)
	if err != nil {
		return IngestResult{}, fmt.Errorf("项目写入失败: %w", err)
	}

	contentHash := hashContent(input.Content)

	summary := a.llm.Summarize(input.Content)
	if summary == "" {
		summary = fallbackSummary(input.Content)
	}
	tags := a.llm.ExtractTags(input.Content)

	chunks := chunkContent(input.Content, a.settings.Chunking)
	if len(chunks) == 0 {
		return IngestResult{}, errors.New("内容切分失败")
	}

	embeddings, err := a.embedder.EmbedBatch(ctx, chunks)
	if err != nil {
		return IngestResult{}, fmt.Errorf("向量化失败: %w", err)
	}
	if len(embeddings) != len(chunks) {
		return IngestResult{}, errors.New("向量数量与片段数量不一致")
	}

	// 计算 memory 级别的平均向量（用于冲突检测和存储）
	avgVector := averageEmbedding(embeddings, a.embedder.dimension)
	avgVector = l2Normalize(avgVector)

	// 两层冲突检测：向量粗筛 + LLM 仲裁
	var action ArbitrateResult = ArbitrateKeepBoth // 默认新建
	semanticTargetID := ""
	semanticSimilarity := 0.0
	oldSummary := ""

	if len(avgVector) > 0 {
		threshold := semanticUpdateThreshold(a.settings.Versioning.SemanticSimilarityThreshold)
		vector := pgvector.NewVector(avgVector)

		// 第一层：向量粗筛（只按项目过滤，不按类型）
		candidateID, similarity, err := findSemanticUpdateCandidate(ctx, a.store, vector, project.ID, threshold, defaultSemanticUpdateCandidates)
		if err != nil {
			return IngestResult{}, fmt.Errorf("语义更新候选查找失败: %w", err)
		}

		// 第二层：LLM 仲裁（仅当向量相似度超过阈值时）
		if candidateID != "" && similarity >= threshold {
			semanticSimilarity = similarity
			semanticTargetID = candidateID
			// 获取旧摘要
			oldMemory, err := a.store.FetchMemorySummary(ctx, candidateID)
			if err == nil && oldMemory.Summary != "" {
				oldSummary = oldMemory.Summary
				// LLM 仲裁：比较新旧摘要
				action = a.llm.Arbitrate(summary, oldMemory.Summary)
			} else {
				// 获取旧摘要失败，保守处理：替换
				action = ArbitrateReplace
			}
		}
	}

	memoryID := newMemoryID()
	if (action == ArbitrateReplace || action == ArbitrateSkip) && semanticTargetID != "" {
		memoryID = semanticTargetID
	}

	if action == ArbitrateSkip {
		if semanticTargetID != "" {
			_ = a.store.InsertArbitrationLog(ctx, ArbitrationLogInsert{
				OwnerID:           input.OwnerID,
				ProjectID:         project.ID,
				CandidateMemoryID: semanticTargetID,
				NewMemoryID:       memoryID,
				Action:            string(action),
				Similarity:        semanticSimilarity,
				OldSummary:        oldSummary,
				NewSummary:        summary,
				Model:             a.settings.LLM.ModelArbitrate,
				CreatedAt:         time.Now().UTC(),
			})
		}
		return IngestResult{ID: memoryID, Status: "skipped"}, nil
	}

	memory := MemoryInsert{
		ID:           memoryID,
		ProjectID:    project.ID,
		ContentType:  input.ContentType,
		Content:      input.Content,
		ContentHash:  contentHash,
		Ts:           input.Ts,
		Summary:      summary,
		Tags:         tags,
		ChunkCount:   len(chunks),
		Embedded:     true,
		AvgEmbedding: avgVector,
		CreatedAt:    time.Now().UTC(),
	}

	tx, err := a.store.pool.Begin(ctx)
	if err != nil {
		return IngestResult{}, fmt.Errorf("事务开启失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if action == ArbitrateReplace && semanticTargetID != "" {
		if err := insertMemoryVersionFromMemoryTx(ctx, tx, memoryID); err != nil {
			return IngestResult{}, fmt.Errorf("保存旧版本失败: %w", err)
		}
		if err := insertArbitrationLogTx(ctx, tx, ArbitrationLogInsert{
			OwnerID:           input.OwnerID,
			ProjectID:         project.ID,
			CandidateMemoryID: semanticTargetID,
			NewMemoryID:       memoryID,
			Action:            string(action),
			Similarity:        semanticSimilarity,
			OldSummary:        oldSummary,
			NewSummary:        summary,
			Model:             a.settings.LLM.ModelArbitrate,
			CreatedAt:         time.Now().UTC(),
		}); err != nil {
			return IngestResult{}, fmt.Errorf("记录仲裁日志失败: %w", err)
		}
		// 替换模式：更新旧记忆，删除旧片段
		if err := updateMemoryTx(ctx, tx, memory); err != nil {
			return IngestResult{}, fmt.Errorf("更新记忆失败: %w", err)
		}
		if err := deleteFragmentsTx(ctx, tx, memoryID); err != nil {
			return IngestResult{}, fmt.Errorf("清理旧片段失败: %w", err)
		}
	} else {
		if semanticTargetID != "" {
			if err := insertArbitrationLogTx(ctx, tx, ArbitrationLogInsert{
				OwnerID:           input.OwnerID,
				ProjectID:         project.ID,
				CandidateMemoryID: semanticTargetID,
				NewMemoryID:       memoryID,
				Action:            string(action),
				Similarity:        semanticSimilarity,
				OldSummary:        oldSummary,
				NewSummary:        summary,
				Model:             a.settings.LLM.ModelArbitrate,
				CreatedAt:         time.Now().UTC(),
			}); err != nil {
				return IngestResult{}, fmt.Errorf("记录仲裁日志失败: %w", err)
			}
		}
		// 新建模式
		if err := insertMemoryTx(ctx, tx, memory); err != nil {
			return IngestResult{}, fmt.Errorf("写入记忆失败: %w", err)
		}
	}

	fragments := make([]FragmentInsert, 0, len(chunks))
	for idx, chunk := range chunks {
		fragments = append(fragments, FragmentInsert{
			ID:         newFragmentID(idx),
			MemoryID:   memoryID,
			ChunkIndex: idx,
			Content:    chunk,
			Embedding:  embeddings[idx],
		})
	}

	if err := insertFragmentsTx(ctx, tx, fragments); err != nil {
		return IngestResult{}, fmt.Errorf("写入片段失败: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return IngestResult{}, fmt.Errorf("事务提交失败: %w", err)
	}

	if action == ArbitrateReplace {
		return IngestResult{ID: memoryID, Status: "updated"}, nil
	}
	return IngestResult{ID: memoryID, Status: "created"}, nil
}

func insertMemoryTx(ctx context.Context, tx pgxTx, memory MemoryInsert) error {
	tagsJSON, _ := json.Marshal(memory.Tags)
	var avgVec any
	if len(memory.AvgEmbedding) > 0 {
		avgVec = pgvector.NewVector(memory.AvgEmbedding)
	}
	_, err := tx.Exec(ctx, `
INSERT INTO memories (
  id, project_id, content_type, content, content_hash, ts,
  summary, tags, chunk_count, embedding_done, avg_embedding
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11)`,
		memory.ID,
		memory.ProjectID,
		memory.ContentType,
		memory.Content,
		memory.ContentHash,
		memory.Ts,
		nullableString(memory.Summary),
		string(tagsJSON),
		memory.ChunkCount,
		memory.Embedded,
		avgVec,
	)
	return err
}

func updateMemoryTx(ctx context.Context, tx pgxTx, memory MemoryInsert) error {
	tagsJSON, _ := json.Marshal(memory.Tags)
	if strings.TrimSpace(memory.ID) == "" {
		return errors.New("记忆ID为空")
	}
	var avgVec any
	if len(memory.AvgEmbedding) > 0 {
		avgVec = pgvector.NewVector(memory.AvgEmbedding)
	}
	tag, err := tx.Exec(ctx, `
UPDATE memories
SET content_type = $2,
    content = $3,
    content_hash = $4,
    ts = $5,
    summary = $6,
    tags = $7::jsonb,
    chunk_count = $8,
    embedding_done = $9,
    avg_embedding = $10,
    updated_at = NOW()
WHERE id = $1`,
		memory.ID,
		memory.ContentType,
		memory.Content,
		memory.ContentHash,
		memory.Ts,
		nullableString(memory.Summary),
		string(tagsJSON),
		memory.ChunkCount,
		memory.Embedded,
		avgVec,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("目标记忆不存在")
	}
	return nil
}

func deleteFragmentsTx(ctx context.Context, tx pgxTx, memoryID string) error {
	if strings.TrimSpace(memoryID) == "" {
		return errors.New("记忆ID为空")
	}
	_, err := tx.Exec(ctx, `DELETE FROM fragments WHERE memory_id = $1`, memoryID)
	return err
}

func insertFragmentsTx(ctx context.Context, tx pgxTx, fragments []FragmentInsert) error {
	if len(fragments) == 0 {
		return nil
	}
	query := `
INSERT INTO fragments (id, memory_id, chunk_index, content, embedding)
VALUES ($1,$2,$3,$4,$5)`
	for _, frag := range fragments {
		if _, err := tx.Exec(ctx, query, frag.ID, frag.MemoryID, frag.ChunkIndex, frag.Content, pgvector.NewVector(frag.Embedding)); err != nil {
			return err
		}
	}
	return nil
}

func insertMemoryVersionFromMemoryTx(ctx context.Context, tx pgxTx, memoryID string) error {
	if strings.TrimSpace(memoryID) == "" {
		return errors.New("记忆ID为空")
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO memory_versions (
  memory_id, project_id, content_type, content, content_hash, ts,
  summary, tags, chunk_count, avg_embedding, created_at, replaced_at
)
SELECT id, project_id, content_type, content, content_hash, ts,
       summary, tags, chunk_count, avg_embedding, created_at, NOW()
FROM memories
WHERE id = $1`, memoryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("旧记忆不存在")
	}
	return nil
}

func insertArbitrationLogTx(ctx context.Context, tx pgxTx, log ArbitrationLogInsert) error {
	_, err := tx.Exec(ctx, `
INSERT INTO memory_arbitrations (
  owner_id, project_id, candidate_memory_id, new_memory_id,
  action, similarity, old_summary, new_summary, model, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		log.OwnerID,
		log.ProjectID,
		nullableString(log.CandidateMemoryID),
		nullableString(log.NewMemoryID),
		log.Action,
		log.Similarity,
		nullableString(log.OldSummary),
		nullableString(log.NewSummary),
		nullableString(log.Model),
		log.CreatedAt,
	)
	return err
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func fallbackSummary(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	return truncateRunes(trimmed, 100)
}

func averageEmbedding(embeddings [][]float32, dimension int) []float32 {
	if dimension <= 0 && len(embeddings) > 0 {
		dimension = len(embeddings[0])
	}
	if dimension <= 0 {
		return []float32{}
	}
	if len(embeddings) == 0 {
		return make([]float32, dimension)
	}
	sum := make([]float32, dimension)
	count := 0
	for _, vec := range embeddings {
		if len(vec) < dimension {
			continue
		}
		for i := 0; i < dimension; i++ {
			sum[i] += vec[i]
		}
		count++
	}
	if count == 0 {
		return make([]float32, dimension)
	}
	for i := 0; i < dimension; i++ {
		sum[i] /= float32(count)
	}
	return sum
}

// l2Normalize 对向量做 L2 归一化，使余弦相似度计算更准确
func l2Normalize(vec []float32) []float32 {
	if len(vec) == 0 {
		return vec
	}
	var sumSq float64
	for _, v := range vec {
		sumSq += float64(v) * float64(v)
	}
	if sumSq == 0 {
		return vec
	}
	norm := float32(1.0 / math.Sqrt(sumSq))
	result := make([]float32, len(vec))
	for i, v := range vec {
		result[i] = v * norm
	}
	return result
}

// findSemanticUpdateCandidate 使用 memory 级别的 avg_embedding 检测语义冲突
// 只按 project_id 过滤，不按 content_type（因为类型不严格互斥）
func findSemanticUpdateCandidate(ctx context.Context, store *Store, vector pgvector.Vector, projectID string, threshold float64, maxCandidates int) (string, float64, error) {
	if maxCandidates <= 0 {
		maxCandidates = defaultSemanticUpdateCandidates
	}
	if threshold <= 0 {
		threshold = 0.85
	}
	if threshold > 1 {
		threshold = 1
	}
	// 直接在 memory 级别做向量搜索，更准确
	rows, err := store.SearchMemoryVectors(ctx, vector, projectID, maxCandidates)
	if err != nil {
		return "", 0, err
	}
	if len(rows) == 0 {
		return "", 0, nil
	}
	// 取相似度最高的
	bestRow := rows[0]
	bestSim := distanceToSimilarity(bestRow.Distance)
	if bestSim < threshold {
		return "", bestSim, nil
	}
	return bestRow.ID, bestSim, nil
}

func distanceToSimilarity(distance float64) float64 {
	sim := 1 - distance
	if sim > 1 {
		return 1
	}
	if sim < -1 {
		return -1
	}
	return sim
}

func semanticUpdateThreshold(value float64) float64 {
	if value <= 0 {
		return 0.85
	}
	if value > 1 {
		return 1
	}
	return value
}
