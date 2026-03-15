package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
)

type LLMClient struct {
	settings     Settings
	client       *QwenClient
	mock         bool
	mu           sync.Mutex
	summaryCache map[string]cachedText
	tagsCache    map[string]cachedTags
	queryCache   map[string]cachedTags
	indexCache   map[string]cachedIndex
}

type cachedText struct {
	Value   string
	Expires time.Time
}

type cachedTags struct {
	Values  []string
	Expires time.Time
}

type cachedIndex struct {
	Axes    MemoryAxes
	Path    []string
	Expires time.Time
}

const (
	llmCacheTTL        = 30 * time.Minute
	llmCacheMaxEntries = 500
)

func NewLLMClient(settings Settings) *LLMClient {
	mock := strings.ToLower(envOrDefault("AGENT_MEM_LLM_MODE", "")) == "mock"
	return &LLMClient{
		settings:     settings,
		client:       NewQwenClient(settings),
		mock:         mock,
		summaryCache: map[string]cachedText{},
		tagsCache:    map[string]cachedTags{},
		queryCache:   map[string]cachedTags{},
		indexCache:   map[string]cachedIndex{},
	}
}

func (l *LLMClient) Summarize(content string) string {
	if l.mock {
		return mockSummary(content)
	}
	model := strings.TrimSpace(l.settings.LLM.ModelSummary)
	cacheKey := cacheKeyWithModel("summary", model, content)
	if cached, ok := l.getCachedText(l.summaryCache, cacheKey); ok {
		return cached
	}
	prompt := "请将以下文档内容压缩为 3-5 句摘要，突出核心结论。\n\n内容：\n" + truncate(content, 12000)
	raw, err := l.client.ChatCompletion(context.Background(), model, prompt, 0.2, 400)
	if err != nil {
		return ""
	}
	result := strings.TrimSpace(raw)
	if result != "" {
		l.setCachedText(l.summaryCache, cacheKey, result)
	}
	return result
}

func (l *LLMClient) ExtractTags(content string) []string {
	if l.mock {
		return fallbackTags(content)
	}
	model := strings.TrimSpace(l.settings.LLM.ModelSummary)
	cacheKey := cacheKeyWithModel("tags", model, content)
	if cached, ok := l.getCachedTags(l.tagsCache, cacheKey); ok {
		return cached
	}
	prompt := "请从以下文本中提取 3-10 个简短标签，输出 JSON 数组（字符串列表），不要输出其他内容。\n\n文本：\n" + truncate(content, 8000)
	raw, err := l.client.ChatCompletion(context.Background(), model, prompt, 0.2, 200)
	if err != nil {
		result := fallbackTags(content)
		l.setCachedTags(l.tagsCache, cacheKey, result)
		return result
	}
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "json"))
	}
	var tags []string
	if err := json.Unmarshal([]byte(cleaned), &tags); err == nil {
		result := normalizeTags(tags)
		l.setCachedTags(l.tagsCache, cacheKey, result)
		return result
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
		result := normalizeTags(fallback)
		l.setCachedTags(l.tagsCache, cacheKey, result)
		return result
	}
	result := fallbackTags(raw)
	l.setCachedTags(l.tagsCache, cacheKey, result)
	return result
}

func (l *LLMClient) ExtractIndex(contentType, summary string, tags []string, content string) (MemoryAxes, []string) {
	if !l.settings.Indexing.Enabled {
		return MemoryAxes{}, nil
	}
	if l.mock {
		return MemoryAxes{}, nil
	}
	model := strings.TrimSpace(l.settings.Indexing.Model)
	if model == "" {
		model = strings.TrimSpace(l.settings.LLM.ModelClassify)
	}
	if model == "" {
		model = l.settings.LLM.ModelSummary
	}
	cacheKey := cacheKeyWithModel("index", model, contentType+"|"+summary+"|"+strings.Join(tags, ",")+"|"+truncate(content, 1000))
	if cached, ok := l.getCachedIndex(cacheKey); ok {
		return cached.Axes, cached.Path
	}

	prompt := fmt.Sprintf(`你是记忆中心的索引器。请输出**机器友好**的纵横索引。

要求：
1) 只输出 JSON，不要输出其它内容。
2) axes 每个字段输出 0-5 个短词；优先小写英文或简短中文词，禁止句子。
3) index_path 输出 1-6 级目录路径，每个节点为短词；不要完整句子。
4) 输出结构：
{"axes":{"domain":[],"stack":[],"problem":[],"lifecycle":[],"component":[]},"index_path":[]}

输入：
content_type: %s
summary: %s
tags: %s
content: %s`, contentType, truncate(summary, 2000), truncate(strings.Join(tags, ","), 500), truncate(content, 2000))

	raw, err := l.client.ChatCompletion(context.Background(), model, prompt, 0.2, 300)
	if err != nil {
		return MemoryAxes{}, nil
	}
	data := parseJSON(raw)
	if data == nil {
		return MemoryAxes{}, nil
	}

	axes := extractAxesFromPayload(data)
	indexPath := getStringSlice(data, "index_path")

	normalizedAxes := normalizeAxesInput(&axes)
	if normalizedAxes == nil {
		resultPath := normalizeIndexPath(indexPath)
		l.setCachedIndex(cacheKey, MemoryAxes{}, resultPath)
		return MemoryAxes{}, resultPath
	}
	resultAxes := *normalizedAxes
	resultPath := normalizeIndexPath(indexPath)
	l.setCachedIndex(cacheKey, resultAxes, resultPath)
	return resultAxes, resultPath
}

