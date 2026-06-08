package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStartFlagEnvCwd(t *testing.T) {
	t.Cleanup(func() { journalDir = "" })

	// Default: current directory.
	journalDir = ""
	t.Setenv(JournalDirEnv, "")
	if got := resolveStart(); got != "." {
		t.Errorf("default resolveStart = %q, want .", got)
	}

	// $JOURNAL_DIR is honored when the flag is unset.
	t.Setenv(JournalDirEnv, "/tmp/from-env")
	if got := resolveStart(); got != "/tmp/from-env" {
		t.Errorf("env resolveStart = %q", got)
	}

	// The flag wins over the env.
	journalDir = "/tmp/from-flag"
	if got := resolveStart(); got != "/tmp/from-flag" {
		t.Errorf("flag should override env, got %q", got)
	}

	// ~ expands.
	home, _ := os.UserHomeDir()
	journalDir = "~/Projects/devnotes"
	if got := resolveStart(); got != filepath.Join(home, "Projects", "devnotes") {
		t.Errorf("tilde not expanded: %q", got)
	}
}

func TestLoadConfigHonorsJournalDir(t *testing.T) {
	// A journal repo somewhere other than the test's working directory.
	cfg := testRepo(t, nil)
	repo := cfg.Root()

	t.Cleanup(func() { journalDir = "" })
	journalDir = repo
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Root() != repo {
		t.Errorf("loadConfig root = %q, want %q (from --journal-dir)", got.Root(), repo)
	}

	// A subdirectory of the repo also resolves up to the root.
	journalDir = filepath.Join(repo, "daily")
	got, err = loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.Root() != repo {
		t.Errorf("subdir resolveRoot = %q, want %q", got.Root(), repo)
	}
}
