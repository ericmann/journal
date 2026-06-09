package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ericmann/journal/internal/embed"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func step(t *testing.T, m tea.Model, msg tea.Msg) (tuiModel, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(msg)
	tm, ok := nm.(tuiModel)
	if !ok {
		t.Fatalf("Update returned %T, want tuiModel", nm)
	}
	return tm, cmd
}

// tuiFixture builds a sized model over an indexed temp repo with one open todo.
func tuiFixture(t *testing.T) (tuiModel, *embed.Fake) {
	t.Helper()
	cfg, fake := indexedRepo(t, map[string]string{
		"daily/2026/06/2026-06-01.md": "# 2026-06-01\n\n## 09:00 @todo\nfinish the tui\n\n## 10:00 #cabot\nrouting note\n",
	})
	m := newTUIModel(cfg, fake)
	sized, _ := step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	// Load data synchronously by invoking the Init command.
	loaded, _ := step(t, sized, sized.loadAll()())
	return loaded, fake
}

func TestTUILoadsAndRendersTabs(t *testing.T) {
	m, _ := tuiFixture(t)
	if !m.ready {
		t.Fatal("model not ready after WindowSizeMsg")
	}
	if !strings.Contains(m.status, "open todos") {
		t.Errorf("status after load = %q", m.status)
	}
	// Every tab renders something.
	for i := 0; i < int(tabCount); i++ {
		m.tab = tuiTab(i)
		if v := m.View(); strings.TrimSpace(v) == "" {
			t.Errorf("tab %s renders empty", tabNames[i])
		}
	}
	// Tab key cycles.
	m.tab = tabToday
	m2, _ := step(t, m, key("tab"))
	if m2.tab != tabTodos {
		t.Errorf("tab key moved to %d, want Todos", m2.tab)
	}
	// Number key jumps.
	m3, _ := step(t, m, key("6"))
	if m3.tab != tabStats {
		t.Errorf("'6' moved to %d, want Stats", m3.tab)
	}
}

func TestTUIDoneKeyCompletesSelectedTodo(t *testing.T) {
	m, _ := tuiFixture(t)
	m.tab = tabTodos
	if len(m.todos.Items()) != 1 {
		t.Fatalf("todos list = %d items, want 1", len(m.todos.Items()))
	}
	m2, cmd := step(t, m, key("d"))
	if cmd == nil {
		t.Fatal("'d' should produce a completion command")
	}
	msg := cmd() // run the async completion
	dm, ok := msg.(doneMsg)
	if !ok {
		t.Fatalf("got %T, want doneMsg", msg)
	}
	if dm.err != nil {
		t.Fatalf("completion failed: %v", dm.err)
	}
	// The file is rewritten on disk.
	data, _ := os.ReadFile(filepath.Join(m2.cfg.Root(), "daily", "2026", "06", "2026-06-01.md"))
	if !strings.Contains(string(data), "@done 20") {
		t.Errorf("todo not completed on disk:\n%s", data)
	}
	// Feeding the msg back updates status and triggers a reload.
	m3, reload := step(t, m2, msg)
	if !strings.Contains(m3.status, "done ✓") {
		t.Errorf("status = %q", m3.status)
	}
	if reload == nil {
		t.Error("done should trigger a data reload")
	}
}

func TestTUISearchFlow(t *testing.T) {
	m, _ := tuiFixture(t)
	// Jump to search: input gets focus.
	m2, _ := step(t, m, key("3"))
	if m2.tab != tabSearch || !m2.typing {
		t.Fatalf("search tab should focus input (tab=%d typing=%v)", m2.tab, m2.typing)
	}
	// Run the search command directly (typing simulation is the input's concern).
	msg := m2.searchCmd("routing note")()
	sm, ok := msg.(searchMsg)
	if !ok || sm.err != nil {
		t.Fatalf("search msg = %T err=%v", msg, sm.err)
	}
	m3, _ := step(t, m2, msg)
	if len(m3.results.Items()) == 0 {
		t.Error("search results list empty")
	}
	if m3.typing {
		t.Error("results should blur the input")
	}
}

func TestTUIDetailOpenAndClose(t *testing.T) {
	m, _ := tuiFixture(t)
	msg := m.openCmd("daily/2026/06/2026-06-01.md")()
	dm, ok := msg.(detailMsg)
	if !ok || dm.err != nil {
		t.Fatalf("detail msg = %T err=%v", msg, dm.err)
	}
	m2, _ := step(t, m, msg)
	if !m2.inDetail {
		t.Fatal("detail view should be open")
	}
	if v := m2.View(); !strings.Contains(v, "esc to go back") && strings.TrimSpace(v) == "" {
		t.Error("detail view renders empty")
	}
	m3, _ := step(t, m2, key("esc"))
	if m3.inDetail {
		t.Error("esc should close the detail view")
	}
}

func TestTUISearchErrorIsNonFatal(t *testing.T) {
	m, _ := tuiFixture(t)
	m2, _ := step(t, m, searchMsg{err: os.ErrDeadlineExceeded})
	if !strings.Contains(m2.status, "search error") {
		t.Errorf("status = %q, want search error surfaced", m2.status)
	}
}
