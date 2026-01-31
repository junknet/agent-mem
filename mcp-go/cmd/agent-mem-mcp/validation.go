package main

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type AppError struct {
	Status   int
	ErrorKey string
	Code     string
	Message  string
}

func (e *AppError) Error() string {
	return e.Message
}

var contentTypeSet = map[string]bool{
	"requirement": true, // 需求功能：PRD、功能描述、业务规则
	"plan":        true, // 计划任务：任务清单、里程碑、TODO、执行步骤
	"development": true, // 开发：架构设计、API定义、实现方案
	"testing":     true, // 测试验收：测试计划、用例、验收报告
	"insight":     true, // 经验沉淀：踩坑记录、最佳实践、注意事项
}

func validateIngestInput(input IngestMemoryInput) error {
	if err := validateOwnerID(input.OwnerID); err != nil {
		return err
	}
	if err := validateProjectKey(input.ProjectKey); err != nil {
		return err
	}
	if err := validateProjectName(input.ProjectName); err != nil {
		return err
	}
	if err := validateMachineNameOptional(input.MachineName); err != nil {
		return err
	}
	if err := validateProjectPathOptional(input.ProjectPath); err != nil {
		return err
	}
	if !contentTypeSet[input.ContentType] {
		return newValidationError("invalid_request", "ERR_INVALID_CONTENT_TYPE", "content_type 无效", 400)
	}
	if strings.TrimSpace(input.Content) == "" {
		return newValidationError("invalid_request", "ERR_INVALID_CONTENT", "content 不能为空", 400)
	}
	if strings.ContainsRune(input.Content, '\u0000') {
		return newValidationError("invalid_request", "ERR_INVALID_CONTENT", "content 包含空字节", 400)
	}
	if !utf8.ValidString(input.Content) {
		return newValidationError("invalid_request", "ERR_INVALID_CONTENT", "content 不是有效 UTF-8", 400)
	}
	if len([]byte(input.Content)) > 1024*1024 {
		return newValidationError("invalid_request", "ERR_INVALID_CONTENT", "content 超过 1MB 限制", 400)
	}
	if err := validateTimestamp(input.Ts); err != nil {
		return err
	}
	if err := validateSummary(input.Summary); err != nil {
		return err
	}
	if err := validateTagsPtr(input.Tags); err != nil {
		return err
	}
	if err := validateAxes(input.Axes); err != nil {
		return err
	}
	if err := validateIndexPathPtr(input.IndexPath); err != nil {
		return err
	}
	return nil
}

func validateSearchInput(input SearchInput) error {
	if err := validateOwnerID(input.OwnerID); err != nil {
		return err
	}
	if input.ProjectKey != "" || input.ProjectName != "" {
		if err := validateProjectKey(input.ProjectKey); err != nil {
			return err
		}
		if err := validateProjectName(input.ProjectName); err != nil {
			return err
		}
	}
	if err := validateMachineNameOptional(input.MachineName); err != nil {
		return err
	}
	if err := validateProjectPathOptional(input.ProjectPath); err != nil {
		return err
	}
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return newValidationError("invalid_request", "ERR_INVALID_QUERY", "query 不能为空", 400)
	}
	if !hasMeaningfulContent(query) {
		return newValidationError("invalid_request", "ERR_INVALID_QUERY", "query 无有效内容", 400)
	}
	if len([]rune(query)) < 2 {
		return newValidationError("invalid_request", "ERR_INVALID_QUERY", "query 太短", 400)
	}
	if len([]rune(query)) > 1000 {
		return newValidationError("invalid_request", "ERR_INVALID_QUERY", "query 过长", 400)
	}
	if input.Scope == "" {
		return newValidationError("invalid_request", "ERR_INVALID_SCOPE", "scope 必填", 400)
	}
	if input.Scope != "all" && !contentTypeSet[input.Scope] {
		return newValidationError("invalid_request", "ERR_INVALID_SCOPE", "scope 无效", 400)
	}
	if err := validateSearchProfilePtr(input.Profile); err != nil {
		return err
	}
	if err := validateSearchModePtr(input.Mode); err != nil {
		return err
	}
	if input.Limit < 1 || input.Limit > 100 {
		return newValidationError("invalid_request", "ERR_INVALID_LIMIT", "limit 必须在 1-100 之间", 400)
	}
	if err := validateAxes(input.Axes); err != nil {
		return err
	}
	if err := validateIndexPathPtr(input.IndexPath); err != nil {
		return err
	}
	return nil
}

