package main

import (
	"encoding/json"
	"strings"
	"time"
)

func decodeJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseJSON(raw string) map[string]any {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "json"))
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(cleaned), &data); err == nil {
		return data
	}
	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(cleaned[start:end+1]), &data); err == nil {
			return data
		}
	}
	return nil
}

func parseJSONArray(raw string) []map[string]any {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.Trim(cleaned, "`")
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "json"))
	}
	var data []map[string]any
	if err := json.Unmarshal([]byte(cleaned), &data); err == nil {
		return data
	}
	start := strings.Index(cleaned, "[")
	end := strings.LastIndex(cleaned, "]")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(cleaned[start:end+1]), &data); err == nil {
			return data
		}
	}
	return nil
}

func getString(data map[string]any, key string) string {
	if value, ok := data[key]; ok {
		switch v := value.(type) {
		case string:
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func getBool(data map[string]any, key string) bool {
	if value, ok := data[key]; ok {
		switch v := value.(type) {
		case bool:
			return v
		}
	}
	return false
}

func getStringSlice(data map[string]any, key string) []string {
	var result []string
	value, ok := data[key]
	if !ok {
		return result
	}
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, strings.TrimSpace(s))
			}
		}
	case []string:
		for _, s := range v {
			result = append(result, strings.TrimSpace(s))
		}
	}
	return result
}
