package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

type Store struct {
	pool *pgxpool.Pool
}

type ProjectRecord struct {
	ID          string
	ProjectName string
	ProjectKey  string
	OwnerID     string
}

type TimelineRecord struct {
	ID          string
	ContentType string
	Summary     string
	Ts          int64
}

type MemoryRow struct {
	ID          string
	ContentType string
	Content     string
	Summary     string
	Tags        []string
	Ts          int64
}

func NewStore(databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) EnsureSchema(ctx context.Context, dimension int, reset bool) error {
	if _, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto"); err != nil {
		return err
	}
	if reset {
		cleanup := `
DROP TABLE IF EXISTS fragments CASCADE;
DROP TABLE IF EXISTS memories CASCADE;
DROP TABLE IF EXISTS projects CASCADE;
DROP TABLE IF EXISTS knowledge CASCADE;`
		if _, err := s.pool.Exec(ctx, cleanup); err != nil {
			return err
		}
	}

	schema := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS projects (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id TEXT NOT NULL,
  project_key TEXT NOT NULL,
  project_name TEXT NOT NULL,
  machine_name TEXT,
  project_path TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(owner_id, project_key)
);

CREATE TABLE IF NOT EXISTS memories (
  id TEXT PRIMARY KEY,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  content_type TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT,
  ts BIGINT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW(),
  summary TEXT,
  tags JSONB,
  chunk_count INT DEFAULT 1,
  embedding_done BOOLEAN DEFAULT false,
  avg_embedding VECTOR(%[1]d)
);

CREATE TABLE IF NOT EXISTS fragments (
  id TEXT PRIMARY KEY,
  memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  chunk_index INT NOT NULL,
  content TEXT NOT NULL,
  embedding VECTOR(%[1]d),
  ts TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(memory_id, chunk_index)
);

CREATE TABLE IF NOT EXISTS memory_versions (
  id BIGSERIAL PRIMARY KEY,
  memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  content_type TEXT NOT NULL,
  content TEXT NOT NULL,
  content_hash TEXT,
  ts BIGINT NOT NULL,
  summary TEXT,
  tags JSONB,
  chunk_count INT DEFAULT 1,
  avg_embedding VECTOR(%[1]d),
  created_at TIMESTAMPTZ,
  replaced_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS memory_arbitrations (
  id BIGSERIAL PRIMARY KEY,
  owner_id TEXT NOT NULL,
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  candidate_memory_id TEXT,
  new_memory_id TEXT,
  action TEXT NOT NULL,
  similarity DOUBLE PRECISION,
  old_summary TEXT,
  new_summary TEXT,
  model TEXT,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
`, dimension)

	if _, err := s.pool.Exec(ctx, schema); err != nil {
		return err
	}

	// 迁移：为已有表添加新字段（幂等操作）
	migrations := []string{
		// projects 表新增 owner_id 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='projects' AND column_name='owner_id') THEN
				ALTER TABLE projects ADD COLUMN owner_id TEXT;
			END IF;
		END $$`,
		// projects 表新增 project_key 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='projects' AND column_name='project_key') THEN
				ALTER TABLE projects ADD COLUMN project_key TEXT;
			END IF;
		END $$`,
		// projects 表 machine_name 改为可空
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='projects' AND column_name='machine_name' AND is_nullable='NO') THEN
				ALTER TABLE projects ALTER COLUMN machine_name DROP NOT NULL;
			END IF;
		END $$`,
		// projects 表 project_path 改为可空
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='projects' AND column_name='project_path' AND is_nullable='NO') THEN
				ALTER TABLE projects ALTER COLUMN project_path DROP NOT NULL;
			END IF;
		END $$`,
		// 移除旧的唯一约束（machine_name, project_path）
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname='projects_machine_name_project_path_key') THEN
				ALTER TABLE projects DROP CONSTRAINT projects_machine_name_project_path_key;
			END IF;
		END $$`,
		// memories 表添加 avg_embedding 字段
		fmt.Sprintf(`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='avg_embedding') THEN
				ALTER TABLE memories ADD COLUMN avg_embedding VECTOR(%d);
			END IF;
		END $$`, dimension),
		// memories 表添加 updated_at 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='updated_at') THEN
				ALTER TABLE memories ADD COLUMN updated_at TIMESTAMPTZ DEFAULT NOW();
			END IF;
		END $$`,
		// memories 表添加 summary 字段（如果旧版本缺失）
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='summary') THEN
				ALTER TABLE memories ADD COLUMN summary TEXT;
			END IF;
		END $$`,
		// memories 表添加 tags 字段
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='memories' AND column_name='tags') THEN
				ALTER TABLE memories ADD COLUMN tags JSONB;
			END IF;
		END $$`,
	}
	for _, stmt := range migrations {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("迁移失败: %w", err)
		}
	}

	indexes := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_owner_key ON projects(owner_id, project_key)",
		"CREATE INDEX IF NOT EXISTS idx_projects_owner ON projects(owner_id)",
		"CREATE INDEX IF NOT EXISTS idx_projects_machine ON projects(machine_name)",
		"CREATE INDEX IF NOT EXISTS idx_projects_path ON projects(project_path)",
		"CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(project_name)",
		"CREATE INDEX IF NOT EXISTS idx_projects_key ON projects(project_key)",
		"CREATE INDEX IF NOT EXISTS idx_memories_project ON memories(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(content_type)",
		"CREATE INDEX IF NOT EXISTS idx_memories_ts ON memories(ts DESC)",
		"CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(content_hash)",
		"CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_memories_avg_embedding ON memories USING hnsw (avg_embedding vector_cosine_ops)",
		"CREATE INDEX IF NOT EXISTS idx_fragments_memory ON fragments(memory_id)",
		"CREATE INDEX IF NOT EXISTS idx_fragments_embedding ON fragments USING hnsw (embedding vector_cosine_ops)",
		"CREATE INDEX IF NOT EXISTS idx_fragments_fts ON fragments USING GIN (to_tsvector('simple', content))",
		"CREATE INDEX IF NOT EXISTS idx_memory_versions_memory ON memory_versions(memory_id)",
		"CREATE INDEX IF NOT EXISTS idx_memory_versions_project ON memory_versions(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_memory_arbitrations_project ON memory_arbitrations(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_memory_arbitrations_owner ON memory_arbitrations(owner_id)",
	}
	for _, stmt := range indexes {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) UpsertProject(ctx context.Context, ownerID, projectKey, projectName, machineName, projectPath string) (ProjectRecord, error) {
	query := `
