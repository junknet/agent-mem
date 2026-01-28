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
