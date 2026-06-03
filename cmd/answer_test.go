package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/synth"
)

func TestAnswerQueryUsesClientAndGroundsInChunks(t *testing.T) {
	fake := &synth.Fake{Reply: "**Janus** is a hot lead. [projects/janus/notes/2026-06-02.md:3-10]"}
	chunks := []store.Chunk{
		{Path: "projects/janus/notes/2026-06-02.md", LineStart: 3, LineEnd: 10, Body: "Hot lead in Janus Henderson."},
	}
	got, err := answerQuery(context.Background(), fake, "claude-test", 2048, "What about Janus?", chunks)
	if err != nil {
		t.Fatal(err)
	}
	if got != fake.Reply {
		t.Errorf("answer = %q, want the client reply", got)
	}
	if fake.CallCount != 1 {
		t.Errorf("client called %d times, want 1", fake.CallCount)
	}
	// The prompt must carry the question and the chunk citation for grounding.
	if !strings.Contains(fake.LastReq.Prompt, "What about Janus?") ||
		!strings.Contains(fake.LastReq.Prompt, "projects/janus/notes/2026-06-02.md:3-10") {
		t.Errorf("prompt missing question or citation:\n%s", fake.LastReq.Prompt)
	}
	if fake.LastReq.Model != "claude-test" || fake.LastReq.MaxTokens != 2048 {
		t.Errorf("model/max_tokens not threaded through: %+v", fake.LastReq)
	}
}

func TestWantAnswer(t *testing.T) {
	cases := []struct {
		name                             string
		jsonMode, forceOn, forceOff, key bool
		wantDo, wantMissing              bool
	}{
		{"auto with key", false, false, false, true, true, false},
		{"auto no key (skip)", false, false, false, false, false, false},
		{"force on no key (error)", false, true, false, false, false, true},
		{"force on with key", false, true, false, true, true, false},
		{"no-answer wins", false, false, true, true, false, false},
		{"json never answers", true, true, false, true, false, false},
	}
	for _, c := range cases {
		do, missing := wantAnswer(c.jsonMode, c.forceOn, c.forceOff, c.key)
		if do != c.wantDo || missing != c.wantMissing {
			t.Errorf("%s: got (do=%v, missing=%v), want (do=%v, missing=%v)",
				c.name, do, missing, c.wantDo, c.wantMissing)
		}
	}
}

func TestRenderMarkdownPlainWhenNotTTY(t *testing.T) {
	var buf bytes.Buffer // not an *os.File → plain markdown, no ANSI
	renderMarkdown(&buf, "# Title\n\n**bold** and a point.\n")
	got := buf.String()
	if !strings.Contains(got, "# Title") || !strings.Contains(got, "**bold**") {
		t.Errorf("expected raw markdown when not a TTY, got:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("did not expect ANSI escapes when not a TTY:\n%q", got)
	}
}