INSERT INTO projects (owner_id, project_key, project_name, machine_name, project_path)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (owner_id, project_key)
DO UPDATE SET project_name = EXCLUDED.project_name,
              machine_name = CASE WHEN EXCLUDED.machine_name IS NULL OR EXCLUDED.machine_name = '' THEN projects.machine_name ELSE EXCLUDED.machine_name END,
              project_path = CASE WHEN EXCLUDED.project_path IS NULL OR EXCLUDED.project_path = '' THEN projects.project_path ELSE EXCLUDED.project_path END,
              updated_at = NOW()
RETURNING id, project_name, project_key, owner_id`

	var (
		projectID   string
		storedName  string
		storedKey   string
		storedOwner string
	)
	if err := s.pool.QueryRow(ctx, query, ownerID, projectKey, projectName, nullableString(machineName), nullableString(projectPath)).Scan(&projectID, &storedName, &storedKey, &storedOwner); err != nil {
		return ProjectRecord{}, err
	}
	return ProjectRecord{ID: projectID, ProjectName: storedName, ProjectKey: storedKey, OwnerID: storedOwner}, nil
}

func (s *Store) FindProjectIDByKey(ctx context.Context, ownerID, projectKey string) (string, error) {
	query := `SELECT id FROM projects WHERE owner_id = $1 AND project_key = $2`
	var id string
	if err := s.pool.QueryRow(ctx, query, ownerID, projectKey).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

func (s *Store) BackfillProjectIdentity(ctx context.Context, ownerID string) error {
	owner := strings.TrimSpace(ownerID)
	if owner == "" {
		owner = defaultOwnerID
	}
	if _, err := s.pool.Exec(ctx, `
UPDATE projects
SET owner_id = $1
WHERE owner_id IS NULL OR owner_id = ''`, owner); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
UPDATE projects
SET project_key = COALESCE(NULLIF(project_key, ''), NULLIF(project_path, ''), project_name)
WHERE project_key IS NULL OR project_key = ''`); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
UPDATE projects
SET project_name = COALESCE(NULLIF(project_name, ''), project_key)
WHERE project_name IS NULL OR project_name = ''`); err != nil {
		return err
	}
	return nil
}

func (s *Store) FindDuplicateMemory(ctx context.Context, projectID, contentHash string, sinceTs int64) (string, error) {
	query := `SELECT id FROM memories WHERE project_id = $1 AND content_hash = $2 AND ts >= $3 LIMIT 1`
	var id string
	if err := s.pool.QueryRow(ctx, query, projectID, contentHash, sinceTs).Scan(&id); err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

func (s *Store) UpdateMemoryTimestamp(ctx context.Context, memoryID string, ts int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE memories SET ts = $2, updated_at = NOW() WHERE id = $1`, memoryID, ts)
	return err
}

