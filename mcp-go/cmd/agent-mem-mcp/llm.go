package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

type LLMClient struct {
	settings Settings
	client   *QwenClient
	mock     bool
}

func NewLLMClient(settings Settings) *LLMClient {
	mock := strings.ToLower(envOrDefault("AGENT_MEM_LLM_MODE", "")) == "mock"
	return &LLMClient{
		settings: settings,
		client:   NewQwenClient(settings),
		mock:     mock,
	}
}

func (l *LLMClient) Summarize(content string) string {
	if l.mock {
		return mockSummary(content)
	}
	prompt := "请将以下文档内容压缩为 3-5 句摘要，突出核心结论。\n\n内容：\n" + truncate(content, 12000)
	raw, err := l.client.ChatCompletion(context.Background(), l.settings.LLM.ModelSummary, prompt, 0.2, 400)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

func (l *LLMClient) ExtractTags(content string) []string {
	if l.mock {
		return fallbackTags(content)
	}
	prompt := "请从以下文本中提取 3-10 个简短标签，输出 JSON 数组（字符串列表），不要输出其他内容。\n\n文本：\n" + truncate(content, 8000)
	raw, err := l.client.ChatCompletion(context.Background(), l.settings.LLM.ModelSummary, prompt, 0.2, 200)
	if err != nil {
		return fallbackTags(content)
	}
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "json"))
	}
	var tags []string
	if err := json.Unmarshal([]byte(cleaned), &tags); err == nil {
		return normalizeTags(tags)
	}
	if parsed := parseJSONArray(raw); parsed != nil {
		var fallback []string
		for _, item := range parsed {
			for _, value := range item {
				if s, ok := value.(string); ok {
					fallback = append(fallback, s)
				}
			}
		}
		return normalizeTags(fallback)
	}
	return fallbackTags(raw)
}

func (l *LLMClient) ExpandQuery(query string) []string {
	if !l.settings.QueryExpand.Enabled {
		return fallbackQueryKeywords(query, l.settings.QueryExpand.MaxKeywords)
	}
	if l.mock {
		return fallbackQueryKeywords(query, l.settings.QueryExpand.MaxKeywords)
	}
	model := strings.TrimSpace(l.settings.QueryExpand.Model)
	if model == "" {
		model = l.settings.LLM.ModelSummary
	}
	maxKeywords := l.settings.QueryExpand.MaxKeywords
	if maxKeywords <= 0 {
		maxKeywords = 6
	}
	prompt := fmt.Sprintf("请将以下检索问题扩展为 %d 个以内的关键词或同义短语，输出 JSON 数组（字符串列表），不要输出其他内容。\\n\\n问题：\\n%s", maxKeywords, truncate(query, 2000))
	raw, err := l.client.ChatCompletion(context.Background(), model, prompt, 0.2, 200)
	if err != nil {
		return fallbackQueryKeywords(query, maxKeywords)
	}
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "json"))
	}
	var items []string
	if err := json.Unmarshal([]byte(cleaned), &items); err == nil {
		return limitTags(normalizeTags(items), maxKeywords)
	}
	if parsed := parseJSONArray(raw); parsed != nil {
		var fallback []string
		for _, item := range parsed {
			for _, value := range item {
				if s, ok := value.(string); ok {
					fallback = append(fallback, s)
				}
			}
		}
		return limitTags(normalizeTags(fallback), maxKeywords)
	}
	return fallbackQueryKeywords(query, maxKeywords)
}

func (l *LLMClient) Rerank(query string, documents []string, topN int) ([]RerankResult, error) {
	if l.mock {
		return nil, nil
	}
	if topN <= 0 {
		topN = 10
	}
	model := strings.TrimSpace(l.settings.Rerank.Model)
	if model == "" {
		return nil, fmt.Errorf("缺少 rerank 模型配置")
	}
	return l.client.Rerank(context.Background(), model, query, documents, topN)
}

// ArbitrateResult 仲裁结果
type ArbitrateResult string

