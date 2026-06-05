// Package quill reads meeting transcripts from the local Quill app's SQLite
// database and renders them to Markdown for journal's transcript landing zone.
//
// Quill (macOS/Windows) stores everything locally in a Prisma-managed SQLite DB;
// it does not export files, so journal PULLS. ALL database access lives behind
// the Reader interface in this file, so a Quill schema change (the schema is
// app-internal and drifts) is a one-file fix. We open the DB strictly read-only
// from a temp copy and never write to it.
package quill

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Mirror the store's driver setup: the sqlite-vec binding sets the WASM
	// SQLite binary, the driver registers the "sqlite3" database/sql driver. Both
	// packages then share one WASM build (no conflict). We don't use vec here —
	// just plain read-only SQLite — but reusing the binding avoids a second WASM.
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// Segment is one speaker turn in a transcript.
type Segment struct {
	Speaker string
	Text    string
}

// Meeting is a normalized record extracted from Quill, independent of Quill's
// internal column layout.
type Meeting struct {
	ID           string
	Title        string
	Start        time.Time
	End          time.Time
	Type         string
	Participants []string
	Tags         []string
	Notes        string // Quill's AI-generated note markdown (Note.body), if any
	Transcript   []Segment
}

// Reader pulls meetings from a Quill database. It is the single seam for all
// schema-specific access.
type Reader interface {
	// Meetings returns meetings whose start is strictly after since (zero = all),
	// oldest first. Rows that can't be parsed are skipped (reported via warnf),
	// never fatal.
	Meetings(ctx context.Context, since time.Time, warnf func(string, ...any)) ([]Meeting, error)
	// Count returns the total number of meetings in the database.
	Count(ctx context.Context) (int, error)
	Close() error
}

// Open opens the Quill database read-only. It copies the DB (and any -wal/-shm
// sidecars) to a temp file first so it never contends with a running Quill, and
// never mutates the original.
func Open(dbPath string) (Reader, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("no Quill database configured (Quill runs on macOS/Windows only)")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("Quill database not found at %s: %w", dbPath, err)
	}
	tmp, err := os.MkdirTemp("", "journal-quill-")
	if err != nil {
		return nil, err
	}
	copyPath := filepath.Join(tmp, "quill.db")
	if err := copyFile(dbPath, copyPath); err != nil {
		os.RemoveAll(tmp)
		return nil, fmt.Errorf("copying Quill database: %w", err)
	}
	// Include WAL/SHM if present so we see committed-but-not-checkpointed data.
	for _, sfx := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + sfx); err == nil {
			_ = copyFile(dbPath+sfx, copyPath+sfx)
		}
	}

	db, err := sql.Open("sqlite3", "file:"+copyPath+"?mode=ro")
	if err != nil {
		os.RemoveAll(tmp)
		return nil, fmt.Errorf("opening Quill database: %w", err)
	}
	db.SetMaxOpenConns(1)
	return &dbReader{db: db, tmpDir: tmp}, nil
}

type dbReader struct {
	db     *sql.DB
	tmpDir string
}

func (r *dbReader) Close() error {
	err := r.db.Close()
	_ = os.RemoveAll(r.tmpDir)
	return err
}

func (r *dbReader) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "Meeting"`).Scan(&n)
	return n, err
}

// wantedMeetingCols is the set we read; we select only those actually present
// so a dropped/renamed column degrades to an empty value instead of an error.
var wantedMeetingCols = []string{
	"id", "start", "end", "type", "participants", "tags", "audio_transcript",
	"manualTitle", "llmTitle", "eventTitle", "title",
}

func (r *dbReader) Meetings(ctx context.Context, since time.Time, warnf func(string, ...any)) ([]Meeting, error) {
	if warnf == nil {
		warnf = func(string, ...any) {}
	}
	present, err := tableColumns(ctx, r.db, "Meeting")
	if err != nil {
		return nil, fmt.Errorf("reading Quill schema (it may have changed — see internal/quill/reader.go): %w", err)
	}
	var cols []string
	for _, c := range wantedMeetingCols {
		if present[c] {
			cols = append(cols, c)
		}
	}
	if !present["id"] || !present["start"] {
		return nil, fmt.Errorf("unexpected Quill schema: Meeting lacks id/start (see internal/quill/reader.go)")
	}

	q := `SELECT ` + quoteCols(cols) + ` FROM "Meeting"`
	var args []any
	if !since.IsZero() {
		q += ` WHERE start > ?`
		args = append(args, since.UTC().Format(time.RFC3339))
	}
	q += ` ORDER BY start ASC`

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying meetings: %w", err)
	}
	// Drain fully before any follow-up query: the store runs on a single
	// connection, so issuing latestNote() while this cursor is open deadlocks.
	var out []Meeting
	for rows.Next() {
		row, err := scanRow(rows, cols)
		if err != nil {
			warnf("quill: skipping a meeting row: %v", err)
			continue
		}
		m := meetingFromRow(row)
		if m.ID == "" {
			continue
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Now fetch each meeting's AI note (separate queries, cursor closed).
	for i := range out {
		out[i].Notes = r.latestNote(ctx, out[i].ID)
	}
	return out, nil
}

// latestNote returns the most recent non-empty AI note body for a meeting, or ""
// (best-effort; Note table/columns may be absent in a future schema).
func (r *dbReader) latestNote(ctx context.Context, meetingID string) string {
	var body sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT body FROM "Note" WHERE meeting_id=? AND body IS NOT NULL AND body != '' ORDER BY createdAt DESC LIMIT 1`,
		meetingID).Scan(&body)
	if err != nil {
		return ""
	}
	return body.String
}

