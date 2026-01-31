package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func registerHTTPRoutes(mux *http.ServeMux, app *App) {
	mux.HandleFunc("/ingest/memory", func(w http.ResponseWriter, r *http.Request) {
		handleIngestMemory(w, r, app)
	})
	mux.HandleFunc("/memories/search", func(w http.ResponseWriter, r *http.Request) {
		handleSearchMemories(w, r, app)
	})
	mux.HandleFunc("/memories", func(w http.ResponseWriter, r *http.Request) {
		handleGetMemories(w, r, app)
	})
	mux.HandleFunc("/memories/timeline", func(w http.ResponseWriter, r *http.Request) {
		handleTimeline(w, r, app)
	})
	mux.HandleFunc("/memories/index", func(w http.ResponseWriter, r *http.Request) {
		handleIndex(w, r, app)
	})
	mux.HandleFunc("/memories/metrics", func(w http.ResponseWriter, r *http.Request) {
		handleMetrics(w, r, app)
	})
	mux.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		handleListProjects(w, r, app)
	})
	mux.HandleFunc("/arbitrations", func(w http.ResponseWriter, r *http.Request) {
		handleArbitrationHistory(w, r, app)
	})
	mux.HandleFunc("/memories/chain", func(w http.ResponseWriter, r *http.Request) {
		handleMemoryChain(w, r, app)
	})
	mux.HandleFunc("/memories/rollback", func(w http.ResponseWriter, r *http.Request) {
		handleRollback(w, r, app)
	})
}

