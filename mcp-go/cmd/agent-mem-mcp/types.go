package main

import "time"

type IngestMemoryInput struct {
	OwnerID     string      `json:"owner_id"`
	ProjectKey  string      `json:"project_key"`
	ProjectName string      `json:"project_name"`
	MachineName string      `json:"machine_name,omitempty"`
	ProjectPath string      `json:"project_path,omitempty"`
	ContentType string      `json:"content_type"`
	Content     string      `json:"content"`
	Summary     string      `json:"summary,omitempty"`
	Tags        *[]string   `json:"tags,omitempty"`
	SkipLLM     bool        `json:"skip_llm,omitempty"`
	Axes        *MemoryAxes `json:"axes,omitempty"`
	IndexPath   *[]string   `json:"index_path,omitempty"`
	Ts          int64       `json:"ts"`
}

type IngestMemoryOutput struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Ts     int64  `json:"ts"`
}

type SearchInput struct {
	OwnerID     string      `json:"owner_id"`
	ProjectKey  string      `json:"project_key"`
	ProjectName string      `json:"project_name"`
	MachineName string      `json:"machine_name,omitempty"`
	ProjectPath string      `json:"project_path,omitempty"`
	Query       string      `json:"query"`
	Scope       string      `json:"scope"`
	Profile     *string     `json:"profile,omitempty"`
	Mode        *string     `json:"mode,omitempty"`
	Axes        *MemoryAxes `json:"axes,omitempty"`
	IndexPath   *[]string   `json:"index_path,omitempty"`
	Limit       int         `json:"limit"`
}

type SearchResult struct {
	ID          string       `json:"id"`
	Snippet     string       `json:"snippet,omitempty"`
	ContentType string       `json:"content_type,omitempty"`
	ProjectKey  string       `json:"project_key,omitempty"`
	Axes        *MemoryAxes  `json:"axes,omitempty"`
	IndexPath   []string     `json:"index_path,omitempty"`
	Trace       *SearchTrace `json:"trace,omitempty"`
	Score       float64      `json:"score,omitempty"`
	Ts          int64        `json:"ts,omitempty"`
	ChunkIndex  int          `json:"chunk_index,omitempty"`
	TotalChunks int          `json:"total_chunks,omitempty"`
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
	IDs     []string `json:"ids"`
	OwnerID string   `json:"owner_id,omitempty"` // 可选，兼容 Codex 传参
}

type MemoryRecord struct {
	ID          string      `json:"id"`
	Content     string      `json:"content"`
	ContentType string      `json:"content_type"`
	Summary     string      `json:"summary"`
	Tags        []string    `json:"tags"`
	Axes        *MemoryAxes `json:"axes,omitempty"`
	IndexPath   []string    `json:"index_path,omitempty"`
	Ts          int64       `json:"ts"`
}

type GetMemoriesResponse struct {
	Results []MemoryRecord `json:"results"`
}

type TimelineInput struct {
	OwnerID     string `json:"owner_id"`
	ProjectKey  string `json:"project_key,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
	MachineName string `json:"machine_name,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
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
	Axes         MemoryAxes
	IndexPath    []string
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
	Axes         MemoryAxes
	IndexPath    []string
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
	Axes         MemoryAxes
	IndexPath    []string
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
	ProjectKey  string
	Ts          int64
	ChunkCount  int
	Axes        MemoryAxes
	IndexPath   []string
	Distance    float64
	RankScore   float64
}

type MemoryAxes struct {
	Domain    []string `json:"domain,omitempty"`
	Stack     []string `json:"stack,omitempty"`
	Problem   []string `json:"problem,omitempty"`
	Lifecycle []string `json:"lifecycle,omitempty"`
	Component []string `json:"component,omitempty"`
}

type SearchTrace struct {
	Sources  []string       `json:"sources,omitempty"`
	Ranks    map[string]int `json:"ranks,omitempty"`
	RRFScore float64        `json:"rrf_score,omitempty"`
}

