package log

import (
	"strings"
	"testing"
	"time"
)

var testTime = time.Date(2026, 6, 30, 14, 35, 0, 0, time.UTC)

func TestAssembleShaped(t *testing.T) {
	in := AssembleInput{
		Title:             "Deploy Review",
		Summary:           "Reviewed the deploy logs.",
		Body:              "Found an anomaly in the deploy output.",
		RawText:           "um so I reviewed deploy logs today uh there was an anomaly",
		Tags:              []string{"deploy", "ops"},
		Markers:           []string{"@todo check the anomaly"},
		DurationSec:       0,
		Transcriber:       "text",
		KeepRawTranscript: true,
		CapturedAt:        testTime,
	}
	got := Assemble(in)

	// Frontmatter checks.
	if !strings.Contains(got, "source: voice") {
		t.Error("missing source: voice")
	}
	if !strings.Contains(got, `date: "2026-06-30"`) {
		t.Error("missing date")
	}
	if !strings.Contains(got, `transcriber: "text"`) {
		t.Error("missing transcriber")
	}
	if !strings.Contains(got, `"voice"`) {
		t.Error("missing voice tag")
	}
	if !strings.Contains(got, `"deploy"`) {
		t.Error("missing deploy tag")
	}
	if !strings.Contains(got, "todos: 1") {
		t.Error("missing todos count")
	}
	if !strings.Contains(got, "audio: null") {
		t.Error("missing audio: null")
	}

	// Document sections.
	if !strings.Contains(got, "# Deploy Review") {
		t.Error("missing title H1")
	}
	if !strings.Contains(got, "## Summary") {
		t.Error("missing Summary section")
	}
	if !strings.Contains(got, "## Notes") {
		t.Error("missing Notes section")
	}
	if !strings.Contains(got, "@todo check the anomaly") {
		t.Error("missing marker line")
	}

	// Raw transcript details block.
	if !strings.Contains(got, "<details>") {
		t.Error("missing <details> block")
	}
	if !strings.Contains(got, "um so I reviewed") {
		t.Error("missing raw text in details block")
	}
}

func TestAssembleRawFallback(t *testing.T) {
	in := AssembleInput{
		Title:             "",
		Summary:           "",
		Body:              "",
		RawText:           "deploy logs look fine nothing unusual",
		Tags:              nil,
		KeepRawTranscript: false,
		CapturedAt:        testTime,
		Transcriber:       "text",
	}
	got := Assemble(in)

	if !strings.Contains(got, "# Voice Note") {
		t.Error("missing default title")
	}
	if !strings.Contains(got, "## Notes") {
		t.Error("missing Notes section")
	}
	if !strings.Contains(got, "deploy logs look fine") {
		t.Error("missing raw text in Notes")
	}
	// No Summary section when no summary.
	if strings.Contains(got, "## Summary") {
		t.Error("Summary section should be absent when no summary")
	}
	// No details block when KeepRawTranscript=false.
	if strings.Contains(got, "<details>") {
		t.Error("<details> block should be absent when KeepRawTranscript=false")
	}
}

func TestAssembleTagDeduplication(t *testing.T) {
	in := AssembleInput{
		Tags:        []string{"voice", "ops", "voice"},
		CapturedAt:  testTime,
		Transcriber: "text",
	}
	got := Assemble(in)
	// Count occurrences of "voice" in the tags line.
	tagsLine := ""
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "tags:") {
			tagsLine = line
			break
		}
	}
	count := strings.Count(tagsLine, `"voice"`)
	if count != 1 {
		t.Errorf("voice tag duplicated in tags line: %q", tagsLine)
	}
}

func TestAssembleKeepRawFalseOmitsDetails(t *testing.T) {
	in := AssembleInput{
		Title:             "T",
		Summary:           "S",
		Body:              "B",
		RawText:           "raw words",
		KeepRawTranscript: false,
		CapturedAt:        testTime,
		Transcriber:       "text",
	}
	got := Assemble(in)
	if strings.Contains(got, "<details>") {
		t.Error("details block present despite KeepRawTranscript=false")
	}
}

func TestAssembleMarkerCounts(t *testing.T) {
	in := AssembleInput{
		Markers:     []string{"@todo item1", "@decision go with A", "@question why"},
		CapturedAt:  testTime,
		Transcriber: "text",
	}
	got := Assemble(in)
	if !strings.Contains(got, "todos: 1") {
		t.Error("missing todos count")
	}
	if !strings.Contains(got, "decisions: 1") {
		t.Error("missing decisions count")
	}
	if !strings.Contains(got, "questions: 1") {
		t.Error("missing questions count")
	}
}
