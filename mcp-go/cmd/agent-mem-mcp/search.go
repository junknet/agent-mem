package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type Searcher struct {
	store    *Store
	llm      *LLMClient
	embedder *Embedder
	settings Settings
}

type SourceRows struct {
	Name string
	Rows []FragmentRow
}

func NewSearcher(store *Store, llm *LLMClient, embedder *Embedder, settings Settings) *Searcher {
	return &Searcher{store: store, llm: llm, embedder: embedder, settings: settings}
}

func (s *Searcher) Search(ctx context.Context, input SearchInput) (SearchResponse, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return SearchResponse{}, fmt.Errorf("query 不能为空")
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}

	scope := input.Scope
	if scope == "" {
		scope = "all"
	}

	axes := MemoryAxes{}
	if input.Axes != nil {
		axes = *input.Axes
	}
	var indexPath []string
	if input.IndexPath != nil {
		indexPath = *input.IndexPath
	}
	profile := derefString(input.Profile, "deep")
	mode := derefString(input.Mode, "compact")

	projectScoped := strings.TrimSpace(input.ProjectKey) != ""
	projectID := ""
	if projectScoped {
		var err error
		projectID, err = s.store.FindProjectIDByKey(ctx, input.OwnerID, input.ProjectKey)
		if err != nil {
			return SearchResponse{}, err
		}
		if projectID == "" {
			return SearchResponse{Results: []SearchResult{}, Metadata: SearchMetadata{Total: 0, Returned: 0, NextAction: "use_ids_to_call_mem_get"}}, nil
		}
	}

	initialMultiplier := 5
	switch profile {
	case "fast":
		initialMultiplier = 3
	case "deep":
		initialMultiplier = 8
	}
	initialLimit := limit * initialMultiplier
	var vectorRows []FragmentRow
	vector, err := s.embedder.EmbedQuery(query)
	if err == nil && s.embedder != nil && s.embedder.provider != "mock" {
		if projectScoped {
			vectorRows, err = s.store.SearchVectorFragments(ctx, vector, projectID, scope, axes, indexPath, initialLimit)
		} else {
			vectorRows, err = s.store.SearchVectorFragmentsByOwner(ctx, vector, input.OwnerID, scope, axes, indexPath, initialLimit)
		}
		if err != nil {
			return SearchResponse{}, err
		}
	}

	lexicalQuery := normalizeQuery(query)
	if lexicalQuery == "" {
		lexicalQuery = query
	}

	var sources []SourceRows

	var keywordRows []FragmentRow
	if projectScoped {
		keywordRows, err = s.store.SearchKeywordFragments(ctx, lexicalQuery, projectID, scope, axes, indexPath, initialLimit)
	} else {
		keywordRows, err = s.store.SearchKeywordFragmentsByOwner(ctx, lexicalQuery, input.OwnerID, scope, axes, indexPath, initialLimit)
	}
	if err != nil {
		return SearchResponse{}, err
	}
	if len(keywordRows) > 0 {
		sources = append(sources, SourceRows{Name: "keyword", Rows: keywordRows})
	}

	var bm25Rows []FragmentRow
	if projectScoped {
		bm25Rows, err = s.store.SearchBM25Fragments(ctx, lexicalQuery, projectID, scope, axes, indexPath, initialLimit)
	} else {
		bm25Rows, err = s.store.SearchBM25FragmentsByOwner(ctx, lexicalQuery, input.OwnerID, scope, axes, indexPath, initialLimit)
	}
	if err != nil {
		return SearchResponse{}, err
	}
	if len(bm25Rows) > 0 {
		sources = append(sources, SourceRows{Name: "bm25", Rows: bm25Rows})
	}

	// 关键词/BM25 已命中时，向量结果必须包含关键词，避免噪声召回
	if len(vectorRows) > 0 && (len(keywordRows) > 0 || len(bm25Rows) > 0) {
		tokens := tokenizeLexicalQuery(lexicalQuery)
		vectorRows = filterRowsByTokens(vectorRows, tokens)
	}
	if len(vectorRows) > 0 {
		sources = append(sources, SourceRows{Name: "vector", Rows: vectorRows})
	}

	useExpand := s.settings.QueryExpand.Enabled && s.llm != nil
	if useExpand && s.llm.mock {
		useExpand = false
	}
	if profile == "fast" {
		useExpand = false
	}
	if useExpand && profile != "deep" && len([]rune(query)) < 4 {
		useExpand = false
	}
	expandThreshold := limit
	if profile == "deep" {
		expandThreshold = limit * 2
	}
	baseCount := countUniqueMemories(sources)
	shouldExpand := useExpand && (baseCount < expandThreshold || (len(keywordRows) == 0 && len(bm25Rows) == 0))
	if shouldExpand {
		expanded := s.llm.ExpandQuery(query)
		for _, keyword := range uniqueStrings(expanded) {
			if keyword == "" || keyword == lexicalQuery {
				continue
			}
			var expandedKeywordRows []FragmentRow
			if projectScoped {
				expandedKeywordRows, err = s.store.SearchKeywordFragments(ctx, keyword, projectID, scope, axes, indexPath, initialLimit)
			} else {
				expandedKeywordRows, err = s.store.SearchKeywordFragmentsByOwner(ctx, keyword, input.OwnerID, scope, axes, indexPath, initialLimit)
			}
			if err != nil {
				return SearchResponse{}, err
			}
			if len(expandedKeywordRows) > 0 {
				sources = append(sources, SourceRows{Name: "keyword", Rows: expandedKeywordRows})
			}

			var expandedBM25Rows []FragmentRow
			if projectScoped {
				expandedBM25Rows, err = s.store.SearchBM25Fragments(ctx, keyword, projectID, scope, axes, indexPath, initialLimit)
			} else {
				expandedBM25Rows, err = s.store.SearchBM25FragmentsByOwner(ctx, keyword, input.OwnerID, scope, axes, indexPath, initialLimit)
			}
			if err != nil {
				return SearchResponse{}, err
			}
			if len(expandedBM25Rows) > 0 {
				sources = append(sources, SourceRows{Name: "bm25", Rows: expandedBM25Rows})
			}
		}
	}

	var (
		combined []FragmentRow
		traceMap map[string]*SearchTrace
	)
	if s.settings.SearchExplain.Enabled {
		combined, traceMap = rrfMergeWithTrace(sources...)
	} else {
		combined = rrfMerge(sources...)
	}
	if len(combined) == 0 {
		return SearchResponse{Results: []SearchResult{}, Metadata: SearchMetadata{Total: 0, Returned: 0, NextAction: "use_ids_to_call_mem_get"}}, nil
	}

	combined = dedupeByMemory(combined, limit*3)
	totalCount := len(combined)
	useRerank := s.settings.Rerank.Enabled && s.llm != nil
	if profile == "fast" {
		useRerank = false
	}
	if len(combined) <= limit {
		useRerank = false
	}
	if len(sources) <= 1 {
		useRerank = false
	}
	combined = maybeRerank(ctx, s, query, combined, limit, useRerank)

	if len(combined) > limit {
		combined = combined[:limit]
	}

	results := make([]SearchResult, 0, len(combined))
	for _, row := range combined {
		result := SearchResult{ID: row.MemoryID}
		if mode != "ids" {
			snippet := ""
			if mode != "compact" {
				snippet = buildSnippet(row.Content, 200)
			}
			result.Snippet = snippet
			result.ContentType = row.ContentType
			result.ProjectKey = row.ProjectKey
			result.Axes = axesPtr(row.Axes)
			result.IndexPath = row.IndexPath
			result.Score = row.RankScore
			result.Ts = row.Ts
			result.ChunkIndex = row.ChunkIndex
			result.TotalChunks = row.ChunkCount
		}
		if mode != "ids" && s.settings.SearchExplain.Enabled && traceMap != nil {
			if trace, ok := traceMap[row.FragmentID]; ok {
				result.Trace = trace
			}
		}
		results = append(results, result)
	}
	return SearchResponse{
		Results: results,
		Metadata: SearchMetadata{
			Total:      totalCount,
			Returned:   len(results),
			NextAction: "use_ids_to_call_mem_get",
		},
	}, nil
}