const (
	ArbitrateReplace  ArbitrateResult = "REPLACE"   // 新内容替换旧内容
	ArbitrateKeepBoth ArbitrateResult = "KEEP_BOTH" // 保留两者，新建记忆
	ArbitrateSkip     ArbitrateResult = "SKIP"      // 跳过，不写入
)

// Arbitrate 判断新知识与已有知识的关系
// 输入：新摘要、旧摘要
// 输出：REPLACE / KEEP_BOTH / SKIP
func (l *LLMClient) Arbitrate(newSummary, oldSummary string) ArbitrateResult {
	if l.mock {
		// mock 模式：简单规则判断
		return mockArbitrate(newSummary, oldSummary)
	}

	model := strings.TrimSpace(l.settings.LLM.ModelArbitrate)
	if model == "" {
		model = "qwen-flash" // 默认用便宜快速的模型
	}

	prompt := fmt.Sprintf(`你是知识库管理员。判断新知识与已有知识的关系。

【已有知识摘要】
%s

【新知识摘要】
%s

请判断：
1. 如果新知识是旧知识的更新/修正/补充版本（同一主题的迭代）→ 输出 REPLACE
2. 如果新旧知识主题不同，只是表述相似（不同主题）→ 输出 KEEP_BOTH
3. 如果新旧知识几乎完全相同，无新增价值（重复内容）→ 输出 SKIP

只输出一个词：REPLACE 或 KEEP_BOTH 或 SKIP`, oldSummary, newSummary)

	raw, err := l.client.ChatCompletion(context.Background(), model, prompt, 0.1, 20)
	if err != nil {
		// 出错时保守处理：保留两者
		return ArbitrateKeepBoth
	}

	result := strings.TrimSpace(strings.ToUpper(raw))
	switch {
	case strings.Contains(result, "REPLACE"):
		return ArbitrateReplace
	case strings.Contains(result, "SKIP"):
		return ArbitrateSkip
	default:
		return ArbitrateKeepBoth
	}
}

// mockArbitrate 简单规则判断（测试用）
func mockArbitrate(newSummary, oldSummary string) ArbitrateResult {
	// 完全相同 -> SKIP
	if strings.TrimSpace(newSummary) == strings.TrimSpace(oldSummary) {
		return ArbitrateSkip
	}
	// 有较多重叠 -> REPLACE（简化判断）
	newWords := strings.Fields(newSummary)
	oldWords := strings.Fields(oldSummary)
	if len(newWords) == 0 || len(oldWords) == 0 {
		return ArbitrateKeepBoth
	}
	overlap := 0
	oldSet := make(map[string]bool)
	for _, w := range oldWords {
		oldSet[w] = true
	}
	for _, w := range newWords {
		if oldSet[w] {
			overlap++
		}
	}
	overlapRatio := float64(overlap) / float64(len(newWords))
	if overlapRatio > 0.5 {
		return ArbitrateReplace
	}
	return ArbitrateKeepBoth
}

func fallbackTags(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return []string{}
	}
	candidates := strings.FieldsFunc(trimmed, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsNumber(r))
	})
	var tags []string
	for _, item := range candidates {
		item = strings.TrimSpace(item)
		if len([]rune(item)) < 2 {
			continue
		}
		tags = append(tags, item)
		if len(tags) >= 10 {
			break
		}
	}
	return normalizeTags(tags)
}

func fallbackQueryKeywords(query string, max int) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return []string{}
	}
	normalized := normalizeQuery(query)
	if normalized == "" {
		return []string{query}
	}
	parts := strings.Fields(normalized)
	if max <= 0 {
		max = 6
	}
	if len(parts) > max {
		parts = parts[:max]
	}
	return normalizeTags(parts)
}

func limitTags(tags []string, max int) []string {
	if max <= 0 || len(tags) <= max {
		return tags
	}
	return tags[:max]
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func mockSummary(content string) string {
	lines := strings.Split(content, "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts = append(parts, line)
		if len(parts) >= 3 {
			break
		}
	}
	return strings.Join(parts, "；")
}
