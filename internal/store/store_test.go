package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTemp(t *testing.T, dim int) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "journal.db"), dim)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// vec returns a deterministic unit-ish vector of length dim seeded by n.
func vec(dim, n int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = float32((i*7+n*13)%11) / 10.0
	}
	return v
}

func sampleChunk(id, path, project string, tags, markers []string) Chunk {
	return Chunk{
		ID:        id,
		Path:      path,
		LineStart: 1,
		LineEnd:   3,
		Heading:   "09:14",
		Body:      "body of " + id,
		Project:   project,
		CreatedAt: time.Date(2026, 6, 1, 9, 14, 0, 0, time.UTC),
		IndexedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		Tags:      tags,
		Markers:   markers,
	}
}

func TestOpenCreatesSchemaAtVersion(t *testing.T) {
	s := openTemp(t, 8)
	v, err := s.SchemaVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != SchemaVersion {
		t.Errorf("schema version = %d, want %d", v, SchemaVersion)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "j.db")
	s1, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	// Insert a row so we can prove re-opening doesn't wipe / re-migrate.
	ctx := context.Background()
	if err := s1.Upsert(ctx, sampleChunk("a", "daily/x.md", "", []string{"cabot"}, nil), vec(8, 1)); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	s2, err := Open(path, 8) // re-open: migrate must be a no-op
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer s2.Close()
	v, _ := s2.SchemaVersion(ctx)
	if v != SchemaVersion {
		t.Errorf("version after reopen = %d", v)
	}
	n, _ := s2.Count(ctx)
	if n != 1 {
		t.Errorf("row count after reopen = %d, want 1 (data preserved)", n)
	}
}

func TestDimMismatchOnReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "j.db")
	s, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(path, 16); err == nil {
		t.Error("expected dim-mismatch error reopening with a different dimension")
	}
}

func TestOpenRejectsBadDim(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "j.db"), 0); err == nil {
		t.Error("expected error for dim=0")
	}
}

func TestUpsertGetRoundTrip(t *testing.T) {
	s := openTemp(t, 8)
	ctx := context.Background()
	in := sampleChunk("a", "daily/2026/06/2026-06-01.md", "canton", []string{"cabot", "litellm"}, []string{"decision"})
	if err := s.Upsert(ctx, in, vec(8, 1)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != in.Path || got.Project != "canton" || got.Heading != "09:14" {
		t.Errorf("scalar fields wrong: %+v", got)
	}
	if !eq(got.Tags, []string{"cabot", "litellm"}) {
		t.Errorf("tags = %v", got.Tags)
	}
	if !eq(got.Markers, []string{"decision"}) {
		t.Errorf("markers = %v", got.Markers)
	}
	if !got.CreatedAt.Equal(in.CreatedAt) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, in.CreatedAt)
	}
}

func TestUpsertReplacesTagsMarkersAndVector(t *testing.T) {
	s := openTemp(t, 8)
	ctx := context.Background()
	if err := s.Upsert(ctx, sampleChunk("a", "p.md", "", []string{"old"}, []string{"todo"}), vec(8, 1)); err != nil {
		t.Fatal(err)
	}
	// Re-upsert same id with new tags/markers; old ones must not linger.
	if err := s.Upsert(ctx, sampleChunk("a", "p.md", "", []string{"new"}, []string{"decision"}), vec(8, 2)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, "a")
	if !eq(got.Tags, []string{"new"}) {
		t.Errorf("tags after re-upsert = %v, want [new]", got.Tags)
	}
	if !eq(got.Markers, []string{"decision"}) {
		t.Errorf("markers after re-upsert = %v", got.Markers)
	}
	n, _ := s.Count(ctx)
	if n != 1 {
		t.Errorf("count = %d, want 1 (upsert, not insert)", n)
	}
}

func TestUpsertRejectsWrongDimEmbedding(t *testing.T) {
	s := openTemp(t, 8)
	if err := s.Upsert(context.Background(), sampleChunk("a", "p.md", "", nil, nil), vec(4, 1)); err == nil {
		t.Error("expected error upserting embedding of wrong length")
	}
}

