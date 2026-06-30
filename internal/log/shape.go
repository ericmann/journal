package log

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/synth"
)

// shapeResponse is the JSON schema the LLM must return.
type shapeResponse struct {
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Body    string   `json:"body"`
	Tags    []string `json:"tags"`
	Markers []string `json:"markers"`
}

// shapePrompt builds the LLM prompt for voice-note shaping.
func shapePrompt(rawText, voiceProfile string) string {
	var b strings.Builder
	b.WriteString("You are a developer journal assistant. Clean and structure the following voice note.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Correct disfluencies, filler words, and punctuation. Never rewrite meaning.\n")
	b.WriteString("- Return a concise title (≤10 words).\n")
	b.WriteString("- Return a 1–2 sentence summary.\n")
	b.WriteString("- Return the cleaned body as prose.\n")
	b.WriteString("- Return relevant lowercase tags (no # prefix).\n")
	b.WriteString("- Surface any spoken @todo, @decision, or @question cues as full inline lines.\n")
	b.WriteString("- Respond ONLY with valid JSON matching this schema (no markdown fences):\n")
	b.WriteString(`{"title":"...","summary":"...","body":"...","tags":["..."],"markers":["@todo ..."]}`)
	b.WriteString("\n\n")
	if strings.TrimSpace(voiceProfile) != "" {
		b.WriteString("Author voice profile (for tone guidance only — do not change meaning):\n")
		b.WriteString(strings.TrimSpace(voiceProfile))
		b.WriteString("\n\n")
	}
	b.WriteString("Voice note:\n\n")
	b.WriteString(rawText)
	return b.String()
}

// Shape calls the synthesis client to clean, title, summarize, tag, and
// extract markers from rawText. If localOnly is true the call is skipped and
// shaped=false is returned (caller falls back to raw land). A parse or network
// error also returns shaped=false so a note is always written.
func Shape(ctx context.Context, client synth.Client, model string, maxTokens int,
	rawText, voiceProfile string, localOnly bool) (ShapeResult, bool, error) {

	if localOnly {
		return ShapeResult{}, false, nil
	}
	if client == nil {
		return ShapeResult{}, false, nil
	}

	prompt := shapePrompt(rawText, voiceProfile)
	resp, err := client.Complete(ctx, synth.Request{
		Model:     model,
		MaxTokens: maxTokens,
		Prompt:    prompt,
	})
	if err != nil {
		return ShapeResult{}, false, fmt.Errorf("shaping voice note: %w", err)
	}

	text := strings.TrimSpace(resp.Text)
	// Strip optional markdown code fences the model may add.
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text, "\n"); idx >= 0 {
			text = text[idx+1:]
		}
		text = strings.TrimSuffix(strings.TrimRight(text, "\n "), "```")
		text = strings.TrimSpace(text)
	}

	var sr shapeResponse
	if err := json.Unmarshal([]byte(text), &sr); err != nil {
		// JSON parse failure → fall through to raw land (not an error).
		return ShapeResult{}, false, nil
	}

	// Validate markers: only keep entries that contain a known @marker keyword.
	var validMarkers []string
	for _, m := range sr.Markers {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		// Extract the keyword after the leading "@".
		parts := strings.Fields(m)
		if len(parts) == 0 {
			continue
		}
		kw := strings.TrimPrefix(parts[0], "@")
		if note.ValidMarker(kw) {
			validMarkers = append(validMarkers, m)
		}
	}

	return ShapeResult{
		Title:   strings.TrimSpace(sr.Title),
		Summary: strings.TrimSpace(sr.Summary),
		Body:    strings.TrimSpace(sr.Body),
		Tags:    sr.Tags,
		Markers: validMarkers,
	}, true, nil
}
