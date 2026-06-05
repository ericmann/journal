package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeQuillFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "quill.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE "Meeting" (id TEXT PRIMARY KEY, start TEXT, "end" TEXT, type TEXT,
			participants TEXT, tags TEXT, audio_transcript TEXT,
			manualTitle TEXT, llmTitle TEXT, eventTitle TEXT, title TEXT)`,
		`CREATE TABLE "Note" (id TEXT PRIMARY KEY, meeting_id TEXT, body TEXT, createdAt TEXT)`,
		`INSERT INTO "Meeting" VALUES('m1','2026-06-05T14:00:00Z','2026-06-05T14:30:00Z','standup',
			'["Alice"]','["weekly"]','[{"speaker":"Alice","text":"Kickoff."}]',NULL,'Weekly Sync',NULL,NULL)`,
		`INSERT INTO "Note" VALUES('n1','m1','## Summary\n- shipped','2026-06-05T15:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	return path
}

func TestRunQuillSyncRendersAndWatermarks(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Quill.DBPath = writeQuillFixture(t) // absolute; bypasses macOS default

	var buf bytes.Buffer
	if err := runQuillSync(context.Background(), cfg, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "rendered 1 meeting") {
		t.Errorf("expected a rendered meeting, got: %s", buf.String())
	}
	// A transcript markdown file was written into the landing zone.
	got, err := os.ReadFile(filepath.Join(cfg.TranscriptsAbsPath(), "2026-06-05-weekly-sync.md"))
	if err != nil {
		t.Fatalf("transcript not written: %v", err)
	}
	if !strings.Contains(string(got), "# Weekly Sync") || !strings.Contains(string(got), "**Alice:** Kickoff.") {
		t.Errorf("rendered transcript wrong:\n%s", got)
	}

	// Second run is incremental: the watermark suppresses the already-synced meeting.
	var buf2 bytes.Buffer
	if err := runQuillSync(context.Background(), cfg, &buf2); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf2.String(), "no new meetings") {
		t.Errorf("second run should be a no-op, got: %s", buf2.String())
	}
}

func TestRunQuillSyncDisabled(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Quill.Enabled = false
	var buf bytes.Buffer
	if err := runQuillSync(context.Background(), cfg, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "disabled") {
		t.Errorf("expected disabled notice, got: %s", buf.String())
	}
}
