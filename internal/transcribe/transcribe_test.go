package transcribe

import (
	"strings"
	"testing"
	"time"
)

const sampleJSON = `{
  "segments": [
    {"start": 3.0,  "end": 6.0,  "text": " Welcome everyone. ", "speaker": "SPEAKER_00"},
    {"start": 6.5,  "end": 9.0,  "text": "Let's get started.",   "speaker": "SPEAKER_00"},
    {"start": 9.5,  "end": 12.0, "text": "Sounds good.",         "speaker": "SPEAKER_01"},
    {"start": 3700,  "end": 3702, "text": "Closing thoughts?",   "speaker": "SPEAKER_00"},
    {"start": 3703, "end": 3704, "text": "   ",                  "speaker": "SPEAKER_01"}
  ]
}`

func TestParseWhisperXTrimsAndDropsEmpty(t *testing.T) {
	segs, err := ParseWhisperX([]byte(sampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 4 { // the whitespace-only segment is dropped
		t.Fatalf("got %d segments, want 4", len(segs))
	}
	if segs[0].Text != "Welcome everyone." {
		t.Errorf("text not trimmed: %q", segs[0].Text)
	}
}

func TestParseWhisperXErrors(t *testing.T) {
	if _, err := ParseWhisperX([]byte(`not json`)); err == nil {
		t.Error("expected parse error")
	}
	if _, err := ParseWhisperX([]byte(`{"segments":[]}`)); err == nil {
		t.Error("expected no-segments error")
	}
}

func TestSpeakersDistinctInOrder(t *testing.T) {
	segs, _ := ParseWhisperX([]byte(sampleJSON))
	got := Speakers(segs)
	if len(got) != 2 || got[0] != "SPEAKER_00" || got[1] != "SPEAKER_01" {
		t.Errorf("speakers = %v", got)
	}
}

func TestRenderShapeAndCoalescing(t *testing.T) {
	segs, _ := ParseWhisperX([]byte(sampleJSON))
	date := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	md := Render("Acme Q2 Planning", date, segs, "OVERVIEW: a meeting.", []string{"meeting"})

	for _, want := range []string{
		"title: \"Acme Q2 Planning\"", "date: \"2026-06-02\"", "source: whisperx",
		"participants: [\"SPEAKER_00\", \"SPEAKER_01\"]", "tags: [\"meeting\"]",
		"# Acme Q2 Planning", "## Notes\n\nOVERVIEW: a meeting.", "## Transcript",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered md missing %q\n---\n%s", want, md)
		}
	}
	// Consecutive SPEAKER_00 lines coalesce into one turn.
	if !strings.Contains(md, "**[0:03] SPEAKER_00:** Welcome everyone. Let's get started.") {
		t.Errorf("segments not coalesced:\n%s", md)
	}
	// Hour-long offset formats as H:MM:SS.
	if !strings.Contains(md, "**[1:01:40] SPEAKER_00:** Closing thoughts?") {
		t.Errorf("hour timestamp wrong:\n%s", md)
	}
}

func TestRenderOmitsEmptyNotes(t *testing.T) {
	segs, _ := ParseWhisperX([]byte(sampleJSON))
	md := Render("X", time.Time{}, segs, "  ", nil)
	if strings.Contains(md, "## Notes") {
		t.Errorf("## Notes should be omitted when summary is empty:\n%s", md)
	}
	if !strings.Contains(md, "participants: [\"SPEAKER_00\", \"SPEAKER_01\"]") {
		t.Errorf("participants missing:\n%s", md)
	}
}

func TestFilename(t *testing.T) {
	d := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	if got := Filename(d, "Acme Customer Advisory Board 6/2"); got != "2026-06-02-acme-customer-advisory-board-6-2.md" {
		t.Errorf("Filename = %q", got)
	}
	if got := Filename(time.Time{}, ""); got != "undated-meeting.md" {
		t.Errorf("fallback Filename = %q", got)
	}
}
