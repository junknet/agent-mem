package main

import (
	"fmt"
	"testing"
	"time"
)

func TestNormalizeAxesAndIndexPath(t *testing.T) {
	axes := &MemoryAxes{
		Domain:  []string{" AI ", "ai", "ML"},
		Stack:   []string{"Go", "go"},
		Problem: []string{"  性能  "},
	}
	indexPath := []string{" 项目 ", "", "模块"}
	input := IngestMemoryInput{
		OwnerID:     "personal",
		ProjectKey:  "test-project",
		ProjectName: "test-project",
		ContentType: "development",
		Content:     "test content",
		Axes:        axes,
		IndexPath:   &indexPath,
	}
	settings := defaultSettings()
	normalized, err := normalizeIngestInput(input, settings, time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("normalize 失败: %v", err)
	}
	if normalized.Axes == nil || len(normalized.Axes.Domain) != 2 {
		t.Fatalf("axes 归一化失败: %+v", normalized.Axes)
	}
	if normalized.Axes.Domain[0] != "ai" {
		t.Fatalf("axes 未小写化: %+v", normalized.Axes.Domain)
	}
	if normalized.IndexPath == nil || len(*normalized.IndexPath) != 2 || (*normalized.IndexPath)[0] != "项目" || (*normalized.IndexPath)[1] != "模块" {
		t.Fatalf("index_path 归一化失败: %+v", normalized.IndexPath)
	}
}

func TestValidateAxesTooMany(t *testing.T) {
	values := make([]string, maxAxisValues+1)
	for i := range values {
		values[i] = fmt.Sprintf("v%d", i)
	}
	input := SearchInput{
		OwnerID:     "personal",
		ProjectKey:  "p",
		ProjectName: "p",
		Query:       "test query",
		Scope:       "all",
		Axes:        &MemoryAxes{Domain: values},
	}
	if err := validateSearchInput(input); err == nil {
		t.Fatalf("预期 axes 过多应报错")
	}
}

func TestValidateIndexPathTooDeep(t *testing.T) {
	path := make([]string, maxIndexPathDepth+1)
	for i := range path {
		path[i] = fmt.Sprintf("node-%d", i)
	}
	input := SearchInput{
		OwnerID:     "personal",
		ProjectKey:  "p",
		ProjectName: "p",
		Query:       "test query",
		Scope:       "all",
		IndexPath:   &path,
	}
	if err := validateSearchInput(input); err == nil {
		t.Fatalf("预期 index_path 过深应报错")
	}
}