func handleIngestMemory(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 POST", "ERR_METHOD")
		return
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var payload IngestMemoryInput
	if err := decoder.Decode(&payload); err != nil {
		unknown := parseUnknownField(err)
		if unknown != "" {
			writeError(w, http.StatusBadRequest, "invalid_field", fmt.Sprintf("unknown field: %s", unknown), "ERR_INVALID_FIELD")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "请求体解析失败", "ERR_INVALID_BODY")
		return
	}
	normalized, err := normalizeIngestInput(payload, app.settings, time.Now().UTC())
	if err != nil {
		writeValidationError(w, err)
		return
	}
	if err := validateIngestInput(normalized); err != nil {
		writeValidationError(w, err)
		return
	}

	result, err := app.IngestMemory(r.Context(), normalized)
	if err != nil {
		log.Printf("❌ ingest 失败: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "服务器错误", "ERR_INTERNAL")
		return
	}
	status := result.Status
	if status == "" {
		status = "created"
	}
	writeJSON(w, http.StatusOK, IngestMemoryOutput{ID: result.ID, Status: status, Ts: normalized.Ts})
}

func handleSearchMemories(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET", "ERR_METHOD")
		return
	}
	if err := rejectUnknownQuery(r, map[string]bool{"owner_id": true, "project_key": true, "project_name": true, "machine_name": true, "project_path": true, "query": true, "scope": true, "profile": true, "mode": true, "axes": true, "index_path": true, "limit": true}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_field", err.Error(), "ERR_INVALID_FIELD")
		return
	}

	payload := SearchInput{
		OwnerID:     strings.TrimSpace(r.URL.Query().Get("owner_id")),
		ProjectKey:  strings.TrimSpace(r.URL.Query().Get("project_key")),
		ProjectName: strings.TrimSpace(r.URL.Query().Get("project_name")),
		MachineName: strings.TrimSpace(r.URL.Query().Get("machine_name")),
		ProjectPath: strings.TrimSpace(r.URL.Query().Get("project_path")),
		Query:       strings.TrimSpace(r.URL.Query().Get("query")),
		Scope:       strings.TrimSpace(r.URL.Query().Get("scope")),
		Profile:     strPtr(strings.TrimSpace(r.URL.Query().Get("profile"))),
		Mode:        strPtr(strings.TrimSpace(r.URL.Query().Get("mode"))),
	}
	axes, err := parseAxesQuery(r.URL.Query().Get("axes"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "axes 参数格式错误", "ERR_INVALID_AXES")
		return
	}
	if axes != nil {
		payload.Axes = axes
	}
	indexPath, err := parseIndexPathQuery(r.URL.Query()["index_path"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "index_path 参数格式错误", "ERR_INVALID_INDEX_PATH")
		return
	}
	if indexPath != nil {
		payload.IndexPath = &indexPath
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_LIMIT")
		return
	}
	payload.Limit = limit

	output, err := app.SearchMemories(r.Context(), payload)
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			writeValidationError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "服务器错误", "ERR_INTERNAL")
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func handleGetMemories(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET", "ERR_METHOD")
		return
	}
	if err := rejectUnknownQuery(r, map[string]bool{"ids": true, "owner_id": true}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_field", err.Error(), "ERR_INVALID_FIELD")
		return
	}

	ids := r.URL.Query()["ids"]
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "ids 不能为空", "ERR_INVALID_IDS")
		return
	}
	cleaned := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "ids 不能为空", "ERR_INVALID_IDS")
			return
		}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) > 10 {
		writeError(w, http.StatusBadRequest, "invalid_request", "ids 不能超过 10 个", "ERR_INVALID_IDS")
		return
	}
	clean := uniqueStrings(cleaned)
	if len(clean) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "ids 不能为空", "ERR_INVALID_IDS")
		return
	}
	output, err := app.GetMemories(r.Context(), clean)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "服务器错误", "ERR_INTERNAL")
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func handleTimeline(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET", "ERR_METHOD")
		return
	}
	if err := rejectUnknownQuery(r, map[string]bool{"owner_id": true, "project_key": true, "project_name": true, "machine_name": true, "project_path": true, "days": true, "limit": true}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_field", err.Error(), "ERR_INVALID_FIELD")
		return
	}

	payload := TimelineInput{
		OwnerID:     strings.TrimSpace(r.URL.Query().Get("owner_id")),
		ProjectKey:  strings.TrimSpace(r.URL.Query().Get("project_key")),
		ProjectName: strings.TrimSpace(r.URL.Query().Get("project_name")),
		MachineName: strings.TrimSpace(r.URL.Query().Get("machine_name")),
		ProjectPath: strings.TrimSpace(r.URL.Query().Get("project_path")),
	}
	days, err := parseOptionalInt(r.URL.Query().Get("days"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_DAYS")
		return
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_LIMIT")
		return
	}
	payload.Days = days
	payload.Limit = limit

	output, err := app.Timeline(r.Context(), payload)
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			writeValidationError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "服务器错误", "ERR_INTERNAL")
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func handleIndex(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET", "ERR_METHOD")
		return
	}
	if err := rejectUnknownQuery(r, map[string]bool{"owner_id": true, "project_key": true, "project_name": true, "machine_name": true, "project_path": true, "index_path": true, "limit": true, "path_tree_depth": true, "path_tree_width": true}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_field", err.Error(), "ERR_INVALID_FIELD")
		return
	}

	payload := IndexInput{
		OwnerID:     strings.TrimSpace(r.URL.Query().Get("owner_id")),
		ProjectKey:  strings.TrimSpace(r.URL.Query().Get("project_key")),
		ProjectName: strings.TrimSpace(r.URL.Query().Get("project_name")),
		MachineName: strings.TrimSpace(r.URL.Query().Get("machine_name")),
		ProjectPath: strings.TrimSpace(r.URL.Query().Get("project_path")),
	}
	indexPathArr, err := parseIndexPathQuery(r.URL.Query()["index_path"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "index_path 参数格式错误", "ERR_INVALID_INDEX_PATH")
		return
	}
	if indexPathArr != nil {
		payload.IndexPath = &indexPathArr
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_LIMIT")
		return
	}
	payload.Limit = limit
	depth, err := parseOptionalInt(r.URL.Query().Get("path_tree_depth"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_PATH_TREE_DEPTH")
		return
	}
	payload.PathTreeDepth = depth
	width, err := parseOptionalInt(r.URL.Query().Get("path_tree_width"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_PATH_TREE_WIDTH")
		return
	}
	payload.PathTreeWidth = width

	output, err := app.Index(r.Context(), payload)
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			writeValidationError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "服务器错误", "ERR_INTERNAL")
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func handleMetrics(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET", "ERR_METHOD")
		return
	}
	if err := rejectUnknownQuery(r, map[string]bool{"owner_id": true, "project_key": true, "project_name": true, "machine_name": true, "project_path": true, "index_path": true, "limit": true, "path_tree_depth": true, "path_tree_width": true}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_field", err.Error(), "ERR_INVALID_FIELD")
		return
	}

	payload := IndexInput{
		OwnerID:     strings.TrimSpace(r.URL.Query().Get("owner_id")),
		ProjectKey:  strings.TrimSpace(r.URL.Query().Get("project_key")),
		ProjectName: strings.TrimSpace(r.URL.Query().Get("project_name")),
		MachineName: strings.TrimSpace(r.URL.Query().Get("machine_name")),
		ProjectPath: strings.TrimSpace(r.URL.Query().Get("project_path")),
	}
	metricsIndexPath, err := parseIndexPathQuery(r.URL.Query()["index_path"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "index_path 参数格式错误", "ERR_INVALID_INDEX_PATH")
		return
	}
	if metricsIndexPath != nil {
		payload.IndexPath = &metricsIndexPath
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_LIMIT")
		return
	}
	payload.Limit = limit
	depth, err := parseOptionalInt(r.URL.Query().Get("path_tree_depth"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_PATH_TREE_DEPTH")
		return
	}
	payload.PathTreeDepth = depth
	width, err := parseOptionalInt(r.URL.Query().Get("path_tree_width"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_PATH_TREE_WIDTH")
		return
	}
	payload.PathTreeWidth = width

	output, err := app.Metrics(r.Context(), payload)
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			writeValidationError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "服务器错误", "ERR_INTERNAL")
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(output.Content))
}

