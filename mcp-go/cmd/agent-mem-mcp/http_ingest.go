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
	mux.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		handleListProjects(w, r, app)
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
	if err := rejectUnknownQuery(r, map[string]bool{"owner_id": true, "project_key": true, "project_name": true, "machine_name": true, "project_path": true, "query": true, "scope": true, "limit": true}); err != nil {
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
	if err := rejectUnknownQuery(r, map[string]bool{"ids": true}); err != nil {
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
