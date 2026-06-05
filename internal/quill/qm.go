package quill

import (
	"encoding/json"
	"fmt"
	"strings"
)

// QMMarker is the literal first line of a Quill manual-export (.qm) file.
const QMMarker = "QMv2"

// IsQM reports whether content looks like a Quill .qm export.
func IsQM(content string) bool {
	return strings.HasPrefix(content, QMMarker)
}

// ParseQM parses a manually-exported .qm file (first line "QMv2", remainder
// JSON) into a Meeting. The JSON shape is app-internal and unverified, so fields
// are extracted defensively (mirroring the DB reader); missing fields degrade to
// empty rather than failing.
func ParseQM(content string) (Meeting, error) {
	if !IsQM(content) {
		return Meeting{}, fmt.Errorf("not a .qm file (missing %q marker)", QMMarker)
	}
	body := strings.TrimSpace(strings.TrimPrefix(content, QMMarker))
	var obj map[string]any
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		return Meeting{}, fmt.Errorf("parsing .qm JSON: %w", err)
	}

	m := Meeting{
		ID:    firstField(obj, "id", "meeting_id", "uuid"),
		Title: firstNonEmpty(firstField(obj, "manualTitle", "llmTitle", "eventTitle", "title", "name"), "Untitled meeting"),
		Type:  firstField(obj, "type"),
		Start: parseQuillTime(firstField(obj, "start", "startTime", "date")),
		End:   parseQuillTime(firstField(obj, "end", "endTime")),
		Notes: firstField(obj, "notes", "summary", "body"),
	}
	if raw, ok := obj["participants"]; ok {
		m.Participants = parseStringList(rawJSON(raw))
	}
	if raw, ok := obj["tags"]; ok {
		m.Tags = parseStringList(rawJSON(raw))
	}
	for _, key := range []string{"audio_transcript", "transcript", "segments"} {
		if raw, ok := obj[key]; ok {
			if segs := parseSegments(rawJSON(raw)); len(segs) > 0 {
				m.Transcript = segs
				break
			}
		}
	}
	return m, nil
}

// rawJSON re-marshals a decoded value so the shared string/segment parsers can
// re-interpret it (they accept JSON text).
func rawJSON(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