func handleListProjects(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET", "ERR_METHOD")
		return
	}
	if err := rejectUnknownQuery(r, map[string]bool{"owner_id": true, "limit": true}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_field", err.Error(), "ERR_INVALID_FIELD")
		return
	}

	limit, err := parseOptionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_LIMIT")
		return
	}
	payload := ListProjectsInput{
		OwnerID: strings.TrimSpace(r.URL.Query().Get("owner_id")),
		Limit:   limit,
	}
	output, err := app.ListProjects(r.Context(), payload)
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			writeValidationError(w, err)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "服务器错误", "ERR_INTERNAL")
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func writeValidationError(w http.ResponseWriter, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		writeError(w, appErr.Status, appErr.ErrorKey, appErr.Message, appErr.Code)
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "ERR_INVALID_REQUEST")
}

func parseUnknownField(err error) string {
	msg := err.Error()
	if !strings.Contains(msg, "unknown field") {
		return ""
	}
	parts := strings.Split(msg, "\"")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func parseRequiredInt(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, errors.New("参数缺失")
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New("参数格式错误")
	}
	return parsed, nil
}

func parseOptionalInt(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, errors.New("参数格式错误")
	}
	return parsed, nil
}

func parseAxesQuery(raw string) (*MemoryAxes, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var axes MemoryAxes
	if err := json.Unmarshal([]byte(trimmed), &axes); err != nil {
		return nil, err
	}
	normalized := normalizeAxesInput(&axes)
	return normalized, nil
}

func parseIndexPathQuery(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) == 1 {
		trimmed := strings.TrimSpace(values[0])
		if strings.HasPrefix(trimmed, "[") {
			var path []string
			if err := json.Unmarshal([]byte(trimmed), &path); err != nil {
				return nil, err
			}
			return normalizeIndexPath(path), nil
		}
	}
	var path []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			path = append(path, part)
		}
	}
	return normalizeIndexPath(path), nil
}

func rejectUnknownQuery(r *http.Request, allowed map[string]bool) error {
	for key := range r.URL.Query() {
		if !allowed[key] {
			return fmt.Errorf("unknown field: %s", key)
		}
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, errKey, message, code string) {
	payload := ErrorResponse{
		Error:     errKey,
		Message:   message,
		Code:      code,
		Timestamp: time.Now().UTC().Unix(),
	}
	writeJSON(w, status, payload)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func handleArbitrationHistory(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET", "ERR_METHOD")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}

	input := ArbitrationHistoryInput{
		OwnerID:    strings.TrimSpace(r.URL.Query().Get("owner_id")),
		MemoryID:   strings.TrimSpace(r.URL.Query().Get("memory_id")),
		ProjectKey: strings.TrimSpace(r.URL.Query().Get("project_key")),
		Limit:      limit,
	}

	result, err := app.ArbitrationHistory(r.Context(), input)
	if err != nil {
		writeValidationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func handleMemoryChain(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET", "ERR_METHOD")
		return
	}

	input := MemoryChainInput{
		OwnerID:  strings.TrimSpace(r.URL.Query().Get("owner_id")),
		MemoryID: strings.TrimSpace(r.URL.Query().Get("memory_id")),
	}

	result, err := app.MemoryChain(r.Context(), input)
	if err != nil {
		writeValidationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func handleRollback(w http.ResponseWriter, r *http.Request, app *App) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 POST", "ERR_METHOD")
		return
	}

	arbID, _ := strconv.ParseInt(r.URL.Query().Get("arbitration_id"), 10, 64)
	input := RollbackInput{
		OwnerID:       strings.TrimSpace(r.URL.Query().Get("owner_id")),
		ArbitrationID: arbID,
	}

	result, err := app.Rollback(r.Context(), input)
	if err != nil {
		writeValidationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
