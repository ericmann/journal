package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/note"
)

const todoFixture = "# 2026-06-01\n\n" +
	"## 09:00 @todo\ncall bob about pricing\n\n" +
	"## 10:00 @todo\ndraft the janus SOW\n\n" +
	"## 11:00 @done 2026-06-01\nalready finished thing\n\n" +
	"## 12:00\njust a note, no marker\n"

func TestListTodosOpenAndDone(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{"daily/2026/06/2026-06-01.md": todoFixture})
	ctx := context.Background()

	open, err := listTodos(ctx, cfg, []string{note.MarkerTodo}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("open todos = %d, want 2", len(open))
	}
	done, err := listTodos(ctx, cfg, []string{note.MarkerDone}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || !strings.Contains(done[0].Snippet, "already finished") {
		t.Errorf("done todos = %+v, want the finished item", done)
	}
	all, err := listTodos(ctx, cfg, []string{note.MarkerTodo, note.MarkerDone}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("all = %d, want 3", len(all))
	}
}

func TestCompleteTodoByTextRewritesFile(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{"daily/2026/06/2026-06-01.md": todoFixture})
	ctx := context.Background()

	res, err := completeTodo(ctx, cfg, fake, "call bob", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Snippet, "call bob") {
		t.Errorf("completed result = %+v", res)
	}

	// The file now carries @done <date> where @todo was — content otherwise intact.
	data, _ := os.ReadFile(filepath.Join(cfg.Root(), "daily", "2026", "06", "2026-06-01.md"))
	got := string(data)
	if !strings.Contains(got, "## 09:00 @done 20") {
		t.Errorf("@todo not rewritten to dated @done:\n%s", got)
	}
	if !strings.Contains(got, "call bob about pricing") || !strings.Contains(got, "## 10:00 @todo") {
		t.Errorf("unrelated content disturbed:\n%s", got)
	}

	// The store reflects it immediately: one open, two done.
	open, _ := listTodos(ctx, cfg, []string{note.MarkerTodo}, "", "")
	done, _ := listTodos(ctx, cfg, []string{note.MarkerDone}, "", "")
	if len(open) != 1 || len(done) != 2 {
		t.Errorf("after done: open=%d done=%d, want 1/2", len(open), len(done))
	}
}

func TestCompleteTodoByCitation(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{"daily/2026/06/2026-06-01.md": todoFixture})
	open, _ := listTodos(context.Background(), cfg, []string{note.MarkerTodo}, "", "")
	if len(open) == 0 {
		t.Fatal("fixture has no open todos")
	}
	ref := open[0].Citation()
	res, err := completeTodo(context.Background(), cfg, fake, ref, "", nil)
	if err != nil {
		t.Fatalf("citation %q: %v", ref, err)
	}
	if res.Path != open[0].Path {
		t.Errorf("completed %s, want %s", res.Path, open[0].Path)
	}
}

func TestCompleteTodoAmbiguousAndMissing(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{"daily/2026/06/2026-06-01.md": todoFixture})
	ctx := context.Background()

	// "the" appears in both open todo bodies? "call bob about pricing" no 'the'…
	// Use a fragment in both: none. Both contain "o"? Use "a" — too broad. Craft:
	// "pricing" unique; "janus" unique; both contain " " — use " " (space).
	_, err := completeTodo(ctx, cfg, fake, "a", "", nil)
	if err == nil || !strings.Contains(err.Error(), "matches") {
		t.Errorf("ambiguous fragment should error with candidates, got %v", err)
	}
	_, err = completeTodo(ctx, cfg, fake, "no such todo text", "", nil)
	if err == nil || !strings.Contains(err.Error(), "no open todo") {
		t.Errorf("missing fragment should error, got %v", err)
	}
}

func TestCompleteTodoStaleIndex(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{"daily/2026/06/2026-06-01.md": todoFixture})
	// Hand-edit the file so the indexed @todo is gone, without re-indexing.
	abs := filepath.Join(cfg.Root(), "daily", "2026", "06", "2026-06-01.md")
	data, _ := os.ReadFile(abs)
	if err := os.WriteFile(abs, []byte(strings.ReplaceAll(string(data), "@todo", "@question")), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := completeTodo(context.Background(), cfg, fake, "call bob", "", nil)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Errorf("expected stale-index error, got %v", err)
	}
}

func TestCompleteTodoWithResolutionNote(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{"daily/2026/06/2026-06-01.md": todoFixture})
	ctx := context.Background()

	_, err := completeTodo(ctx, cfg, fake, "janus SOW", "sent draft to client", nil)
	if err != nil {
		t.Fatal(err)
	}

	abs := filepath.Join(cfg.Root(), "daily", "2026", "06", "2026-06-01.md")
	data, _ := os.ReadFile(abs)
	got := string(data)
	if !strings.Contains(got, "Resolution: sent draft to client") {
		t.Errorf("resolution line missing:\n%s", got)
	}
	// The other blocks must remain untouched.
	if !strings.Contains(got, "## 09:00 @todo") {
		t.Errorf("unrelated todo disturbed:\n%s", got)
	}
}

func TestCompleteTodoResolutionWhitespaceIsStripped(t *testing.T) {
	cfg, fake := indexedRepo(t, map[string]string{"daily/2026/06/2026-06-01.md": todoFixture})
	ctx := context.Background()

	_, err := completeTodo(ctx, cfg, fake, "call bob", "  trimmed resolution  ", nil)
	if err != nil {
		t.Fatal(err)
	}

	abs := filepath.Join(cfg.Root(), "daily", "2026", "06", "2026-06-01.md")
	data, _ := os.ReadFile(abs)
	if !strings.Contains(string(data), "Resolution: trimmed resolution") {
		t.Errorf("resolution not trimmed:\n%s", string(data))
	}
}

func TestMCPTodosAndDone(t *testing.T) {
	cfg, _ := indexedRepo(t, map[string]string{"daily/2026/06/2026-06-01.md": todoFixture})
	ctx := context.Background()

	out, err := mcpTodos(ctx, cfg, todosInput{})
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid todos JSON: %v\n%s", err, out)
	}
	if len(env.Results) != 2 {
		t.Fatalf("mcp todos = %d, want 2", len(env.Results))
	}

	dout, err := mcpDone(ctx, cfg, embed.NewFake(cfg.EmbedDim), doneInput{Ref: "janus SOW", Resolution: ""})
	if err != nil {
		t.Fatal(err)
	}
	var denv struct {
		Completed map[string]any `json:"completed"`
	}
	if err := json.Unmarshal([]byte(dout), &denv); err != nil {
		t.Fatalf("invalid done JSON: %v\n%s", err, dout)
	}
	if denv.Completed["path"] == "" {
		t.Errorf("done result missing path: %s", dout)
	}
	// Now only one open todo remains via MCP.
	out2, _ := mcpTodos(ctx, cfg, todosInput{})
	var env2 struct {
		Results []map[string]any `json:"results"`
	}
	_ = json.Unmarshal([]byte(out2), &env2)
	if len(env2.Results) != 1 {
		t.Errorf("after mcp done, open todos = %d, want 1", len(env2.Results))
	}
}