func TestDeleteRemovesAllRows(t *testing.T) {
	s := openTemp(t, 8)
	ctx := context.Background()
	if err := s.Upsert(ctx, sampleChunk("a", "p.md", "", []string{"t"}, []string{"todo"}), vec(8, 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Count(ctx); n != 0 {
		t.Errorf("count after delete = %d, want 0", n)
	}
	// Its vector must be gone too: KNN should return nothing.
	hits, err := s.KNN(ctx, vec(8, 1), 5, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("KNN returned %d hits after delete, want 0", len(hits))
	}
}

func TestDeleteMissingIsNoOp(t *testing.T) {
	s := openTemp(t, 8)
	if err := s.Delete(context.Background(), "does-not-exist"); err != nil {
		t.Errorf("deleting missing id should be a no-op, got %v", err)
	}
}

func TestChunkIDsByPath(t *testing.T) {
	s := openTemp(t, 8)
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if err := s.Upsert(ctx, sampleChunk(id, "daily/d.md", "", nil, nil), vec(8, len(id))); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Upsert(ctx, sampleChunk("c", "other.md", "", nil, nil), vec(8, 3)); err != nil {
		t.Fatal(err)
	}
	ids, err := s.ChunkIDsByPath(ctx, "daily/d.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || !contains(ids, "a") || !contains(ids, "b") {
		t.Errorf("ChunkIDsByPath = %v, want [a b]", ids)
	}
}

func TestKNNOrdersByDistance(t *testing.T) {
	s := openTemp(t, 8)
	ctx := context.Background()
	query := vec(8, 1)
	// 'near' is the query itself (distance 0); 'far' is a different vector.
	if err := s.Upsert(ctx, sampleChunk("near", "p.md", "", nil, nil), query); err != nil {
		t.Fatal(err)
	}
	far := make([]float32, 8)
	for i := range far {
		far[i] = 99
	}
	if err := s.Upsert(ctx, sampleChunk("far", "p.md", "", nil, nil), far); err != nil {
		t.Fatal(err)
	}
	hits, err := s.KNN(ctx, query, 2, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].ID != "near" {
		t.Errorf("nearest = %q, want near", hits[0].ID)
	}
	if hits[0].Distance > hits[1].Distance {
		t.Errorf("distances not ascending: %v > %v", hits[0].Distance, hits[1].Distance)
	}
}

func TestKNNFilters(t *testing.T) {
	s := openTemp(t, 8)
	ctx := context.Background()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	mk := func(id, project string, tags, markers []string, created time.Time) Chunk {
		c := sampleChunk(id, "p.md", project, tags, markers)
		c.CreatedAt = created
		return c
	}
	rows := []Chunk{
		mk("d1", "canton", []string{"tax"}, []string{"decision"}, recent),
		mk("d2", "canton", []string{"tax"}, []string{"question"}, recent),
		mk("d3", "displace", []string{"tax"}, []string{"decision"}, recent),
		mk("d4", "canton", []string{"tax"}, []string{"decision"}, old),
	}
	for i, c := range rows {
		if err := s.Upsert(ctx, c, vec(8, i)); err != nil {
			t.Fatal(err)
		}
	}
	q := vec(8, 0)

	// project + marker filter
	hits, err := s.KNN(ctx, q, 10, Filter{Project: "canton", Markers: []string{"decision"}})
	if err != nil {
		t.Fatal(err)
	}
	got := ids(hits)
	if !sameSet(got, []string{"d1", "d4"}) {
		t.Errorf("project+marker filter = %v, want {d1,d4}", got)
	}

	// since filter excludes the old chunk
	hits, _ = s.KNN(ctx, q, 10, Filter{Project: "canton", Markers: []string{"decision"}, Since: recent})
	if got := ids(hits); !sameSet(got, []string{"d1"}) {
		t.Errorf("since filter = %v, want {d1}", got)
	}

	// tag filter present on all
	hits, _ = s.KNN(ctx, q, 10, Filter{Tags: []string{"tax"}})
	if len(hits) != 4 {
		t.Errorf("tag filter returned %d, want 4", len(hits))
	}
}

func TestKNNValidates(t *testing.T) {
	s := openTemp(t, 8)
	ctx := context.Background()
	if _, err := s.KNN(ctx, vec(4, 1), 5, Filter{}); err == nil {
		t.Error("expected error for wrong query dimension")
	}
	if _, err := s.KNN(ctx, vec(8, 1), 0, Filter{}); err == nil {
		t.Error("expected error for k=0")
	}
}

func TestDimAndGetMissing(t *testing.T) {
	s := openTemp(t, 8)
	if s.Dim() != 8 {
		t.Errorf("Dim = %d, want 8", s.Dim())
	}
	if _, err := s.Get(context.Background(), "nope"); err == nil {
		t.Error("expected error getting missing chunk")
	}
}

func TestUpsertZeroCreatedAtStoresNull(t *testing.T) {
	s := openTemp(t, 8)
	ctx := context.Background()
	c := sampleChunk("a", "p.md", "", nil, nil)
	c.CreatedAt = time.Time{} // unknown timestamp
	if err := s.Upsert(ctx, c, vec(8, 1)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, "a")
	if !got.CreatedAt.IsZero() {
		t.Errorf("created_at = %v, want zero", got.CreatedAt)
	}
	// A Since filter must exclude chunks with unknown created_at.
	hits, _ := s.KNN(ctx, vec(8, 1), 5, Filter{Since: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if len(hits) != 0 {
		t.Errorf("since filter let through a zero-created_at chunk: %d hits", len(hits))
	}
}

func ids(cs []Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}
