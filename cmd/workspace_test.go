package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/store"
)

// TestWorkspaceIsolation proves the clone-to-second-workspace pattern: two
// independent journal repos index and search with their own gitignored .db and
// never contaminate each other.
func TestWorkspaceIsolation(t *testing.T) {
	ctx := context.Background()

	// Two separate repos, as a clone-to-Displace would produce.
	personal := testRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00 #personal\nrenewed the domain registration\n",
	})
	displace := testRepo(t, map[string]string{
		"daily/2026/06/d.md": "# 2026-06-01\n\n## 09:00 #displace\nset up the canton conflict tracking\n",
	})

	// Each indexes with its own embedder (separate Ollama call paths in reality).
	if _, err := runIndex(ctx, personal, embed.NewFake(personal.EmbedDim), indexOptions{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := runIndex(ctx, displace, embed.NewFake(displace.EmbedDim), indexOptions{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	// The two databases live under each repo's own .journal/index and differ.
	if personal.StoreAbsPath() == displace.StoreAbsPath() {
		t.Fatal("the two repos share a store path")
	}
	for _, c := range []*config.Config{personal, displace} {
		if !strings.Contains(c.StoreAbsPath(), filepath.Join(".journal", "index")) {
			t.Errorf("store not under .journal/index: %s", c.StoreAbsPath())
		}
		if _, err := os.Stat(c.StoreAbsPath()); err != nil {
			t.Errorf("store not created for repo %s: %v", c.Root(), err)
		}
	}

	// Each repo's .gitignore ignores its own index.
	for _, c := range []*config.Config{personal, displace} {
		gi, _ := os.ReadFile(filepath.Join(c.Root(), ".gitignore"))
		if !strings.Contains(string(gi), ".journal/index/") {
			t.Errorf("repo %s does not gitignore its index", c.Root())
		}
	}

	// Searching one repo returns only that repo's notes — no cross-contamination.
	pres, err := runSearch(ctx, personal, embed.NewFake(personal.EmbedDim), "canton conflict tracking", 5, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range pres {
		if strings.Contains(r.Snippet, "canton") {
			t.Errorf("personal workspace leaked a displace note: %q", r.Snippet)
		}
	}

	dres, err := runSearch(ctx, displace, embed.NewFake(displace.EmbedDim), "canton conflict tracking", 5, store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	foundCanton := false
	for _, r := range dres {
		if strings.Contains(r.Snippet, "canton") {
			foundCanton = true
		}
		if strings.Contains(r.Snippet, "domain registration") {
			t.Errorf("displace workspace leaked a personal note: %q", r.Snippet)
		}
	}
	if !foundCanton {
		t.Error("displace workspace did not find its own canton note")
	}
}

// TestWorkspaceSeparateSecrets confirms the API key is per-environment, not
// per-repo: it is read from the env at invocation and never stored in config.
func TestWorkspaceSeparateSecrets(t *testing.T) {
	cfg := testRepo(t, nil)
	out, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	// synth_provider: anthropic is a legitimate (non-secret) value, so look for
	// actual secret shapes: a key-bearing field name or a token prefix.
	if strings.Contains(strings.ToLower(string(out)), "api_key") ||
		strings.Contains(string(out), "sk-") {
		t.Errorf("config.yaml must not contain any secret:\n%s", out)
	}

	t.Setenv("ANTHROPIC_API_KEY", "sk-personal-token")
	if k, _ := config.AnthropicAPIKey(); k != "sk-personal-token" {
		t.Errorf("key = %q, want the env value", k)
	}
	// Swapping the env (as a second workspace would) swaps the effective token.
	t.Setenv("ANTHROPIC_API_KEY", "sk-work-token")
	if k, _ := config.AnthropicAPIKey(); k != "sk-work-token" {
		t.Errorf("key = %q, want the swapped env value", k)
	}
}
