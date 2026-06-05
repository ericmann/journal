package quill

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureDB writes a minimal Quill-shaped SQLite database and returns its path.
// Mirrors the real (Prisma) Meeting/Note column subset we read.
func fixtureDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quill.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE "Meeting" (id TEXT PRIMARY KEY, start TEXT, end TEXT, type TEXT,
			participants TEXT, tags TEXT, audio_transcript TEXT,
			manualTitle TEXT, llmTitle TEXT, eventTitle TEXT, title TEXT)`,
		`CREATE TABLE "Note" (id TEXT PRIMARY KEY, meeting_id TEXT, body TEXT, createdAt TEXT)`,
		`CREATE TABLE "Contact" (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE "ContactMeeting" (contact_id TEXT, speaker_id TEXT, meeting_id TEXT)`,
		// Real Quill shape: audio_transcript is an object with a blocks[] array,
		// each block {text, speaker_id}; speaker_id resolves via ContactMeeting.
		`INSERT INTO "Meeting" VALUES('m1','2026-06-05T14:00:00Z','2026-06-05T14:30:00Z','standup',
			'["Alice","Bob"]','["weekly"]',
			'{"blocks":[{"text":"Kickoff.","speaker_id":"SPK-a"},{"text":"On it.","speaker_id":"SPK-b"}]}',
			NULL,'Weekly Sync',NULL,'Calendar Title')`,
		`INSERT INTO "Meeting" VALUES('m0','2026-06-01T09:00:00Z','2026-06-01T09:15:00Z','standup',
			'[]','[]','bad-not-json',NULL,NULL,NULL,'Older Meeting')`,
		`INSERT INTO "Note" VALUES('n1','m1','## Summary\n- shipped the thing','2026-06-05T15:00:00Z')`,
		`INSERT INTO "Contact" VALUES('c-a','Alice')`,
		`INSERT INTO "ContactMeeting" VALUES('c-a','SPK-a','m1')`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("fixture exec: %v", err)
		}
	}
	return path
}

func TestReaderParsesMeetings(t *testing.T) {
	r, err := Open(fixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	ms, err := r.Meetings(context.Background(), time.Time{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("got %d meetings, want 2", len(ms))
	}
	// Oldest first.
	if ms[0].ID != "m0" || ms[1].ID != "m1" {
		t.Errorf("order = %s,%s want m0,m1", ms[0].ID, ms[1].ID)
	}
	m := ms[1]
	if m.Title != "Weekly Sync" { // llmTitle wins over title; manual/event empty
		t.Errorf("title = %q, want Weekly Sync", m.Title)
	}
	if len(m.Participants) != 2 || m.Participants[0] != "Alice" {
		t.Errorf("participants = %v", m.Participants)
	}
	if len(m.Transcript) != 2 || m.Transcript[0].Text != "Kickoff." {
		t.Errorf("transcript = %+v", m.Transcript)
	}
	// SPK-a resolves to the contact name; the unmapped speaker gets a stable label.
	if m.Transcript[0].Speaker != "Alice" {
		t.Errorf("speaker[0] = %q, want resolved name Alice", m.Transcript[0].Speaker)
	}
	if m.Transcript[1].Speaker != "Speaker 1" {
		t.Errorf("speaker[1] = %q, want fallback 'Speaker 1' (first unmapped speaker)", m.Transcript[1].Speaker)
	}
	if !strings.Contains(m.Notes, "shipped the thing") {
		t.Errorf("notes = %q", m.Notes)
	}
	// Malformed audio_transcript must not crash; it degrades to no segments.
	if len(ms[0].Transcript) != 0 {
		t.Errorf("bad transcript JSON should yield no segments, got %+v", ms[0].Transcript)
	}
}

func TestReaderSinceFilter(t *testing.T) {
	r, err := Open(fixtureDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	since := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	ms, err := r.Meetings(context.Background(), since, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].ID != "m1" {
		t.Errorf("since filter = %d meetings, want only m1", len(ms))
	}
}

func TestRenderMarkdown(t *testing.T) {
	m := Meeting{
		ID: "m1", Title: "Weekly Sync", Type: "standup",
		Start:        time.Date(2026, 6, 5, 14, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 6, 5, 14, 30, 0, 0, time.UTC),
		Participants: []string{"Alice", "Bob"}, Tags: []string{"weekly"},
		Notes:      "## Summary\n- shipped",
		Transcript: []Segment{{Speaker: "Alice", Text: "Kickoff."}},
	}
	out := RenderMarkdown(m)
	for _, want := range []string{"---\n", `meeting_id: "m1"`, "source: quill", "# Weekly Sync", "## Notes", "shipped", "## Transcript", "**Alice:** Kickoff."} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	if m.Filename() != "2026-06-05-weekly-sync.md" {
		t.Errorf("filename = %q", m.Filename())
	}
}

func TestParseQM(t *testing.T) {
	content := "QMv2\n" + `{"id":"q1","llmTitle":"QM Meeting","start":"2026-06-05T10:00:00Z",
		"participants":["Carol"],"tags":["demo"],
		"audio_transcript":[{"speaker":"Carol","text":"Hi there."}]}`
	m, err := ParseQM(content)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "q1" || m.Title != "QM Meeting" || len(m.Transcript) != 1 {
		t.Errorf("ParseQM = %+v", m)
	}
	if _, err := ParseQM("not a qm file"); err == nil {
		t.Error("expected error for non-qm content")
	}
}

func TestWatermarkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if w, err := LoadWatermark(dir); err != nil || !w.IsZero() {
		t.Errorf("missing watermark should be zero, got %v err %v", w, err)
	}
	want := time.Date(2026, 6, 5, 14, 0, 0, 0, time.UTC)
	if err := SaveWatermark(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWatermark(dir)
	if err != nil || !got.Equal(want) {
		t.Errorf("watermark round-trip = %v err %v, want %v", got, err, want)
	}
}

func TestOpenMissingDB(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Error("empty path should error (Quill unavailable)")
	}
	if _, err := Open(filepath.Join(t.TempDir(), "nope.db")); err == nil {
		t.Error("missing db should error")
	}
}
