package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecideWritesDecisionBlock(t *testing.T) {
	root := t.TempDir()
	path, err := capture(root, ts(t, "2026-06-15 10:00"), "use sqlite-vec for vectors", nil, "", "decision")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "@decision") {
		t.Errorf("block missing @decision marker:\n%s", got)
	}
	if !strings.Contains(got, "use sqlite-vec for vectors") {
		t.Errorf("block missing statement:\n%s", got)
	}
}

func TestDecideWithRationaleAppendsRationaleLine(t *testing.T) {
	root := t.TempDir()
	body := "use sqlite-vec for vectors\nRationale: Pure-Go, no cgo needed"
	path, err := capture(root, ts(t, "2026-06-15 10:00"), body, nil, "", "decision")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "Rationale: Pure-Go, no cgo needed") {
		t.Errorf("rationale line missing:\n%s", got)
	}
}

func TestDecideWithProjectRoutesToProjectNotes(t *testing.T) {
	root := t.TempDir()
	path, err := capture(root, ts(t, "2026-06-15 10:00"), "use postgres", nil, "myproject", "decision")
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, "projects/myproject/") {
		t.Errorf("project path = %q, want projects/myproject/ prefix", rel)
	}
}

func TestDecideEmptyStatementErrors(t *testing.T) {
	root := t.TempDir()
	_, err := capture(root, ts(t, "2026-06-15 10:00"), "", nil, "", "decision")
	if err == nil {
		t.Error("expected error for empty statement")
	}
}

func TestDecideCommandFlagCombinations(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		rationale string
		wantBody  string
		wantErr   bool
	}{
		{
			name:     "statement only",
			args:     []string{"use sqlite-vec"},
			wantBody: "use sqlite-vec",
		},
		{
			name:      "statement with rationale",
			args:      []string{"use sqlite-vec"},
			rationale: "Pure-Go",
			wantBody:  "use sqlite-vec\nRationale: Pure-Go",
		},
		{
			name:     "multi-word statement",
			args:     []string{"use", "sqlite-vec", "for", "vectors"},
			wantBody: "use sqlite-vec for vectors",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Join(tc.args, " ")
			if tc.rationale != "" {
				body += "\nRationale: " + tc.rationale
			}
			root := t.TempDir()
			path, err := capture(root, ts(t, "2026-06-15 10:00"), body, nil, "", "decision")
			if tc.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(path)
			if !strings.Contains(string(data), tc.wantBody) {
				t.Errorf("body %q not found in:\n%s", tc.wantBody, string(data))
			}
		})
	}
}
