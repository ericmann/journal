// Package store is the sqlite-vec persistence layer: schema, migrations, and
// the upsert/delete/query primitives over chunks, their tags/markers, and their
// embeddings. The database is a derived, disposable cache — markdown is the
// source of truth — so it is always safe to delete and rebuild.
//
// It uses the pure-Go ncruces/go-sqlite3 driver (WASM via wazero) with
// sqlite-vec compiled into the bundled binary, so no cgo is required. See
// docs/DECISIONS.md for the driver rationale and version lock.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	_ "github.com/ncruces/go-sqlite3/driver" // registers the "sqlite3" driver
)

// SchemaVersion is the current schema version written to PRAGMA user_version.
const SchemaVersion = 1

// Chunk is one indexed unit of a note: a heading block with its location,
// body, parsed metadata, and timestamps.
type Chunk struct {
	ID        string // stable hash(path|anchor|body)
	Path      string // repo-relative path
	LineStart int
	LineEnd   int
	Heading   string
	Body      string
	Project   string
	CreatedAt time.Time // parsed from daily date + block time; zero if unknown
	IndexedAt time.Time
	Tags      []string
	Markers   []string
}

// Candidate is a KNN hit: a chunk plus its vector distance to the query.
type Candidate struct {
	Chunk
	Distance float64
}

// Filter narrows query results by metadata. Zero values mean "no constraint".
type Filter struct {
	Tags    []string  // chunk must have ALL of these tags
	Markers []string  // chunk must have ALL of these markers
	Project string    // chunk must belong to this project
	Since   time.Time // chunk CreatedAt must be >= Since
}

// Store wraps the sqlite-vec database. The embedding dimension is fixed for the
// life of a database file.
type Store struct {
	db  *sql.DB
	dim int
}

// Open opens (creating if needed) the sqlite-vec database at path with the given
// embedding dimension, running migrations. If the database already exists with a
// different dimension, it returns an error (rebuild required). Use ":memory:"
// for an ephemeral store.
func Open(path string, dim int) (*Store, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("embed dimension must be > 0, got %d", dim)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	// ncruces is safe for concurrent use, but the watcher/CLI is effectively
	// single-writer; keep one connection to avoid lock churn.
	db.SetMaxOpenConns(1)

	s := &Store{db: db, dim: dim}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.checkDim(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Dim returns the embedding dimension this store was opened with.
func (s *Store) Dim() int { return s.dim }

// SchemaVersion returns the database's current schema version.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// migrate applies pending migrations idempotently based on PRAGMA user_version.
func (s *Store) migrate(ctx context.Context) error {
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}
	if v >= SchemaVersion {
		return nil // up to date; a no-op on an already-migrated DB
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback on early return

	if v < 1 {
		if err := s.migrateV1(ctx, tx); err != nil {
			return fmt.Errorf("migrating to schema v1: %w", err)
		}
	}

	// PRAGMA user_version cannot be parameterized.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) migrateV1(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE chunks (
			id          TEXT PRIMARY KEY,
			path        TEXT NOT NULL,
			line_start  INTEGER NOT NULL,
			line_end    INTEGER NOT NULL,
			heading     TEXT,
			body        TEXT NOT NULL,
			project     TEXT,
			created_at  TEXT,
			indexed_at  TEXT NOT NULL
		)`,
		`CREATE TABLE tags    (chunk_id TEXT NOT NULL, tag    TEXT NOT NULL)`,
		`CREATE TABLE markers (chunk_id TEXT NOT NULL, marker TEXT NOT NULL)`,
		`CREATE TABLE meta    (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE INDEX idx_chunks_path    ON chunks(path)`,
		`CREATE INDEX idx_chunks_project ON chunks(project)`,
		`CREATE INDEX idx_chunks_created ON chunks(created_at)`,
		`CREATE INDEX idx_tags_chunk     ON tags(chunk_id)`,
		`CREATE INDEX idx_tags_tag       ON tags(tag)`,
		`CREATE INDEX idx_markers_chunk  ON markers(chunk_id)`,
		`CREATE INDEX idx_markers_marker ON markers(marker)`,
		// vec0 virtual table; the dimension is baked in at creation time.
		fmt.Sprintf(`CREATE VIRTUAL TABLE vec_chunks USING vec0(
			chunk_id TEXT PRIMARY KEY,
			embedding float[%d]
		)`, s.dim),
	}
	for _, q := range stmts {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("exec %q: %w", firstLine(q), err)
		}
	}
	// Record the dimension so a later Open can detect a mismatch.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES('embed_dim', ?)`, fmt.Sprint(s.dim)); err != nil {
		return err
	}
	return nil
}

// checkDim verifies the stored embedding dimension matches s.dim.
func (s *Store) checkDim(ctx context.Context) error {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='embed_dim'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // pre-dim DB; nothing to check
	}
	if err != nil {
		return err
	}
	if v != fmt.Sprint(s.dim) {
		return fmt.Errorf("embed dimension mismatch: database was built with %s but config requests %d; run `journal index --rebuild`", v, s.dim)
	}
	return nil
}

