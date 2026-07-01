// Package log implements the voice-note capture pipeline: shape → assemble →
// land → index. It is pure over data (no mic, no audio I/O) so the full
// pipeline is testable with typed text. Callers that also import the stdlib
// "log" package should alias this one: jlog "github.com/ericmann/journal/internal/log".
package log

import "time"

// ShapeResult is the structured output from an LLM shaping call.
type ShapeResult struct {
	Title   string
	Summary string
	Body    string
	Tags    []string
	Markers []string // validated @marker lines, e.g. "@todo review deploy logs"
}

// AssembleInput carries all data needed to render a voice note document.
type AssembleInput struct {
	Title             string
	Summary           string
	Body              string
	RawText           string
	Tags              []string
	Markers           []string
	DurationSec       int    // 0 for --text (no recording)
	Transcriber       string // "text" for the --text path
	KeepRawTranscript bool
	CapturedAt        time.Time
	// AudioPath, when non-empty, is recorded as the `audio:` frontmatter
	// field — set when a recorded WAV is retained (log.audio.keep_wav).
	AudioPath string
}
