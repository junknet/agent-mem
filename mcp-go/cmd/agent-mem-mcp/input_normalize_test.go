package main

import (
	"testing"
	"time"
)

func TestNormalizeIngestInputDefaults(t *testing.T) {
	settings := defaultSettings()
	settings.Project.OwnerID = "personal"
	now := time.Date(2026, 1, 26, 0, 0, 0, 0, time.UTC)
	input := IngestMemoryInput{
		ProjectName: "agent-mem",
		ContentType: "development",
		Content:     "test",
		Ts:          0,
	}
	normalized, err := normalizeIngestInput(input, settings, now)
	if err != nil {
		t.Fatalf("归一化失败: %v", err)
	}
	if normalized.OwnerID != "personal" {
		t.Fatalf("owner_id 默认值错误: %s", normalized.OwnerID)
	}
	if normalized.ProjectKey != "agent-mem" {
		t.Fatalf("project_key 默认值错误: %s", normalized.ProjectKey)
	}
	if normalized.ProjectName != "agent-mem" {
		t.Fatalf("project_name 默认值错误: %s", normalized.ProjectName)
	}
	if normalized.Ts != now.Unix() {
		t.Fatalf("ts 默认值错误: %d", normalized.Ts)
	}
}

func TestNormalizeIngestInputOwnerMismatch(t *testing.T) {
	settings := defaultSettings()
	settings.Project.OwnerID = "personal"
	input := IngestMemoryInput{
		OwnerID:     "other",
		ProjectName: "agent-mem",
		ContentType: "development",
		Content:     "test",
		Ts:          1,
	}
	if _, err := normalizeIngestInput(input, settings, time.Date(2026, 1, 26, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatalf("期望 owner_id 不一致时报错")
	}
}

func TestNormalizeIngestInputTimestampUnits(t *testing.T) {
	settings := defaultSettings()
	now := time.Date(2026, 1, 26, 0, 0, 0, 0, time.UTC)
	base := IngestMemoryInput{
		ProjectName: "agent-mem",
		ContentType: "development",
		Content:     "test",
	}
	tests := []struct {
		name string
		ts   int64
		want int64
	}{
		{name: "seconds", ts: 1769385600, want: 1769385600},
		{name: "milliseconds", ts: 1769385600123, want: 1769385600},
		{name: "microseconds", ts: 1769385600123456, want: 1769385600},
		{name: "nanoseconds", ts: 1769385600123456789, want: 1769385600},
	}
	for _, tt := range tests {
		input := base
		input.Ts = tt.ts
		normalized, err := normalizeIngestInput(input, settings, now)
		if err != nil {
			t.Fatalf("%s 归一化失败: %v", tt.name, err)
		}
		if normalized.Ts != tt.want {
			t.Fatalf("%s 归一化错误: got=%d want=%d", tt.name, normalized.Ts, tt.want)
		}
	}
}

func TestValidateTimestampAllowsClockSkew(t *testing.T) {
	ts := time.Now().UTC().Add(5 * time.Minute).Unix()
	if err := validateTimestamp(ts); err != nil {
		t.Fatalf("期望 10 分钟内未来时间通过校验: %v", err)
	}

	ts = time.Now().UTC().Add(11 * time.Minute).Unix()
	if err := validateTimestamp(ts); err == nil {
		t.Fatalf("期望超过 10 分钟未来时间报错")
	}
}

func TestNormalizeSearchInputDefaults(t *testing.T) {
	settings := defaultSettings()
	settings.Project.OwnerID = "personal"
	input := SearchInput{Query: "测试"}
	normalized, err := normalizeSearchInput(input, settings)
	if err != nil {
		t.Fatalf("归一化失败: %v", err)
	}
	if normalized.OwnerID != "personal" {
		t.Fatalf("owner_id 默认值错误: %s", normalized.OwnerID)
	}
	if normalized.Scope != "all" {
		t.Fatalf("scope 默认值错误: %s", normalized.Scope)
	}
	if normalized.Limit != defaultSearchLimit {
		t.Fatalf("limit 默认值错误: %d", normalized.Limit)
	}
	if normalized.Profile == nil || *normalized.Profile != "deep" {
		t.Fatalf("profile 默认值错误: %v", normalized.Profile)
	}
	if normalized.Mode == nil || *normalized.Mode != "compact" {
		t.Fatalf("mode 默认值错误: %v", normalized.Mode)
	}
}

func TestResolveProjectIdentityFromPath(t *testing.T) {
	key, name, err := resolveProjectIdentity("", "", "/path/to/agent-mem")
	if err != nil {
		t.Fatalf("解析项目失败: %v", err)
	}
	if key != "/path/to/agent-mem" {
		t.Fatalf("project_key 解析错误: %s", key)
	}
	if name != "agent-mem" {
		t.Fatalf("project_name 解析错误: %s", name)
	}
}

func TestNormalizeIndexInputIndexPath(t *testing.T) {
	settings := defaultSettings()
	indexPath := []string{" Root ", "Sub"}
	input := IndexInput{
		OwnerID:   "personal",
		Limit:     10,
		IndexPath: &indexPath,
	}
	normalized, err := normalizeIndexInput(input, settings)
	if err != nil {
		t.Fatalf("归一化失败: %v", err)
	}
	if normalized.IndexPath == nil || len(*normalized.IndexPath) != 2 || (*normalized.IndexPath)[0] != "root" || (*normalized.IndexPath)[1] != "sub" {
		t.Fatalf("index_path 归一化失败: %+v", normalized.IndexPath)
	}
}

func TestValidateIndexInputPathTreeLimits(t *testing.T) {
	input := IndexInput{OwnerID: "personal", Limit: 10, PathTreeDepth: maxIndexPathDepth + 1}
	if err := validateIndexInput(input); err == nil {
		t.Fatalf("期望 path_tree_depth 过大时报错")
	}
	input = IndexInput{OwnerID: "personal", Limit: 10, PathTreeWidth: 101}
	if err := validateIndexInput(input); err == nil {
		t.Fatalf("期望 path_tree_width 过大时报错")
	}
}