// meetingFromRow builds a Meeting from a column→value map, defensively.
func meetingFromRow(row map[string]string) Meeting {
	m := Meeting{
		ID:    row["id"],
		Type:  row["type"],
		Title: firstNonEmpty(row["manualTitle"], row["llmTitle"], row["eventTitle"], row["title"], "Untitled meeting"),
		Start: parseQuillTime(row["start"]),
		End:   parseQuillTime(row["end"]),
	}
	m.Participants = parseStringList(row["participants"])
	m.Tags = parseStringList(row["tags"])
	m.Transcript = parseSegments(row["audio_transcript"])
	return m
}

// --- helpers ---

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func scanRow(rows *sql.Rows, cols []string) (map[string]string, error) {
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(cols))
	for i, c := range cols {
		out[c] = toString(vals[i])
	}
	return out, nil
}

func toString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		return fmt.Sprintf("%v", t)
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func quoteCols(cols []string) string {
	q := make([]string, len(cols))
	for i, c := range cols {
		q[i] = `"` + c + `"`
	}
	return strings.Join(q, ", ")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseQuillTime tolerates the common SQLite datetime encodings.
func parseQuillTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999-07:00", "2006-01-02 15:04:05", "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// parseStringList tolerates a JSON array of strings, a JSON array of objects
// with name/email, or a comma-separated string.
func parseStringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var strs []string
		if err := json.Unmarshal([]byte(raw), &strs); err == nil {
			return cleanList(strs)
		}
		var objs []map[string]any
		if err := json.Unmarshal([]byte(raw), &objs); err == nil {
			var out []string
			for _, o := range objs {
				if s := firstField(o, "name", "displayName", "email", "label"); s != "" {
					out = append(out, s)
				}
			}
			return cleanList(out)
		}
	}
	return cleanList(strings.Split(raw, ","))
}

func cleanList(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// parseSegments tolerantly extracts speaker-labeled turns from the
// audio_transcript JSON. Quill's exact shape is app-internal and unverified
// (the test DB had no meetings), so this tries the common shapes and degrades to
// nil rather than failing. Validate against a real recorded meeting and adjust.
func parseSegments(raw string) []Segment {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Shape A: top-level array of segment objects.
	if segs := segmentsFromArray([]byte(raw)); segs != nil {
		return segs
	}
	// Shape B: object wrapping the array under a common key.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		for _, key := range []string{"segments", "transcript", "turns", "utterances", "results"} {
			if v, ok := obj[key]; ok {
				if segs := segmentsFromArray(v); segs != nil {
					return segs
				}
			}
		}
	}
	return nil
}

func segmentsFromArray(b []byte) []Segment {
	var arr []map[string]any
	if err := json.Unmarshal(b, &arr); err != nil {
		return nil
	}
	var out []Segment
	for _, o := range arr {
		text := firstField(o, "text", "transcript", "content", "value")
		if text == "" {
			if words := wordsText(o["words"]); words != "" {
				text = words
			}
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, Segment{
			Speaker: firstField(o, "speaker", "speakerName", "speaker_label", "speakerId", "speaker_id"),
			Text:    strings.TrimSpace(text),
		})
	}
	return out
}

func wordsText(v any) string {
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, w := range arr {
		switch t := w.(type) {
		case string:
			b.WriteString(t + " ")
		case map[string]any:
			b.WriteString(firstField(t, "text", "word", "value") + " ")
		}
	}
	return strings.TrimSpace(b.String())
}

func firstField(o map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := o[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