func extractAxesFromPayload(payload map[string]any) MemoryAxes {
	if axesData, ok := payload["axes"]; ok {
		if axesMap, ok := axesData.(map[string]any); ok {
			return axesFromMap(axesMap)
		}
	}
	return axesFromMap(payload)
}

func axesFromMap(data map[string]any) MemoryAxes {
	return MemoryAxes{
		Domain:    getStringSlice(data, "domain"),
		Stack:     getStringSlice(data, "stack"),
		Problem:   getStringSlice(data, "problem"),
		Lifecycle: getStringSlice(data, "lifecycle"),
		Component: getStringSlice(data, "component"),
	}
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
	cacheKey := cacheKeyWithModel("query", model, fmt.Sprintf("%d|%s", maxKeywords, query))
	if cached, ok := l.getCachedTags(l.queryCache, cacheKey); ok {
		return cached
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
		result := limitTags(normalizeTags(items), maxKeywords)
		l.setCachedTags(l.queryCache, cacheKey, result)
		return result
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
		result := limitTags(normalizeTags(fallback), maxKeywords)
		l.setCachedTags(l.queryCache, cacheKey, result)
		return result
	}
	return fallbackQueryKeywords(query, maxKeywords)
}

func (l *LLMClient) getCachedText(cache map[string]cachedText, key string) (string, bool) {
	if key == "" {
		return "", false
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := cache[key]
	if !ok {
		return "", false
	}
	if entry.Expires.Before(now) {
		delete(cache, key)
		return "", false
	}
	return entry.Value, true
}

func (l *LLMClient) setCachedText(cache map[string]cachedText, key, value string) {
	if key == "" || value == "" {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(cache) >= llmCacheMaxEntries {
		pruneTextCache(cache, now)
	}
	cache[key] = cachedText{
		Value:   value,
		Expires: now.Add(llmCacheTTL),
	}
}

func (l *LLMClient) getCachedTags(cache map[string]cachedTags, key string) ([]string, bool) {
	if key == "" {
		return nil, false
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := cache[key]
	if !ok {
		return nil, false
	}
	if entry.Expires.Before(now) {
		delete(cache, key)
		return nil, false
	}
	return cloneStringSlice(entry.Values), true
}

func (l *LLMClient) setCachedTags(cache map[string]cachedTags, key string, values []string) {
	if key == "" || len(values) == 0 {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(cache) >= llmCacheMaxEntries {
		pruneTagsCache(cache, now)
	}
	cache[key] = cachedTags{
		Values:  cloneStringSlice(values),
		Expires: now.Add(llmCacheTTL),
	}
}

func (l *LLMClient) getCachedIndex(key string) (cachedIndex, bool) {
	if key == "" {
		return cachedIndex{}, false
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.indexCache[key]
	if !ok {
		return cachedIndex{}, false
	}
	if entry.Expires.Before(now) {
		delete(l.indexCache, key)
		return cachedIndex{}, false
	}
	return cachedIndex{
		Axes:    cloneAxes(entry.Axes),
		Path:    cloneStringSlice(entry.Path),
		Expires: entry.Expires,
	}, true
}

func (l *LLMClient) setCachedIndex(key string, axes MemoryAxes, path []string) {
	if key == "" {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.indexCache) >= llmCacheMaxEntries {
		pruneIndexCache(l.indexCache, now)
	}
	l.indexCache[key] = cachedIndex{
		Axes:    cloneAxes(axes),
		Path:    cloneStringSlice(path),
		Expires: now.Add(llmCacheTTL),
	}
}

func pruneTextCache(cache map[string]cachedText, now time.Time) {
	for key, entry := range cache {
		if entry.Expires.Before(now) {
			delete(cache, key)
		}
	}
	pruneExcessEntries(len(cache), func() bool {
		for key := range cache {
			delete(cache, key)
			if len(cache) <= cacheTargetSize() {
				return true
			}
		}
		return true
	})
}

func pruneTagsCache(cache map[string]cachedTags, now time.Time) {
	for key, entry := range cache {
		if entry.Expires.Before(now) {
			delete(cache, key)
		}
	}
	pruneExcessEntries(len(cache), func() bool {
		for key := range cache {
			delete(cache, key)
			if len(cache) <= cacheTargetSize() {
				return true
			}
		}
		return true
	})
}

func pruneIndexCache(cache map[string]cachedIndex, now time.Time) {
	for key, entry := range cache {
		if entry.Expires.Before(now) {
			delete(cache, key)
		}
	}
	pruneExcessEntries(len(cache), func() bool {
		for key := range cache {
			delete(cache, key)
			if len(cache) <= cacheTargetSize() {
				return true
			}
		}
		return true
	})
}

func pruneExcessEntries(size int, evict func() bool) {
	if size < llmCacheMaxEntries {
		return
	}
	evict()
}

func cacheTargetSize() int {
	if llmCacheMaxEntries <= 0 {
		return 0
	}
	target := llmCacheMaxEntries - llmCacheMaxEntries/10
	if target <= 0 {
		target = 1
	}
	return target
}

func cacheKey(prefix, content string) string {
	return prefix + ":" + hashString(content)
}

func cacheKeyWithModel(prefix, model, content string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return cacheKey(prefix, content)
	}
	return prefix + ":" + hashString(model+"|"+content)
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func cloneAxes(axes MemoryAxes) MemoryAxes {
	return MemoryAxes{
		Domain:    cloneStringSlice(axes.Domain),
		Stack:     cloneStringSlice(axes.Stack),
		Problem:   cloneStringSlice(axes.Problem),
		Lifecycle: cloneStringSlice(axes.Lifecycle),
		Component: cloneStringSlice(axes.Component),
	}
}

// Distill 将多条记忆摘要蒸馏为一段精炼的知识总结
func (l *LLMClient) Distill(summaries []string, projectKey string) string {
	if len(summaries) == 0 {
		return ""
	}
	if l.mock {
		// mock 模式：拼接前 3 条作为蒸馏结果
		var parts []string
		for i, s := range summaries {
			if i >= 3 {
				break
			}
			parts = append(parts, strings.TrimSpace(s))
		}
		return "[蒸馏] " + strings.Join(parts, "；")
	}

	model := strings.TrimSpace(l.settings.LLM.ModelDistill)
	if model == "" {
		model = strings.TrimSpace(l.settings.LLM.ModelSummary)
	}

	// 拼接所有摘要，控制总长度
	var sb strings.Builder
	for i, s := range summaries {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(s)))
	}
	summaryBlock := truncate(sb.String(), 12000)

	prompt := fmt.Sprintf(`你是知识蒸馏专家。请将以下来自项目「%s」的多条工作记忆摘要蒸馏为一段精炼的长期知识总结。

要求：
1. 去除重复信息，合并相似内容
2. 提取关键发现、决策和结论
3. 保留因果链和依赖关系（如 A 导致 B，因为 C 所以选择 D）
4. 保留重要的技术细节和数据指标
5. 输出结构清晰的知识总结，使用层级标题
6. 无损核心信息，但去除冗余表述

原始记忆摘要（共 %d 条）：
%s

请输出蒸馏后的知识总结：`, projectKey, len(summaries), summaryBlock)

	raw, err := l.client.ChatCompletion(context.Background(), model, prompt, 0.3, 2000)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

// PredictForesights 基于记忆内容生成 1-3 条前瞻预测
func (l *LLMClient) PredictForesights(content, summary, contentType string) []string {
	if l.mock {
		return mockForesights(summary)
	}
	model := strings.TrimSpace(l.settings.LLM.ModelSummary)
	if model == "" {
		model = "qwen-turbo"
	}

	prompt := fmt.Sprintf(`你是知识库的前瞻预测器。基于以下记忆内容，预测用户接下来可能需要什么信息或做什么操作。

【记忆类型】%s
【摘要】%s
【内容】%s

请输出 JSON 数组，包含 1-3 条简短预测字符串，每条描述一个用户可能的下一步需求。
示例：["需要查询相关的API文档","可能要实现对应的单元测试","接下来可能需要部署配置"]

只输出 JSON 数组，不要输出其他内容。`, contentType, truncate(summary, 2000), truncate(content, 6000))

	raw, err := l.client.ChatCompletion(context.Background(), model, prompt, 0.3, 300)
	if err != nil {
		return nil
	}

	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "json"))
	}

	var predictions []string
	if err := json.Unmarshal([]byte(cleaned), &predictions); err == nil {
		if len(predictions) > 3 {
			predictions = predictions[:3]
		}
		return predictions
	}

	// 尝试提取 JSON 数组
	start := strings.Index(cleaned, "[")
	end := strings.LastIndex(cleaned, "]")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(cleaned[start:end+1]), &predictions); err == nil {
			if len(predictions) > 3 {
				predictions = predictions[:3]
			}
			return predictions
		}
	}

	return nil
}

// mockForesights 测试用前瞻生成
func mockForesights(summary string) []string {
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	return []string{"可能需要相关文档", "接下来可能进行测试"}
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
