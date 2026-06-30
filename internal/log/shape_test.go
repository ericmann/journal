package log

import (
	"context"
	"errors"
	"testing"

	"github.com/ericmann/journal/internal/synth"
)

func TestShape(t *testing.T) {
	validJSON := `{"title":"Deploy Review","summary":"Checked deploy logs.","body":"Reviewed the deploy logs and found an anomaly.","tags":["deploy","ops"],"markers":["@todo review the deploy logs"]}`

	tests := []struct {
		name       string
		reply      string
		forcedErr  error
		localOnly  bool
		wantShaped bool
		wantErr    bool
		wantTitle  string
	}{
		{
			name:       "normal shaping",
			reply:      validJSON,
			wantShaped: true,
			wantTitle:  "Deploy Review",
		},
		{
			name:       "local_only skips shaping",
			reply:      validJSON,
			localOnly:  true,
			wantShaped: false,
		},
		{
			name:       "LLM error falls through to raw land",
			forcedErr:  errors.New("network error"),
			wantShaped: false,
			wantErr:    true,
		},
		{
			name:       "JSON parse failure falls through to raw land",
			reply:      "not json at all",
			wantShaped: false,
		},
		{
			name:       "JSON with markdown fences is stripped",
			reply:      "```json\n" + validJSON + "\n```",
			wantShaped: true,
			wantTitle:  "Deploy Review",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &synth.Fake{Reply: tc.reply, ForcedErr: tc.forcedErr}
			result, shaped, err := Shape(context.Background(), fake, "model", 1024,
				"reviewed deploy logs todo review the deploy logs", "", tc.localOnly)

			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if shaped != tc.wantShaped {
				t.Errorf("shaped = %v, want %v", shaped, tc.wantShaped)
			}
			if tc.wantTitle != "" && result.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", result.Title, tc.wantTitle)
			}
			if tc.localOnly && fake.CallCount != 0 {
				t.Errorf("local_only should not call LLM, got %d calls", fake.CallCount)
			}
		})
	}
}

func TestShapeValidatesMarkers(t *testing.T) {
	reply := `{"title":"T","summary":"S","body":"B","tags":[],"markers":["@todo fix this","@invalid marker","@decision go with option A","not a marker"]}`
	fake := &synth.Fake{Reply: reply}
	result, shaped, err := Shape(context.Background(), fake, "m", 512, "text", "", false)
	if err != nil || !shaped {
		t.Fatalf("Shape() shaped=%v err=%v", shaped, err)
	}
	if len(result.Markers) != 2 {
		t.Errorf("markers = %v, want 2 valid entries", result.Markers)
	}
	for _, m := range result.Markers {
		if m != "@todo fix this" && m != "@decision go with option A" {
			t.Errorf("unexpected marker %q", m)
		}
	}
}

func TestShapeNilClientFallsThrough(t *testing.T) {
	_, shaped, err := Shape(context.Background(), nil, "m", 512, "text", "", false)
	if err != nil {
		t.Errorf("nil client: unexpected error: %v", err)
	}
	if shaped {
		t.Error("nil client: expected shaped=false")
	}
}