type IndexInput struct {
	OwnerID       string    `json:"owner_id"`
	ProjectKey    string    `json:"project_key,omitempty"`
	ProjectName   string    `json:"project_name,omitempty"`
	MachineName   string    `json:"machine_name,omitempty"`
	ProjectPath   string    `json:"project_path,omitempty"`
	Limit         int       `json:"limit"`
	IndexPath     *[]string `json:"index_path,omitempty"`
	PathTreeDepth int       `json:"path_tree_depth,omitempty"`
	PathTreeWidth int       `json:"path_tree_width,omitempty"`
}

type AxisCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type IndexAxis struct {
	Axis   string      `json:"axis"`
	Values []AxisCount `json:"values"`
}

type IndexPathCount struct {
	Path  []string `json:"path"`
	Count int      `json:"count"`
}

type IndexPathNode struct {
	Name     string          `json:"name"`
	Count    int             `json:"count"`
	Children []IndexPathNode `json:"children,omitempty"`
}

type DepthCount struct {
	Depth int `json:"depth"`
	Count int `json:"count"`
}

type IndexStats struct {
	TotalMemories     int          `json:"total_memories"`
	AxesCoverage      float64      `json:"axes_coverage"`
	IndexPathCoverage float64      `json:"index_path_coverage"`
	AvgPathDepth      float64      `json:"avg_path_depth"`
	MaxPathDepth      int          `json:"max_path_depth"`
	BranchingFactor   float64      `json:"branching_factor"`
	DepthDistribution []DepthCount `json:"depth_distribution,omitempty"`
}

type MetricsResponse struct {
	Content string `json:"content"`
}

type IndexResponse struct {
	Axes     []IndexAxis      `json:"axes"`
	Paths    []IndexPathCount `json:"paths"`
	PathTree []IndexPathNode  `json:"path_tree,omitempty"`
	Stats    IndexStats       `json:"stats,omitempty"`
	Metadata SearchMetadata   `json:"metadata"`
}

// === 仲裁历史与回滚 ===

type ArbitrationHistoryInput struct {
	OwnerID   string `json:"owner_id"`
	MemoryID  string `json:"memory_id,omitempty"`  // 可选：查特定记忆的仲裁历史
	ProjectKey string `json:"project_key,omitempty"` // 可选：查特定项目的仲裁历史
	Limit     int    `json:"limit"`
}

type ArbitrationRecord struct {
	ID                int64   `json:"id"`
	CandidateMemoryID string  `json:"candidate_memory_id"`
	NewMemoryID       string  `json:"new_memory_id"`
	Action            string  `json:"action"`
	Similarity        float64 `json:"similarity"`
	OldSummary        string  `json:"old_summary"`
	NewSummary        string  `json:"new_summary"`
	Model             string  `json:"model"`
	CreatedAt         int64   `json:"created_at"`
}

type ArbitrationHistoryResponse struct {
	Results  []ArbitrationRecord `json:"results"`
	Metadata SearchMetadata      `json:"metadata"`
}

type RollbackInput struct {
	OwnerID       string `json:"owner_id"`
	ArbitrationID int64  `json:"arbitration_id"` // 要回滚的仲裁记录 ID
}

type RollbackOutput struct {
	Status          string `json:"status"`
	RestoredMemoryID string `json:"restored_memory_id"`
	Message         string `json:"message"`
}

// === 记忆演进链 ===

type MemoryChainInput struct {
	OwnerID  string `json:"owner_id"`
	MemoryID string `json:"memory_id"` // 查询此记忆的演进链
}

type MemoryVersion struct {
	VersionID   int64  `json:"version_id"`
	Summary     string `json:"summary"`
	ContentType string `json:"content_type"`
	Ts          int64  `json:"ts"`
	ReplacedAt  int64  `json:"replaced_at"`
}

type MemoryChainResponse struct {
	MemoryID       string          `json:"memory_id"`
	CurrentSummary string          `json:"current_summary"`
	Versions       []MemoryVersion `json:"versions"` // 历史版本（从新到旧）
	Arbitrations   []ArbitrationRecord `json:"arbitrations"` // 相关仲裁记录
}
