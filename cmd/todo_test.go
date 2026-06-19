package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodoWritesTodoBlock(t *testing.T) {
	root := t.TempDir()
	path, err := capture(root, ts(t, "2026-06-15 10:00"), "call bob about pricing", nil, "", "todo")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "@todo") {
		t.Errorf("block missing @todo marker:\n%s", got)
	}
	if !strings.Contains(got, "call bob about pricing") {
		t.Errorf("block missing action text:\n%s", got)
	}
}

func TestTodoWithProjectRoutesToProjectNotes(t *testing.T) {
	root := t.TempDir()
	path, err := capture(root, ts(t, "2026-06-15 10:00"), "review PR", nil, "myproject", "todo")
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, "projects/myproject/") {
		t.Errorf("project path = %q, want projects/myproject/ prefix", rel)
	}
}

func TestTodoEmptyActionErrors(t *testing.T) {
	root := t.TempDir()
	_, err := capture(root, ts(t, "2026-06-15 10:00"), "", nil, "", "todo")
	if err == nil {
		t.Error("expected error for empty action text")
	}
}

func TestTodoMultiWordAction(t *testing.T) {
	root := t.TempDir()
	body := strings.Join([]string{"call", "bob", "about", "pricing"}, " ")
	path, err := capture(root, ts(t, "2026-06-15 10:00"), body, nil, "", "todo")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "call bob about pricing") {
		t.Errorf("multi-word action not in file:\n%s", string(data))
	}
}