// Upsert inserts or replaces a chunk together with its embedding, tags, and
// markers, atomically. The embedding length must equal the store dimension.
func (s *Store) Upsert(ctx context.Context, c Chunk, embedding []float32) error {
	if len(embedding) != s.dim {
		return fmt.Errorf("embedding length %d != store dimension %d", len(embedding), s.dim)
	}
	blob, err := sqlitevec.SerializeFloat32(embedding)
	if err != nil {
		return fmt.Errorf("serializing embedding: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback on early return

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chunks(id, path, line_start, line_end, heading, body, project, created_at, indexed_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			path=excluded.path, line_start=excluded.line_start, line_end=excluded.line_end,
			heading=excluded.heading, body=excluded.body, project=excluded.project,
			created_at=excluded.created_at, indexed_at=excluded.indexed_at`,
		c.ID, c.Path, c.LineStart, c.LineEnd, c.Heading, c.Body, c.Project,
		nullableTime(c.CreatedAt), timeString(c.IndexedAt)); err != nil {
		return fmt.Errorf("upsert chunk: %w", err)
	}

	if err := replaceMulti(ctx, tx, "tags", "tag", c.ID, c.Tags); err != nil {
		return err
	}
	if err := replaceMulti(ctx, tx, "markers", "marker", c.ID, c.Markers); err != nil {
		return err
	}

	// vec0 has no UPSERT; delete any existing vector then insert.
	if _, err := tx.ExecContext(ctx, `DELETE FROM vec_chunks WHERE chunk_id=?`, c.ID); err != nil {
		return fmt.Errorf("clearing old vector: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO vec_chunks(chunk_id, embedding) VALUES(?, ?)`, c.ID, blob); err != nil {
		return fmt.Errorf("insert vector: %w", err)
	}
	return tx.Commit()
}

func replaceMulti(ctx context.Context, tx *sql.Tx, table, col, chunkID string, vals []string) error {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE chunk_id=?`, table), chunkID); err != nil {
		return fmt.Errorf("clearing %s: %w", table, err)
	}
	for _, v := range vals {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s(chunk_id, %s) VALUES(?, ?)`, table, col), chunkID, v); err != nil {
			return fmt.Errorf("insert %s: %w", table, err)
		}
	}
	return nil
}

// Delete removes the given chunk ids and all their associated rows. It is a
// no-op for ids that do not exist.
func (s *Store) Delete(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback on early return
	for _, id := range ids {
		for _, q := range []string{
			`DELETE FROM chunks     WHERE id=?`,
			`DELETE FROM tags       WHERE chunk_id=?`,
			`DELETE FROM markers    WHERE chunk_id=?`,
			`DELETE FROM vec_chunks WHERE chunk_id=?`,
		} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return fmt.Errorf("delete %s: %w", id, err)
			}
		}
	}
	return tx.Commit()
}