func validateTimelineInput(input TimelineInput) error {
	if err := validateOwnerID(input.OwnerID); err != nil {
		return err
	}
	if input.ProjectKey != "" || input.ProjectName != "" {
		if err := validateProjectKey(input.ProjectKey); err != nil {
			return err
		}
		if err := validateProjectName(input.ProjectName); err != nil {
			return err
		}
	}
	if err := validateMachineNameOptional(input.MachineName); err != nil {
		return err
	}
	if err := validateProjectPathOptional(input.ProjectPath); err != nil {
		return err
	}
	if input.Days < 1 {
		return newValidationError("invalid_request", "ERR_INVALID_DAYS", "days 必须 >= 1", 400)
	}
	if input.Limit < 1 || input.Limit > 100 {
		return newValidationError("invalid_request", "ERR_INVALID_LIMIT", "limit 必须在 1-100 之间", 400)
	}
	return nil
}

func validateListProjectsInput(input ListProjectsInput) error {
	if err := validateOwnerID(input.OwnerID); err != nil {
		return err
	}
	if input.Limit < 1 || input.Limit > 1000 {
		return newValidationError("invalid_request", "ERR_INVALID_LIMIT", "limit 必须在 1-1000 之间", 400)
	}
	return nil
}

func validateIndexInput(input IndexInput) error {
	if err := validateOwnerID(input.OwnerID); err != nil {
		return err
	}
	if input.ProjectKey != "" || input.ProjectName != "" {
		if err := validateProjectKey(input.ProjectKey); err != nil {
			return err
		}
		if err := validateProjectName(input.ProjectName); err != nil {
			return err
		}
	}
	if err := validateMachineNameOptional(input.MachineName); err != nil {
		return err
	}
	if err := validateProjectPathOptional(input.ProjectPath); err != nil {
		return err
	}
	if err := validateIndexPathPtr(input.IndexPath); err != nil {
		return err
	}
	if input.Limit < 1 || input.Limit > 200 {
		return newValidationError("invalid_request", "ERR_INVALID_LIMIT", "limit 必须在 1-200 之间", 400)
	}
	if input.PathTreeDepth < 0 || input.PathTreeDepth > maxIndexPathDepth {
		return newValidationError("invalid_request", "ERR_INVALID_PATH_TREE_DEPTH", "path_tree_depth 必须在 0-10 之间", 400)
	}
	if input.PathTreeWidth < 0 || input.PathTreeWidth > 100 {
		return newValidationError("invalid_request", "ERR_INVALID_PATH_TREE_WIDTH", "path_tree_width 必须在 0-100 之间", 400)
	}
	return nil
}

func validateOwnerID(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return newValidationError("invalid_request", "ERR_INVALID_OWNER", "owner_id 不能为空", 400)
	}
	if len([]rune(trimmed)) > 255 {
		return newValidationError("invalid_request", "ERR_INVALID_OWNER", "owner_id 过长", 400)
	}
	if containsControl(trimmed) {
		return newValidationError("invalid_request", "ERR_INVALID_OWNER", "owner_id 包含非法字符", 400)
	}
	return nil
}

func validateProjectKey(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT", "project_key 不能为空", 400)
	}
	if len([]rune(trimmed)) > 1024 {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT", "project_key 过长", 400)
	}
	if strings.ContainsRune(trimmed, '\u0000') {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT", "project_key 包含空字节", 400)
	}
	if containsControl(trimmed) {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT", "project_key 包含非法字符", 400)
	}
	return nil
}

func validateProjectName(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT_NAME", "project_name 不能为空", 400)
	}
	if len([]rune(trimmed)) > 255 {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT_NAME", "project_name 过长", 400)
	}
	if strings.ContainsRune(trimmed, '\u0000') {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT_NAME", "project_name 包含空字节", 400)
	}
	if containsControl(trimmed) {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT_NAME", "project_name 包含非法字符", 400)
	}
	return nil
}

func validateMachineName(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return newValidationError("invalid_request", "ERR_INVALID_MACHINE", "machine_name 不能为空", 400)
	}
	if len([]rune(trimmed)) > 255 {
		return newValidationError("invalid_request", "ERR_INVALID_MACHINE", "machine_name 过长", 400)
	}
	if containsControl(trimmed) {
		return newValidationError("invalid_request", "ERR_INVALID_MACHINE", "machine_name 包含非法字符", 400)
	}
	return nil
}

func validateMachineNameOptional(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return validateMachineName(value)
}

func validateProjectPath(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT_PATH", "project_path 不能为空", 400)
	}
	if len([]rune(trimmed)) > 1024 {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT_PATH", "project_path 过长", 400)
	}
	if strings.ContainsRune(trimmed, '\u0000') {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT_PATH", "project_path 包含空字节", 400)
	}
	if !isAbsolutePath(trimmed) {
		return newValidationError("invalid_request", "ERR_INVALID_PROJECT_PATH", "project_path 必须是绝对路径", 400)
	}
	return nil
}

func validateProjectPathOptional(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return validateProjectPath(value)
}

