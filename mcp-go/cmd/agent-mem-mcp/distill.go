package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DistillInput 蒸馏工具输入参数
type DistillInput struct {
	OwnerID           string `json:"owner_id"`
	ProjectKey        string `json:"project_key"`
	Scope             string `json:"scope,omitempty"`
	SinceDays         int    `json:"since_days,omitempty"`
	TargetContentType string `json:"target_content_type,omitempty"`
}

// DistillOutput 蒸馏工具输出
type DistillOutput struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	SourceCount int    `json:"source_count"`
	Summary     string `json:"summary"`
}

const (
	defaultDistillSinceDays      = 7
	defaultDistillContentType    = "insight"
	maxDistillSourceMemories     = 50
)

// DistillMemories 蒸馏指定项目最近 N 天的记忆，浓缩为精华长期知识
func (a *App) DistillMemories(ctx context.Context, input DistillInput) (DistillOutput, error) {
	// === 参数规范化 ===
	ownerID := strings.TrimSpace(input.OwnerID)
	if ownerID == "" {
		ownerID = a.settings.Project.OwnerID
	}
	if ownerID == "" {
		ownerID = defaultOwnerID
	}

	projectKey := strings.TrimSpace(input.ProjectKey)
	if projectKey == "" {
		return DistillOutput{}, newValidationError("invalid_request", "ERR_INVALID_PROJECT", "project_key 不能为空", 400)
	}

	scope := strings.TrimSpace(input.Scope)
	sinceDays := input.SinceDays
	if sinceDays <= 0 {
		sinceDays = defaultDistillSinceDays
	}

	targetContentType := strings.TrimSpace(input.TargetContentType)
	if targetContentType == "" {
		targetContentType = defaultDistillContentType
	}

	// === 查找项目 ID ===
	projectID, err := a.store.FindProjectIDByKey(ctx, ownerID, projectKey)
	if err != nil {
		return DistillOutput{}, fmt.Errorf("查找项目失败: %w", err)
	}
	if projectID == "" {
		return DistillOutput{}, newValidationError("invalid_request", "ERR_PROJECT_NOT_FOUND", "项目不存在: "+projectKey, 404)
	}

	// === 查询最近 N 天的记忆摘要 ===
	sinceTs := time.Now().UTC().Add(-time.Duration(sinceDays) * 24 * time.Hour).Unix()
	rows, err := a.store.FetchRecentMemorySummaries(ctx, projectID, sinceTs, scope, maxDistillSourceMemories)
	if err != nil {
		return DistillOutput{}, fmt.Errorf("查询记忆摘要失败: %w", err)
	}
	if len(rows) == 0 {
		return DistillOutput{
			Status:      "empty",
			SourceCount: 0,
			Summary:     "没有找到可蒸馏的记忆",
		}, nil
	}

	// === 拼接摘要上下文 ===
	summaries := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Summary) != "" {
			summaries = append(summaries, row.Summary)
		}
	}
	if len(summaries) == 0 {
		return DistillOutput{
			Status:      "empty",
			SourceCount: len(rows),
			Summary:     "源记忆均无有效摘要",
		}, nil
	}

	// === 调用 LLM 蒸馏 ===
	distilled := a.llm.Distill(summaries, projectKey)
	if strings.TrimSpace(distilled) == "" {
		return DistillOutput{}, fmt.Errorf("LLM 蒸馏返回空结果")
	}

	// === 将蒸馏结果通过 IngestMemory 写入 ===
	tags := []string{"distilled", "auto-digest"}
	indexPath := []string{"distill", projectKey, "latest"}
	now := time.Now().UTC()

	ingestInput := IngestMemoryInput{
		OwnerID:     ownerID,
		ProjectKey:  projectKey,
		ProjectName: projectKey,
		ContentType: targetContentType,
		Content:     distilled,
		Summary:     truncateRunes(distilled, 300),
		Tags:        &tags,
		IndexPath:   &indexPath,
		Ts:          now.Unix(),
	}

	normalized, err := normalizeIngestInput(ingestInput, a.settings, now)
	if err != nil {
		return DistillOutput{}, fmt.Errorf("蒸馏结果参数规范化失败: %w", err)
	}

	result, err := a.IngestMemory(ctx, normalized)
	if err != nil {
		return DistillOutput{}, fmt.Errorf("蒸馏结果写入失败: %w", err)
	}

	return DistillOutput{
		ID:          result.ID,
		Status:      "distilled",
		SourceCount: len(summaries),
		Summary:     truncateRunes(distilled, 500),
	}, nil
}
