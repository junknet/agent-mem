package main

import (
	"strings"
	"time"
)

const (
	defaultSearchLimit       = 20
	defaultTimelineDays      = 7
	defaultTimelineLimit     = 20
	defaultListProjectsLimit = 50
	defaultIndexLimit        = 20
)

func normalizeIngestInput(input IngestMemoryInput, settings Settings, now time.Time) (IngestMemoryInput, error) {
	ownerID, err := resolveOwnerID(input.OwnerID, settings)
	if err != nil {
		return input, err
	}
	input.OwnerID = ownerID

	projectKey, projectName, err := resolveProjectIdentity(input.ProjectName, input.ProjectKey, input.ProjectPath)
	if err != nil {
		return input, err
	}
	input.ProjectKey = projectKey
	input.ProjectName = projectName
	input.MachineName = strings.TrimSpace(input.MachineName)
	input.ProjectPath = strings.TrimSpace(input.ProjectPath)
	input.Summary = strings.TrimSpace(input.Summary)
	if input.Tags != nil && len(*input.Tags) > 0 {
		normalized := normalizeTags(*input.Tags)
		input.Tags = &normalized
	}
	input.Axes = normalizeAxesInput(input.Axes)
	input.IndexPath = normalizeIndexPathPtr(input.IndexPath)

	if input.Ts <= 0 {
		input.Ts = now.Unix()
	}
	return input, nil
}

func normalizeSearchInput(input SearchInput, settings Settings) (SearchInput, error) {
	ownerID, err := resolveOwnerID(input.OwnerID, settings)
	if err != nil {
		return input, err
	}
	input.OwnerID = ownerID
	input.MachineName = strings.TrimSpace(input.MachineName)
	input.ProjectPath = strings.TrimSpace(input.ProjectPath)

	if hasProjectSelector(input.ProjectName, input.ProjectKey, input.ProjectPath) {
		projectKey, projectName, err := resolveProjectIdentity(input.ProjectName, input.ProjectKey, input.ProjectPath)
		if err != nil {
			return input, err
		}
		input.ProjectKey = projectKey
		input.ProjectName = projectName
	}

	input.Scope = strings.TrimSpace(input.Scope)
	if input.Scope == "" {
		input.Scope = "all"
	}
	input.Profile = normalizeSearchProfilePtr(input.Profile)
	input.Mode = normalizeSearchModePtr(input.Mode)
	input.Axes = normalizeAxesInput(input.Axes)
	input.IndexPath = normalizeIndexPathPtr(input.IndexPath)
	if input.Limit <= 0 {
		input.Limit = defaultSearchLimit
	}
	return input, nil
}

func normalizeTimelineInput(input TimelineInput, settings Settings) (TimelineInput, error) {
	ownerID, err := resolveOwnerID(input.OwnerID, settings)
	if err != nil {
		return input, err
	}
	input.OwnerID = ownerID
	input.MachineName = strings.TrimSpace(input.MachineName)
	input.ProjectPath = strings.TrimSpace(input.ProjectPath)

	if hasProjectSelector(input.ProjectName, input.ProjectKey, input.ProjectPath) {
		projectKey, projectName, err := resolveProjectIdentity(input.ProjectName, input.ProjectKey, input.ProjectPath)
		if err != nil {
			return input, err
		}
		input.ProjectKey = projectKey
		input.ProjectName = projectName
	}

	if input.Days <= 0 {
		input.Days = defaultTimelineDays
	}
	if input.Limit <= 0 {
		input.Limit = defaultTimelineLimit
	}
	return input, nil
}

func normalizeListProjectsInput(input ListProjectsInput, settings Settings) (ListProjectsInput, error) {
	ownerID, err := resolveOwnerID(input.OwnerID, settings)
	if err != nil {
		return input, err
	}
	input.OwnerID = ownerID
	if input.Limit <= 0 {
		input.Limit = defaultListProjectsLimit
	}
	return input, nil
}

func normalizeIndexInput(input IndexInput, settings Settings) (IndexInput, error) {
	ownerID, err := resolveOwnerID(input.OwnerID, settings)
	if err != nil {
		return input, err
	}
	input.OwnerID = ownerID
	input.MachineName = strings.TrimSpace(input.MachineName)
	input.ProjectPath = strings.TrimSpace(input.ProjectPath)

	if hasProjectSelector(input.ProjectName, input.ProjectKey, input.ProjectPath) {
		projectKey, projectName, err := resolveProjectIdentity(input.ProjectName, input.ProjectKey, input.ProjectPath)
		if err != nil {
			return input, err
		}
		input.ProjectKey = projectKey
		input.ProjectName = projectName
	}
	input.IndexPath = normalizeIndexPathPtr(input.IndexPath)

	if input.Limit <= 0 {
		input.Limit = defaultIndexLimit
	}
	if input.PathTreeDepth < 0 {
		input.PathTreeDepth = 0
	}
	if input.PathTreeWidth < 0 {
		input.PathTreeWidth = 0
	}
	return input, nil
}

func normalizeSearchProfile(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "deep":
		return "deep"
	case "fast":
		return "fast"
	case "balanced":
		return "balanced"
	default:
		return "deep"
	}
}

func normalizeSearchMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "compact":
		return "compact"
	case "ids":
		return "ids"
	case "full":
		return "full"
	default:
		return "compact"
	}
}

func resolveOwnerID(inputOwner string, settings Settings) (string, error) {
	owner := strings.TrimSpace(inputOwner)
	configured := strings.TrimSpace(settings.Project.OwnerID)
	if owner != "" {
		if configured != "" && owner != configured {
			return "", newValidationError("invalid_request", "ERR_OWNER_MISMATCH", "owner_id 与服务端配置不一致", 400)
		}
		return owner, nil
	}
	if configured != "" {
		return configured, nil
	}
	return defaultOwnerID, nil
}

func resolveProjectIdentity(projectName, projectKey, projectPath string) (string, string, error) {
	name := strings.TrimSpace(projectName)
	key := strings.TrimSpace(projectKey)
	path := strings.TrimSpace(projectPath)
	keyFromPath := false
	if key == "" {
		switch {
		case name != "":
			key = name
		case path != "":
			key = path
			keyFromPath = true
		}
	}
	if name == "" {
		switch {
		case keyFromPath && path != "":
			name = baseName(path)
		case key != "":
			name = key
		case path != "":
			name = baseName(path)
		}
	}
	if key == "" {
		return "", "", newValidationError("invalid_request", "ERR_INVALID_PROJECT", "project_key 或 project_name 不能为空", 400)
	}
	return key, name, nil
}

func hasProjectSelector(projectName, projectKey, projectPath string) bool {
	return strings.TrimSpace(projectName) != "" || strings.TrimSpace(projectKey) != "" || strings.TrimSpace(projectPath) != ""
}

// 指针类型辅助函数（支持 null 值）

func normalizeSearchProfilePtr(value *string) *string {
	var v string
	if value != nil {
		v = *value
	}
	result := normalizeSearchProfile(v)
	return &result
}

func normalizeSearchModePtr(value *string) *string {
	var v string
	if value != nil {
		v = *value
	}
	result := normalizeSearchMode(v)
	return &result
}

func normalizeIndexPathPtr(value *[]string) *[]string {
	if value == nil {
		return nil
	}
	result := normalizeIndexPath(*value)
	return &result
}