func validateTimestamp(ts int64) error {
	if ts <= 0 {
		return newValidationError("invalid_request", "ERR_INVALID_TS", "ts 必须为正整数", 400)
	}
	maxFuture := time.Now().UTC().Add(10 * time.Second).Unix()
	if ts > maxFuture {
		return newValidationError("invalid_request", "ERR_INVALID_TS", "ts 不能超过当前时间", 400)
	}
	if ts > 9_000_000_000_000 {
		return newValidationError("invalid_request", "ERR_INVALID_TS", "ts 超出有效范围", 400)
	}
	return nil
}

func validateSummary(summary string) error {
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	if len([]rune(summary)) > 5000 {
		return newValidationError("invalid_request", "ERR_INVALID_SUMMARY", "summary 过长", 400)
	}
	if containsControl(summary) {
		return newValidationError("invalid_request", "ERR_INVALID_SUMMARY", "summary 包含非法字符", 400)
	}
	return nil
}

func validateTags(tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	if len(tags) > 50 {
		return newValidationError("invalid_request", "ERR_INVALID_TAGS", "tags 数量过多", 400)
	}
	for _, tag := range tags {
		item := strings.TrimSpace(tag)
		if item == "" {
			continue
		}
		if len([]rune(item)) > 100 {
			return newValidationError("invalid_request", "ERR_INVALID_TAGS", "tag 过长", 400)
		}
		if containsControl(item) {
			return newValidationError("invalid_request", "ERR_INVALID_TAGS", "tag 包含非法字符", 400)
		}
	}
	return nil
}

func validateSearchProfile(profile string) error {
	switch profile {
	case "", "balanced", "fast", "deep":
		return nil
	default:
		return newValidationError("invalid_request", "ERR_INVALID_PROFILE", "profile 无效", 400)
	}
}

func validateSearchMode(mode string) error {
	switch mode {
	case "", "full", "ids", "compact":
		return nil
	default:
		return newValidationError("invalid_request", "ERR_INVALID_MODE", "mode 无效", 400)
	}
}

func validateAxes(axes *MemoryAxes) error {
	if axes == nil {
		return nil
	}
	if err := validateAxisValues("domain", axes.Domain); err != nil {
		return err
	}
	if err := validateAxisValues("stack", axes.Stack); err != nil {
		return err
	}
	if err := validateAxisValues("problem", axes.Problem); err != nil {
		return err
	}
	if err := validateAxisValues("lifecycle", axes.Lifecycle); err != nil {
		return err
	}
	if err := validateAxisValues("component", axes.Component); err != nil {
		return err
	}
	return nil
}

func validateAxisValues(axis string, values []string) error {
	if len(values) == 0 {
		return nil
	}
	if len(values) > maxAxisValues {
		return newValidationError("invalid_request", "ERR_INVALID_AXES", axis+" 维度数量过多", 400)
	}
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		if len([]rune(item)) > maxAxisValueLen {
			return newValidationError("invalid_request", "ERR_INVALID_AXES", axis+" 维度过长", 400)
		}
		if containsControl(item) {
			return newValidationError("invalid_request", "ERR_INVALID_AXES", axis+" 维度包含非法字符", 400)
		}
	}
	return nil
}

func validateIndexPath(path []string) error {
	if len(path) == 0 {
		return nil
	}
	if len(path) > maxIndexPathDepth {
		return newValidationError("invalid_request", "ERR_INVALID_INDEX_PATH", "index_path 过深", 400)
	}
	for _, value := range path {
		item := strings.TrimSpace(value)
		if item == "" {
			continue
		}
		if len([]rune(item)) > maxIndexPathSegmentLen {
			return newValidationError("invalid_request", "ERR_INVALID_INDEX_PATH", "index_path 节点过长", 400)
		}
		if containsControl(item) {
			return newValidationError("invalid_request", "ERR_INVALID_INDEX_PATH", "index_path 包含非法字符", 400)
		}
	}
	return nil
}

func newValidationError(errKey, code, message string, status int) *AppError {
	return &AppError{Status: status, ErrorKey: errKey, Code: code, Message: message}
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func hasMeaningfulContent(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

func isAbsolutePath(path string) bool {
	if strings.HasPrefix(path, "/") {
		return true
	}
	if strings.HasPrefix(path, "\\\\") {
		return true
	}
	if len(path) >= 3 {
		r0 := rune(path[0])
		if unicode.IsLetter(r0) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
			return true
		}
	}
	return false
}

// 指针类型验证函数（支持 null 值）

func validateSearchProfilePtr(profile *string) error {
	if profile == nil {
		return nil
	}
	return validateSearchProfile(*profile)
}

func validateSearchModePtr(mode *string) error {
	if mode == nil {
		return nil
	}
	return validateSearchMode(*mode)
}

func validateIndexPathPtr(path *[]string) error {
	if path == nil {
		return nil
	}
	return validateIndexPath(*path)
}

func validateTagsPtr(tags *[]string) error {
	if tags == nil {
		return nil
	}
	return validateTags(*tags)
}
