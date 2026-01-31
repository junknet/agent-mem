package main

import (
	"context"
	"fmt"
	"strings"
)

func (a *App) Metrics(ctx context.Context, input IndexInput) (MetricsResponse, error) {
	normalized, err := normalizeIndexInput(input, a.settings)
	if err != nil {
		return MetricsResponse{}, err
	}
	if err := validateIndexInput(normalized); err != nil {
		return MetricsResponse{}, err
	}

	cacheKey := metricsCacheKey(normalized)
	if a.metrics != nil {
		if cached, ok := a.metrics.Get(cacheKey); ok {
			return cached, nil
		}
	}

	projectScoped := strings.TrimSpace(normalized.ProjectKey) != ""
	projectID := ""
	if projectScoped {
		projectID, err = a.store.FindProjectIDByKey(ctx, normalized.OwnerID, normalized.ProjectKey)
		if err != nil {
			return MetricsResponse{}, err
		}
		if projectID == "" {
			return MetricsResponse{Content: ""}, nil
		}
	}

	var indexPath []string
	if normalized.IndexPath != nil {
		indexPath = *normalized.IndexPath
	}
	counts, err := a.store.FetchMemoryCounts(ctx, projectID, normalized.OwnerID, indexPath)
	if err != nil {
		return MetricsResponse{}, err
	}
	depthDist, err := a.store.FetchIndexPathDepthDistribution(ctx, projectID, normalized.OwnerID, indexPath)
	if err != nil {
		return MetricsResponse{}, err
	}
	tree, err := a.store.FetchIndexPaths(ctx, projectID, normalized.OwnerID, normalized.Limit, indexPath)
	if err != nil {
		return MetricsResponse{}, err
	}
	pathsForTree := tree
	if len(indexPath) > 0 {
		pathsForTree = trimIndexPathCounts(tree, indexPath)
	}
	pathTree := buildIndexPathTree(pathsForTree, normalized.PathTreeDepth, normalized.PathTreeWidth)
	stats := buildIndexStats(counts, depthDist, pathTree, len(indexPath))

	var builder strings.Builder
	writeGauge(&builder, "agent_mem_total_memories", stats.TotalMemories, normalized)
	writeGauge(&builder, "agent_mem_axes_coverage", stats.AxesCoverage, normalized)
	writeGauge(&builder, "agent_mem_index_path_coverage", stats.IndexPathCoverage, normalized)
	writeGauge(&builder, "agent_mem_avg_path_depth", stats.AvgPathDepth, normalized)
	writeGauge(&builder, "agent_mem_max_path_depth", stats.MaxPathDepth, normalized)
	writeGauge(&builder, "agent_mem_branching_factor", stats.BranchingFactor, normalized)
	for _, item := range stats.DepthDistribution {
		writeDepthMetric(&builder, "agent_mem_depth_distribution", item.Depth, item.Count, normalized)
	}

	resp := MetricsResponse{Content: builder.String()}
	if a.metrics != nil {
		a.metrics.Set(cacheKey, resp)
	}
	return resp, nil
}

func writeGauge(builder *strings.Builder, name string, value any, input IndexInput) {
	labels := metricsLabels(input, "")
	fmt.Fprintf(builder, "%s%s %v\n", name, labels, value)
}

func writeDepthMetric(builder *strings.Builder, name string, depth int, count int, input IndexInput) {
	labels := metricsLabels(input, fmt.Sprintf("depth=\"%d\"", depth))
	fmt.Fprintf(builder, "%s%s %d\n", name, labels, count)
}

func metricsLabels(input IndexInput, extra string) string {
	var indexPathStr string
	if input.IndexPath != nil {
		indexPathStr = strings.Join(*input.IndexPath, "/")
	}
	base := []string{
		fmt.Sprintf("owner_id=\"%s\"", escapeLabel(input.OwnerID)),
		fmt.Sprintf("project_key=\"%s\"", escapeLabel(input.ProjectKey)),
		fmt.Sprintf("project_name=\"%s\"", escapeLabel(input.ProjectName)),
		fmt.Sprintf("path_prefix=\"%s\"", escapeLabel(indexPathStr)),
	}
	if extra != "" {
		base = append(base, extra)
	}
	return "{" + strings.Join(base, ",") + "}"
}

func escapeLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}

func metricsCacheKey(input IndexInput) string {
	var indexPathStr string
	if input.IndexPath != nil {
		indexPathStr = strings.Join(*input.IndexPath, "/")
	}
	key := strings.Join([]string{
		input.OwnerID,
		input.ProjectKey,
		input.ProjectName,
		indexPathStr,
		fmt.Sprintf("%d", input.Limit),
		fmt.Sprintf("%d", input.PathTreeDepth),
		fmt.Sprintf("%d", input.PathTreeWidth),
	}, "|")
	return "metrics:" + hashContent(key)
}
