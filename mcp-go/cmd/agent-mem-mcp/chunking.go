package main

import (
	"strings"
)

func chunkContent(content string, cfg ChunkingConfig) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return []string{}
	}

	chunkSize := cfg.ChunkSize
	overlap := cfg.Overlap
	charsPerToken := cfg.ApproxCharsPerToken
	if chunkSize <= 0 {
		chunkSize = 500
	}
	if overlap < 0 {
		overlap = 0
	}
	if charsPerToken <= 0 {
		charsPerToken = 4
	}

	maxChars := chunkSize * charsPerToken
	overlapChars := overlap * charsPerToken
	if maxChars <= 0 {
		maxChars = 2000
	}
	if overlapChars >= maxChars {
		overlapChars = maxChars / 5
	}

	runes := []rune(trimmed)
	if len(runes) <= maxChars {
		return []string{string(runes)}
	}

	step := maxChars - overlapChars
	if step <= 0 {
		step = maxChars
	}

	var chunks []string
	for start := 0; start < len(runes); start += step {
		end := start + maxChars
		if end > len(runes) {
			end = len(runes)
		}
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end == len(runes) {
			break
		}
	}
	return chunks
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