func (s *Store) InsertMemory(ctx context.Context, memory MemoryInsert) error {
	tagsJSON, _ := json.Marshal(memory.Tags)
	var avgVec any
	if len(memory.AvgEmbedding) > 0 {
		avgVec = pgvector.NewVector(memory.AvgEmbedding)
	}
	_, err := s.pool.Exec(ctx, `
INSERT INTO memories (
  id, project_id, content_type, content, content_hash, ts,
  summary, tags, chunk_count, embedding_done, avg_embedding
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11)`,
		memory.ID,
		memory.ProjectID,
		memory.ContentType,
		memory.Content,
		memory.ContentHash,
		memory.Ts,
		nullableString(memory.Summary),
		string(tagsJSON),
		memory.ChunkCount,
		memory.Embedded,
		avgVec,
	)
	return err
}

func (s *Store) InsertFragments(ctx context.Context, fragments []FragmentInsert) error {
	if len(fragments) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	query := `
INSERT INTO fragments (id, memory_id, chunk_index, content, embedding)
VALUES ($1,$2,$3,$4,$5)`
	for _, frag := range fragments {
		batch.Queue(query, frag.ID, frag.MemoryID, frag.ChunkIndex, frag.Content, pgvector.NewVector(frag.Embedding))
	}
	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range fragments {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) FetchMemories(ctx context.Context, ids []string) ([]MemoryRow, error) {
	if len(ids) == 0 {
		return []MemoryRow{}, nil
	}
	query := `
SELECT id, content_type, content, COALESCE(summary, ''), COALESCE(tags, '[]'::jsonb), ts
FROM memories
WHERE id = ANY($1)`
	rows, err := s.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRow
	for rows.Next() {
		var (
			row      MemoryRow
			tagsJSON []byte
		)
		if err := rows.Scan(&row.ID, &row.ContentType, &row.Content, &row.Summary, &tagsJSON, &row.Ts); err != nil {
			return nil, err
		}
		row.Tags = decodeTags(tagsJSON)
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) FetchMemorySnapshot(ctx context.Context, memoryID string) (MemorySnapshot, error) {
	query := `
SELECT id,
       project_id,
       content_type,
       content,
       content_hash,
       ts,
       COALESCE(summary, ''),
       COALESCE(tags, '[]'::jsonb),
       chunk_count,
       COALESCE(avg_embedding::text, ''),
       created_at
FROM memories
WHERE id = $1`
	var (
		row      MemorySnapshot
		tagsJSON []byte
		avgText  string
	)
	if err := s.pool.QueryRow(ctx, query, memoryID).Scan(
		&row.ID,
		&row.ProjectID,
		&row.ContentType,
		&row.Content,
		&row.ContentHash,
		&row.Ts,
		&row.Summary,
		&tagsJSON,
		&row.ChunkCount,
		&avgText,
		&row.CreatedAt,
	); err != nil {
		return MemorySnapshot{}, err
	}
	row.Tags = decodeTags(tagsJSON)
	if strings.TrimSpace(avgText) != "" {
		var vec pgvector.Vector
		if err := vec.Parse(avgText); err == nil {
			row.AvgEmbedding = vec.Slice()
		}
	}
	return row, nil
}

func (s *Store) InsertMemoryVersion(ctx context.Context, version MemoryVersionInsert) error {
	tagsJSON, _ := json.Marshal(version.Tags)
	var avgVec any
	if len(version.AvgEmbedding) > 0 {
		avgVec = pgvector.NewVector(version.AvgEmbedding)
	}
	_, err := s.pool.Exec(ctx, `
INSERT INTO memory_versions (
  memory_id, project_id, content_type, content, content_hash, ts,
  summary, tags, chunk_count, avg_embedding, created_at, replaced_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12)`,
		version.MemoryID,
		version.ProjectID,
		version.ContentType,
		version.Content,
		version.ContentHash,
		version.Ts,
		nullableString(version.Summary),
		string(tagsJSON),
		version.ChunkCount,
		avgVec,
		version.CreatedAt,
		version.ReplacedAt,
	)
	return err
}

func (s *Store) InsertArbitrationLog(ctx context.Context, log ArbitrationLogInsert) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO memory_arbitrations (
  owner_id, project_id, candidate_memory_id, new_memory_id,
  action, similarity, old_summary, new_summary, model, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		log.OwnerID,
		log.ProjectID,
		nullableString(log.CandidateMemoryID),
		nullableString(log.NewMemoryID),
		log.Action,
		log.Similarity,
		nullableString(log.OldSummary),
		nullableString(log.NewSummary),
		nullableString(log.Model),
		log.CreatedAt,
	)
	return err
}

func (s *Store) FetchTimeline(ctx context.Context, projectID string, sinceTs int64, limit int) ([]TimelineRecord, error) {
	query := `
SELECT id, content_type, COALESCE(summary, ''), ts
FROM memories
WHERE project_id = $1 AND ts >= $2
ORDER BY ts DESC
LIMIT $3`
	rows, err := s.pool.Query(ctx, query, projectID, sinceTs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []TimelineRecord
	for rows.Next() {
		var row TimelineRecord
		if err := rows.Scan(&row.ID, &row.ContentType, &row.Summary, &row.Ts); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) FetchTimelineByOwner(ctx context.Context, ownerID string, sinceTs int64, limit int) ([]TimelineRecord, error) {
	query := `
SELECT m.id, m.content_type, COALESCE(m.summary, ''), m.ts
FROM memories m
JOIN projects p ON m.project_id = p.id
WHERE p.owner_id = $1 AND m.ts >= $2
ORDER BY m.ts DESC
LIMIT $3`
	rows, err := s.pool.Query(ctx, query, ownerID, sinceTs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []TimelineRecord
	for rows.Next() {
		var row TimelineRecord
		if err := rows.Scan(&row.ID, &row.ContentType, &row.Summary, &row.Ts); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) ListProjects(ctx context.Context, ownerID string, limit int) ([]ProjectListItem, error) {
	query := `
SELECT p.owner_id,
       p.project_key,
       p.machine_name,
       p.project_path,
       p.project_name,
       COUNT(m.id) as memory_count,
       COALESCE(MAX(m.ts), 0) as latest_ts
FROM projects p
LEFT JOIN memories m ON m.project_id = p.id
WHERE ($1 = '' OR p.owner_id = $1)
GROUP BY p.id
ORDER BY COALESCE(MAX(m.ts), 0) DESC
LIMIT $2`

	rows, err := s.pool.Query(ctx, query, ownerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ProjectListItem
	for rows.Next() {
		var item ProjectListItem
		var machineName sql.NullString
		var projectPath sql.NullString
		if err := rows.Scan(&item.OwnerID, &item.ProjectKey, &machineName, &projectPath, &item.ProjectName, &item.MemoryCount, &item.LatestTs); err != nil {
			return nil, err
		}
		item.MachineName = machineName.String
		item.ProjectPath = projectPath.String
		results = append(results, item)
	}
	return results, rows.Err()
}

// MemoryVectorRow represents a memory with its vector distance for conflict detection
type MemoryVectorRow struct {
	ID          string
	ContentType string
	Distance    float64
}

// MemorySummaryRow 用于仲裁时获取旧摘要
type MemorySummaryRow struct {
	ID      string
	Summary string
}

// FetchMemorySummary 获取指定 memory 的摘要（用于仲裁）
func (s *Store) FetchMemorySummary(ctx context.Context, memoryID string) (MemorySummaryRow, error) {
	query := `SELECT id, COALESCE(summary, '') FROM memories WHERE id = $1`
	var row MemorySummaryRow
	if err := s.pool.QueryRow(ctx, query, memoryID).Scan(&row.ID, &row.Summary); err != nil {
		return MemorySummaryRow{}, err
	}
	return row, nil
}

// SearchMemoryVectors searches memories by avg_embedding for semantic conflict detection
// 只按 project_id 过滤，不按 content_type（因为类型不严格互斥）
func (s *Store) SearchMemoryVectors(ctx context.Context, vector pgvector.Vector, projectID string, limit int) ([]MemoryVectorRow, error) {
	query := `
SELECT id, content_type, (avg_embedding <=> $1) AS distance
FROM memories
WHERE project_id = $2 AND avg_embedding IS NOT NULL
ORDER BY avg_embedding <=> $1
LIMIT $3`
	rows, err := s.pool.Query(ctx, query, vector, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryVectorRow
	for rows.Next() {
		var row MemoryVectorRow
		if err := rows.Scan(&row.ID, &row.ContentType, &row.Distance); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) SearchVectorFragments(ctx context.Context, vector pgvector.Vector, projectID, scope string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, m.ts, m.chunk_count, (f.embedding <=> $1) AS distance
FROM fragments f
JOIN memories m ON f.memory_id = m.id
WHERE m.project_id = $2`
	args := []any{vector, projectID}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query += " ORDER BY f.embedding <=> $1 LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFragmentRows(rows)
}

func (s *Store) SearchVectorFragmentsByOwner(ctx context.Context, vector pgvector.Vector, ownerID, scope string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, m.ts, m.chunk_count, (f.embedding <=> $1) AS distance
FROM fragments f
JOIN memories m ON f.memory_id = m.id
JOIN projects p ON m.project_id = p.id
WHERE p.owner_id = $2`
	args := []any{vector, ownerID}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query += " ORDER BY f.embedding <=> $1 LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFragmentRows(rows)
}

func (s *Store) SearchKeywordFragments(ctx context.Context, keyword, projectID, scope string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, m.ts, m.chunk_count, 0 AS distance
FROM fragments f
JOIN memories m ON f.memory_id = m.id
WHERE m.project_id = $1 AND f.content ILIKE $2`
	args := []any{projectID, fmt.Sprintf("%%%s%%", keyword)}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query += " ORDER BY m.ts DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFragmentRows(rows)
}

func (s *Store) SearchKeywordFragmentsByOwner(ctx context.Context, keyword, ownerID, scope string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, m.ts, m.chunk_count, 0 AS distance
FROM fragments f
JOIN memories m ON f.memory_id = m.id
JOIN projects p ON m.project_id = p.id
WHERE p.owner_id = $1 AND f.content ILIKE $2`
	args := []any{ownerID, fmt.Sprintf("%%%s%%", keyword)}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query += " ORDER BY m.ts DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFragmentRows(rows)
}

func (s *Store) SearchBM25Fragments(ctx context.Context, keyword, projectID, scope string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, m.ts, m.chunk_count,
       ts_rank_cd(to_tsvector('simple', f.content), plainto_tsquery('simple', $2)) AS rank
FROM fragments f
JOIN memories m ON f.memory_id = m.id
WHERE m.project_id = $1 AND to_tsvector('simple', f.content) @@ plainto_tsquery('simple', $2)`
	args := []any{projectID, keyword}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query += " ORDER BY rank DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FragmentRow
	for rows.Next() {
		var row FragmentRow
		if err := rows.Scan(&row.FragmentID, &row.MemoryID, &row.ChunkIndex, &row.Content, &row.ContentType, &row.Ts, &row.ChunkCount, &row.RankScore); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func (s *Store) SearchBM25FragmentsByOwner(ctx context.Context, keyword, ownerID, scope string, limit int) ([]FragmentRow, error) {
	query := `
SELECT f.id, f.memory_id, f.chunk_index, f.content, m.content_type, m.ts, m.chunk_count,
       ts_rank_cd(to_tsvector('simple', f.content), plainto_tsquery('simple', $2)) AS rank
FROM fragments f
JOIN memories m ON f.memory_id = m.id
JOIN projects p ON m.project_id = p.id
WHERE p.owner_id = $1 AND to_tsvector('simple', f.content) @@ plainto_tsquery('simple', $2)`
	args := []any{ownerID, keyword}
	if scope != "all" && scope != "" {
		query += " AND m.content_type = $3"
		args = append(args, scope)
	}
	query += " ORDER BY rank DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FragmentRow
	for rows.Next() {
		var row FragmentRow
		if err := rows.Scan(&row.FragmentID, &row.MemoryID, &row.ChunkIndex, &row.Content, &row.ContentType, &row.Ts, &row.ChunkCount, &row.RankScore); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func scanFragmentRows(rows pgx.Rows) ([]FragmentRow, error) {
	var results []FragmentRow
	for rows.Next() {
		var row FragmentRow
		if err := rows.Scan(&row.FragmentID, &row.MemoryID, &row.ChunkIndex, &row.Content, &row.ContentType, &row.Ts, &row.ChunkCount, &row.Distance); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

func decodeTags(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal(raw, &tags); err == nil {
		return normalizeTags(tags)
	}
	return []string{}
}

func baseName(path string) string {
	trimmed := strings.TrimRight(path, "/\\")
	if trimmed == "" {
		return path
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return trimmed
	}
	return parts[len(parts)-1]
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
