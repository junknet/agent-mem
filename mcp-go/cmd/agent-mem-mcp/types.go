package main

import "time"

type IngestMemoryInput struct {
	OwnerID     string `json:"owner_id"`
	ProjectKey  string `json:"project_key"`
	ProjectName string `json:"project_name"`
	MachineName string `json:"machine_name"`
	ProjectPath string `json:"project_path"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
	Ts          int64  `json:"ts"`
}

type IngestMemoryOutput struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Ts     int64  `json:"ts"`
}

type SearchInput struct {
	OwnerID     string `json:"owner_id"`
	ProjectKey  string `json:"project_key"`
	ProjectName string `json:"project_name"`
	MachineName string `json:"machine_name"`
	ProjectPath string `json:"project_path"`
	Query       string `json:"query"`
	Scope       string `json:"scope"`
	Limit       int    `json:"limit"`
}

type SearchResult struct {
	ID          string  `json:"id"`
	Snippet     string  `json:"snippet"`
	ContentType string  `json:"content_type"`
	Score       float64 `json:"score"`
	Ts          int64   `json:"ts"`
	ChunkIndex  int     `json:"chunk_index"`
	TotalChunks int     `json:"total_chunks"`
}

type SearchMetadata struct {
	Total      int    `json:"total"`
	Returned   int    `json:"returned"`
	NextAction string `json:"next_action"`
}

type SearchResponse struct {
	Results  []SearchResult `json:"results"`
	Metadata SearchMetadata `json:"metadata"`
}

type GetMemoriesInput struct {
	IDs []string `json:"ids"`
}

type MemoryRecord struct {
	ID          string   `json:"id"`
	Content     string   `json:"content"`
	ContentType string   `json:"content_type"`
	Summary     string   `json:"summary"`
	Tags        []string `json:"tags"`
	Ts          int64    `json:"ts"`
}

type GetMemoriesResponse struct {
	Results []MemoryRecord `json:"results"`
}

type TimelineInput struct {
	OwnerID     string `json:"owner_id"`
	ProjectKey  string `json:"project_key"`
	ProjectName string `json:"project_name"`
	MachineName string `json:"machine_name"`
	ProjectPath string `json:"project_path"`
	Days        int    `json:"days"`
	Limit       int    `json:"limit"`
}

type TimelineItem struct {
	ID          string `json:"id"`
	ContentType string `json:"content_type"`
	Summary     string `json:"summary"`
	Ts          int64  `json:"ts"`
}

type TimelineResponse struct {
	Results  []TimelineItem `json:"results"`
	Metadata SearchMetadata `json:"metadata"`
}

type ListProjectsInput struct {
	OwnerID string `json:"owner_id"`
	Limit   int    `json:"limit"`
}

type ProjectListItem struct {
	OwnerID     string `json:"owner_id"`
	ProjectKey  string `json:"project_key"`
	MachineName string `json:"machine_name"`
	ProjectPath string `json:"project_path"`
	ProjectName string `json:"project_name"`
	MemoryCount int    `json:"memory_count"`
	LatestTs    int64  `json:"latest_ts"`
}

type ListProjectsResponse struct {
	Results  []ProjectListItem `json:"results"`
	Metadata SearchMetadata    `json:"metadata"`
}

type MemoryInsert struct {
	ID           string
	ProjectID    string
	ContentType  string
	Content      string
	ContentHash  string
	Ts           int64
	Summary      string
	Tags         []string
	ChunkCount   int
	Embedded     bool
	AvgEmbedding []float32
	CreatedAt    time.Time
}

type MemorySnapshot struct {
	ID           string
	ProjectID    string
	ContentType  string
	Content      string
	ContentHash  string
	Ts           int64
	Summary      string
	Tags         []string
	ChunkCount   int
	AvgEmbedding []float32
	CreatedAt    time.Time
}

type MemoryVersionInsert struct {
	MemoryID     string
	ProjectID    string
	ContentType  string
	Content      string
	ContentHash  string
	Ts           int64
	Summary      string
	Tags         []string
	ChunkCount   int
	AvgEmbedding []float32
	CreatedAt    time.Time
	ReplacedAt   time.Time
}

type ArbitrationLogInsert struct {
	OwnerID           string
	ProjectID         string
	CandidateMemoryID string
	NewMemoryID       string
	Action            string
	Similarity        float64
	OldSummary        string
	NewSummary        string
	Model             string
	CreatedAt         time.Time
}

type FragmentInsert struct {
	ID         string
	MemoryID   string
	ChunkIndex int
	Content    string
	Embedding  []float32
}

type FragmentRow struct {
	FragmentID  string
	MemoryID    string
	ChunkIndex  int
	Content     string
	ContentType string
	Ts          int64
	ChunkCount  int
	Distance    float64
	RankScore   float64
}
