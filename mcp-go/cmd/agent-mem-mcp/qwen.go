package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type QwenClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type RerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

func NewQwenClient(settings Settings) *QwenClient {
	apiKeyEnv := settings.LLM.APIKeyEnv
	if apiKeyEnv == "" {
		apiKeyEnv = "DASHSCOPE_API_KEY"
	}
	return &QwenClient{
		baseURL:    settings.LLM.BaseURL,
		apiKey:     envOrDefault(apiKeyEnv, ""),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *QwenClient) ChatCompletion(ctx context.Context, model, prompt string, temperature float64, maxTokens int) (string, error) {
	if c.apiKey == "" {
		return "", errors.New("缺少 DASHSCOPE_API_KEY")
	}
	endpoint := joinURL(c.baseURL, "/chat/completions")
	payload := map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": temperature,
		"max_tokens":  maxTokens,
	}

	body, err := c.postJSON(ctx, endpoint, payload)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("无返回结果")
	}
	return parsed.Choices[0].Message.Content, nil
}

func (c *QwenClient) Embeddings(ctx context.Context, model string, inputs []string) ([][]float32, error) {
	if c.apiKey == "" {
		return nil, errors.New("缺少 DASHSCOPE_API_KEY")
	}
	endpoint := joinURL(c.baseURL, "/embeddings")
	payload := map[string]any{
		"model": model,
		"input": inputs,
	}
	body, err := c.postJSON(ctx, endpoint, payload)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	vectors := make([][]float32, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		vec := make([]float32, len(item.Embedding))
		for i, v := range item.Embedding {
			vec[i] = float32(v)
		}
		vectors = append(vectors, vec)
	}
	return vectors, nil
}

func (c *QwenClient) Rerank(ctx context.Context, model, query string, documents []string, topN int) ([]RerankResult, error) {
	if c.apiKey == "" {
		return nil, errors.New("缺少 DASHSCOPE_API_KEY")
	}
	endpoint := "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"
	payload := map[string]any{
		"model": model,
		"input": map[string]any{
			"query":     query,
			"documents": documents,
		},
		"parameters": map[string]any{
			"return_documents": false,
			"top_n":            topN,
		},
	}
	body, err := c.postJSON(ctx, endpoint, payload)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Output struct {
			Results []RerankResult `json:"results"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	return parsed.Output.Results, nil
}

func (c *QwenClient) postJSON(ctx context.Context, url string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))

	resp, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("请求失败: %s", string(data))
	}
	return data, nil
}

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}