func rrfMerge(sources ...SourceRows) []FragmentRow {
	const k = 60.0
	combined := map[string]*FragmentRow{}
	for _, source := range sources {
		weight := rrfWeight(source.Name)
		for idx, row := range source.Rows {
			rank := float64(idx + 1)
			score := weight * (1.0 / (k + rank))
			key := row.FragmentID
			existing, ok := combined[key]
			if !ok {
				copyRow := row
				copyRow.RankScore = score
				combined[key] = &copyRow
				continue
			}
			existing.RankScore += score
		}
	}

	results := make([]FragmentRow, 0, len(combined))
	for _, row := range combined {
		results = append(results, *row)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].RankScore == results[j].RankScore {
			return results[i].Ts > results[j].Ts
		}
		return results[i].RankScore > results[j].RankScore
	})
	return results
}

func rrfMergeWithTrace(sources ...SourceRows) ([]FragmentRow, map[string]*SearchTrace) {
	const k = 60.0
	combined := map[string]*FragmentRow{}
	traces := map[string]*SearchTrace{}
	for _, source := range sources {
		weight := rrfWeight(source.Name)
		for idx, row := range source.Rows {
			rank := float64(idx + 1)
			score := weight * (1.0 / (k + rank))
			key := row.FragmentID
			existing, ok := combined[key]
			if !ok {
				copyRow := row
				copyRow.RankScore = score
				combined[key] = &copyRow
			} else {
				existing.RankScore += score
			}
			trace, ok := traces[key]
			if !ok {
				trace = &SearchTrace{Ranks: map[string]int{}}
				traces[key] = trace
			}
			if trace.Ranks == nil {
				trace.Ranks = map[string]int{}
			}
			if prev, ok := trace.Ranks[source.Name]; !ok || idx+1 < prev {
				trace.Ranks[source.Name] = idx + 1
			}
			if !stringInSlice(trace.Sources, source.Name) {
				trace.Sources = append(trace.Sources, source.Name)
			}
		}
	}

	results := make([]FragmentRow, 0, len(combined))
	for key, row := range combined {
		if trace, ok := traces[key]; ok {
			trace.RRFScore = row.RankScore
		}
		results = append(results, *row)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].RankScore == results[j].RankScore {
			return results[i].Ts > results[j].Ts
		}
		return results[i].RankScore > results[j].RankScore
	})
	return results, traces
}