// UpdateLines updates only the location of an already-indexed chunk (and its
// indexed_at) without touching its embedding. The incremental indexer calls
// this for chunks whose content is unchanged but whose line numbers shifted
// because an earlier block in the same file grew or shrank — so re-indexing
// never re-embeds unchanged content.
func (s *Store) UpdateLines(ctx context.Context, id string, lineStart, lineEnd int, indexedAt time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE chunks SET line_start=?, line_end=?, indexed_at=? WHERE id=?`,
		lineStart, lineEnd, timeString(indexedAt), id)
	if err != nil {
		return fmt.Errorf("update lines for %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update lines: chunk %s not found", id)
	}
	return nil
}

// ChunkIDsByPath returns the set of chunk ids currently stored for a path. The
// incremental indexer uses this to compute which chunks disappeared.
func (s *Store) ChunkIDsByPath(ctx context.Context, path string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM chunks WHERE path=?`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Count returns the number of indexed chunks.
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chunks`).Scan(&n)
	return n, err
}

// Get returns a single chunk by id, with its tags and markers populated.
func (s *Store) Get(ctx context.Context, id string) (Chunk, error) {
	var c Chunk
	var created sql.NullString
	var indexed string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, path, line_start, line_end, heading, body, project, created_at, indexed_at
		FROM chunks WHERE id=?`, id).
		Scan(&c.ID, &c.Path, &c.LineStart, &c.LineEnd, &c.Heading, &c.Body, &c.Project, &created, &indexed)
	if errors.Is(err, sql.ErrNoRows) {
		return Chunk{}, fmt.Errorf("chunk %s not found", id)
	}
	if err != nil {
		return Chunk{}, err
	}
	c.CreatedAt = parseTime(created.String)
	c.IndexedAt = parseTime(indexed)
	if c.Tags, err = s.multi(ctx, "tags", "tag", id); err != nil {
		return Chunk{}, err
	}
	if c.Markers, err = s.multi(ctx, "markers", "marker", id); err != nil {
		return Chunk{}, err
	}
	return c, nil
}

func (s *Store) multi(ctx context.Context, table, col, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE chunk_id=? ORDER BY rowid`, col, table), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// KNN returns up to k nearest chunks to the query vector, ordered by ascending
// distance, after applying the filter. The vector search fetches the nearest
// candidates; metadata filtering is applied to those candidates. To keep
// filtered result counts close to k, callers performing heavy filtering should
// pass a larger k (the retriever over-fetches, then reranks).
func (s *Store) KNN(ctx context.Context, query []float32, k int, f Filter) ([]Candidate, error) {
	if len(query) != s.dim {
		return nil, fmt.Errorf("query length %d != store dimension %d", len(query), s.dim)
	}
	if k <= 0 {
		return nil, fmt.Errorf("k must be > 0, got %d", k)
	}
	blob, err := sqlitevec.SerializeFloat32(query)
	if err != nil {
		return nil, fmt.Errorf("serializing query: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT chunk_id, distance FROM vec_chunks
		WHERE embedding MATCH ? ORDER BY distance LIMIT ?`, blob, k)
	if err != nil {
		return nil, fmt.Errorf("knn query: %w", err)
	}
	type hit struct {
		id   string
		dist float64
	}
	var hits []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.dist); err != nil {
			rows.Close()
			return nil, err
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var out []Candidate
	for _, h := range hits {
		c, err := s.Get(ctx, h.id)
		if err != nil {
			return nil, err
		}
		if !matches(c, f) {
			continue
		}
		out = append(out, Candidate{Chunk: c, Distance: h.dist})
	}
	return out, nil
}

// matches reports whether a chunk satisfies the filter.
func matches(c Chunk, f Filter) bool {
	if f.Project != "" && c.Project != f.Project {
		return false
	}
	if !f.Since.IsZero() && (c.CreatedAt.IsZero() || c.CreatedAt.Before(f.Since)) {
		return false
	}
	for _, want := range f.Tags {
		if !contains(c.Tags, want) {
			return false
		}
	}
	for _, want := range f.Markers {
		if !contains(c.Markers, want) {
			return false
		}
	}
	return true
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// timeString formats a time as RFC3339; the zero time becomes "".
func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
