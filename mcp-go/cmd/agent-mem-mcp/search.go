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

	initialLimit := limit * 5
	var vectorRows []FragmentRow
	vector, err := s.embedder.EmbedQuery(query)
	if err == nil {
		if projectScoped {
			vectorRows, err = s.store.SearchVectorFragments(ctx, vector, projectID, scope, initialLimit)
		} else {
			vectorRows, err = s.store.SearchVectorFragmentsByOwner(ctx, vector, input.OwnerID, scope, initialLimit)
		}
		if err != nil {
			return SearchResponse{}, err
		}
	}

	lexicalQuery := normalizeQuery(query)
	if lexicalQuery == "" {
		lexicalQuery = query
	}

	keywords := []string{lexicalQuery}
	if s.settings.QueryExpand.Enabled && s.llm != nil {
		expanded := s.llm.ExpandQuery(query)
		if len(expanded) > 0 {
			keywords = append(keywords, expanded...)
		}
	}
	keywords = uniqueStrings(keywords)

	var sources [][]FragmentRow
	sources = append(sources, vectorRows)

	for _, keyword := range keywords {
		if keyword == "" {
			continue
		}
		var keywordRows []FragmentRow
		if projectScoped {
			keywordRows, err = s.store.SearchKeywordFragments(ctx, keyword, projectID, scope, initialLimit)
		} else {
			keywordRows, err = s.store.SearchKeywordFragmentsByOwner(ctx, keyword, input.OwnerID, scope, initialLimit)
		}
		if err != nil {
			return SearchResponse{}, err
		}
		if len(keywordRows) > 0 {
			sources = append(sources, keywordRows)
		}

		var bm25Rows []FragmentRow
		if projectScoped {
			bm25Rows, err = s.store.SearchBM25Fragments(ctx, keyword, projectID, scope, initialLimit)
		} else {
			bm25Rows, err = s.store.SearchBM25FragmentsByOwner(ctx, keyword, input.OwnerID, scope, initialLimit)
		}
		if err != nil {
			return SearchResponse{}, err
		}
		if len(bm25Rows) > 0 {
			sources = append(sources, bm25Rows)
		}
	}

	combined := rrfMerge(sources...)
	if len(combined) == 0 {
		return SearchResponse{Results: []SearchResult{}, Metadata: SearchMetadata{Total: 0, Returned: 0, NextAction: "use_ids_to_call_mem_get"}}, nil
	}

	combined = dedupeByMemory(combined, limit*3)
	totalCount := len(combined)
	combined = maybeRerank(ctx, s, query, combined, limit)

	if len(combined) > limit {
		combined = combined[:limit]
	}

	results := make([]SearchResult, 0, len(combined))
	for _, row := range combined {
		snippet := buildSnippet(row.Content, 200)
		results = append(results, SearchResult{
			ID:          row.MemoryID,
			Snippet:     snippet,
			ContentType: row.ContentType,
			Score:       row.RankScore,
			Ts:          row.Ts,
			ChunkIndex:  row.ChunkIndex,
			TotalChunks: row.ChunkCount,
		})
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

func rrfMerge(lists ...[]FragmentRow) []FragmentRow {
	const k = 60.0
	combined := map[string]*FragmentRow{}
	for _, list := range lists {
		for idx, row := range list {
			rank := float64(idx + 1)
			score := 1.0 / (k + rank)
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

func maybeRerank(ctx context.Context, s *Searcher, query string, rows []FragmentRow, limit int) []FragmentRow {
	if !s.settings.Rerank.Enabled || s.llm == nil || len(rows) == 0 {
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