func rrfWeight(name string) float64 {
	switch name {
	case "vector":
		return 0.4
	case "keyword":
		return 1.0
	case "bm25":
		return 1.0
	default:
		return 1.0
	}
}

func tokenizeLexicalQuery(query string) []string {
	parts := strings.Fields(query)
	if len(parts) == 0 {
		return nil
	}
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len([]rune(part)) < 2 {
			continue
		}
		tokens = append(tokens, strings.ToLower(part))
	}
	return tokens
}

func filterRowsByTokens(rows []FragmentRow, tokens []string) []FragmentRow {
	if len(tokens) == 0 {
		return rows
	}
	filtered := make([]FragmentRow, 0, len(rows))
	for _, row := range rows {
		content := strings.ToLower(row.Content)
		if len(tokens) == 1 {
			if strings.Contains(content, tokens[0]) {
				filtered = append(filtered, row)
			}
			continue
		}
		matchedAll := true
		for _, token := range tokens {
			if !strings.Contains(content, token) {
				matchedAll = false
				break
			}
		}
		if matchedAll {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func dedupeByMemory(rows []FragmentRow, limit int) []FragmentRow {
	if limit <= 0 {
		limit = len(rows)
	}
	seen := map[string]bool{}
	var result []FragmentRow
	for _, row := range rows {
		if seen[row.MemoryID] {
			continue
		}
		seen[row.MemoryID] = true
		result = append(result, row)
		if len(result) >= limit {
			return result
		}
	}
	return result
}

func maybeRerank(ctx context.Context, s *Searcher, query string, rows []FragmentRow, limit int, enabled bool) []FragmentRow {
	if !enabled || len(rows) == 0 {
		return rows
	}
	topN := s.settings.Rerank.TopN
	if topN <= 0 {
		topN = limit
	}
	if topN <= 0 {
		topN = 10
	}
	if topN > len(rows) {
		topN = len(rows)
	}
	docs := make([]string, 0, len(rows))
	for _, row := range rows {
		docs = append(docs, truncateRunes(strings.TrimSpace(row.Content), 2000))
	}
	results, err := s.llm.Rerank(query, docs, topN)
	if err != nil || len(results) == 0 {
		return rows
	}

	ordered := make([]FragmentRow, 0, len(results))
	seen := map[int]bool{}
	for _, item := range results {
		if item.Index < 0 || item.Index >= len(rows) {
			continue
		}
		if seen[item.Index] {
			continue
		}
		seen[item.Index] = true
		row := rows[item.Index]
		row.RankScore = item.RelevanceScore
		ordered = append(ordered, row)
	}
	if len(ordered) == 0 {
		return rows
	}

	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].RankScore == ordered[j].RankScore {
			return ordered[i].Ts > ordered[j].Ts
		}
		return ordered[i].RankScore > ordered[j].RankScore
	})
	return ordered
}

func buildSnippet(content string, limit int) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if len([]rune(trimmed)) <= limit {
		return trimmed
	}
	return truncateRunes(trimmed, limit) + "..."
}

func stringInSlice(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func countUniqueMemories(sources []SourceRows) int {
	seen := map[string]bool{}
	for _, source := range sources {
		for _, row := range source.Rows {
			if row.MemoryID == "" {
				continue
			}
			seen[row.MemoryID] = true
		}
	}
	return len(seen)
}

func normalizeQuery(query string) string {
	var builder strings.Builder
	lastSpace := false
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			builder.WriteRune(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(builder.String())
}

func derefString(ptr *string, defaultVal string) string {
	if ptr == nil {
		return defaultVal
	}
	return *ptr
}
