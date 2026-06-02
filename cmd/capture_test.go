package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericmann/journal/internal/config"
)

func execLook(name string) (string, error) { return exec.LookPath(name) }

func gitInitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestCaptureWritesDailyBlock(t *testing.T) {
	root := t.TempDir()
	path, err := capture(root, ts(t, "2026-06-01 09:14"), "fallback broke on #cabot", []string{"litellm"}, "", "decision")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	// flag tag litellm + inline #cabot, flag marker decision.
	if !strings.Contains(got, "## 09:14 #litellm #cabot @decision\n") {
		t.Errorf("header wrong:\n%s", got)
	}
	if !strings.Contains(got, "fallback broke on #cabot\n") {
		t.Errorf("body missing:\n%s", got)
	}
	if !strings.HasSuffix(path, filepath.Join("daily", "2026", "06", "2026-06-01.md")) {
		t.Errorf("path = %s", path)
	}
}

func TestCaptureMergesAndDedupesTags(t *testing.T) {
	root := t.TempDir()
	// flag "#Cabot,litellm" plus inline #cabot should dedupe to cabot,litellm.
	path, err := capture(root, ts(t, "2026-06-01 09:14"), "note #cabot", []string{"#Cabot,litellm"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "## 09:14 #cabot #litellm\n") {
		t.Errorf("tags not merged/deduped:\n%s", data)
	}
}

func TestCaptureProjectWritesUnderProjects(t *testing.T) {
	root := t.TempDir()
	path, err := capture(root, ts(t, "2026-06-01 09:14"), "declared income", nil, "Canton COI", "decision")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, filepath.Join("projects", "canton-coi", "notes")) {
		t.Errorf("project path not slugified: %s", path)
	}
}

func TestCaptureRejectsEmptyText(t *testing.T) {
	if _, err := capture(t.TempDir(), time.Now(), "   ", nil, "", ""); err == nil {
		t.Error("expected error on empty text")
	}
}

func TestCaptureRejectsBadMarker(t *testing.T) {
	if _, err := capture(t.TempDir(), time.Now(), "x", nil, "", "idea"); err == nil {
		t.Error("expected error on invalid marker")
	}
}

func TestAutoCommitCaptureCommitsInGitRepo(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInitRepo(t, root)
	cfg := config.Default()
	cfg.GitAutocommit = true
	cfg.GitAutocommitSign = false

	// Capture a note, then auto-commit it.
	if _, err := capture(root, ts(t, "2026-06-01 12:53"), "a committed note", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	committed, err := autoCommitCapture(&cfg, root, ts(t, "2026-06-01 12:53"))
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("expected the capture to be committed")
	}
	if out := gitCmd(t, root, "log", "-1", "--pretty=%s"); !strings.Contains(out, "note") {
		t.Errorf("commit message lacks expected text: %q", out)
	}
	if files := gitCmd(t, root, "ls-files"); !strings.Contains(files, "daily/") {
		t.Errorf("note not committed: %s", files)
	}
}

func TestAutoCommitCaptureDisabledIsNoOp(t *testing.T) {
	if !haveGit(t) {
		t.Skip("git not available")
	}
	root := t.TempDir()
	gitInitRepo(t, root)
	cfg := config.Default()
	cfg.GitAutocommit = false // disabled
	if _, err := capture(root, ts(t, "2026-06-01 12:53"), "note", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	committed, err := autoCommitCapture(&cfg, root, ts(t, "2026-06-01 12:53"))
	if err != nil || committed {
		t.Errorf("disabled autocommit should be a no-op, got committed=%v err=%v", committed, err)
	}
}

func TestAutoCommitCaptureOutsideGitRepoIsNoOp(t *testing.T) {
	root := t.TempDir() // not a git repo
	cfg := config.Default()
	cfg.GitAutocommit = true
	committed, err := autoCommitCapture(&cfg, root, time.Now())
	if err != nil || committed {
		t.Errorf("outside a git repo should be a no-op, got committed=%v err=%v", committed, err)
	}
}

// resetComposeSeams restores the stdin/editor seams after a test tweaks them.
func resetComposeSeams() {
	stdinIsTerminal = defaultStdinIsTerminal
	stdinIsPiped = defaultStdinIsPiped
	openEditor = editorOpenDefault
	readStdin = readStdinDefault
}

func TestComposeNoteFromEditor(t *testing.T) {
	// Interactive terminal -> editor path. Inject a fake editor.
	stdinIsTerminal = func() bool { return true }
	openEditor = func(cmd string) (string, error) { return "note from the editor\n", nil }
	t.Cleanup(resetComposeSeams)

	cfg := config.Default()
	got, err := composeNote(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "note from the editor\n" {
		t.Errorf("composeNote = %q", got)
	}
}

func TestComposeNoteEmptyEditorAborts(t *testing.T) {
	stdinIsTerminal = func() bool { return true }
	openEditor = func(cmd string) (string, error) { return "   \n", nil }
	t.Cleanup(resetComposeSeams)

	cfg := config.Default()
	if _, err := composeNote(&cfg); err == nil {
		t.Error("expected an abort error on empty editor content")
	}
}

func TestComposeNoteFromPipedStdin(t *testing.T) {
	stdinIsTerminal = func() bool { return false }
	stdinIsPiped = func() bool { return true }
	readStdin = func() ([]byte, error) { return []byte("piped note #cabot\n"), nil }
	t.Cleanup(resetComposeSeams)

	cfg := config.Default()
	got, err := composeNote(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "piped note #cabot\n" {
		t.Errorf("composeNote(stdin) = %q", got)
	}
}

// Neither a terminal nor piped input (e.g. stdin is /dev/null) must be a clear
// error, not a hung read or an editor launched without a TTY.
func TestComposeNoteNonInteractiveNoInputErrors(t *testing.T) {
	stdinIsTerminal = func() bool { return false }
	stdinIsPiped = func() bool { return false }
	openEditor = func(cmd string) (string, error) {
		t.Fatal("editor must not be launched without a terminal")
		return "", nil
	}
	readStdin = func() ([]byte, error) {
		t.Fatal("stdin must not be read when there is no piped input")
		return nil, nil
	}
	t.Cleanup(resetComposeSeams)

	cfg := config.Default()
	if _, err := composeNote(&cfg); err == nil {
		t.Error("expected an error when stdin is neither a terminal nor piped")
	}
}

func haveGit(t *testing.T) bool {
	t.Helper()
	_, err := execLook("git")
	return err == nil
}

func TestCaptureIsFast(t *testing.T) {
	root := t.TempDir()
	start := time.Now()
	if _, err := capture(root, time.Now(), "quick note", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("capture took %v, want < 2s", d)
	}
}

func TestInitRepoCreatesSkeletonAndConfig(t *testing.T) {
	root := t.TempDir()
	res, err := initRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if !res.created {
		t.Error("expected created=true on fresh repo")
	}
	for _, p := range []string{
		filepath.Join(".journal", "config.yaml"),
		filepath.Join(".journal", "index"),
		filepath.Join(".journal", "sync.sh"),
		filepath.Join("docs", "VOICE_PROFILE.example.md"),
		"daily", "projects", "reflections", ".gitignore",
	} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if !strings.Contains(string(gi), ".journal/index/") {
		t.Errorf("gitignore missing index entry:\n%s", gi)
	}
	// The sync script is executable and the README is the fresh repo's top-level
	// README with the repo path templated into its cron examples.
	if fi, err := os.Stat(filepath.Join(root, ".journal", "sync.sh")); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("sync.sh is not executable: %v", fi.Mode())
	}
	if res.readmePath != filepath.Join(root, "README.md") {
		t.Errorf("expected top-level README on fresh repo, got %s", res.readmePath)
	}
	readme, _ := os.ReadFile(res.readmePath)
	if strings.Contains(string(readme), "{{ROOT}}") {
		t.Error("README still contains unreplaced {{ROOT}} placeholder")
	}
	if !strings.Contains(string(readme), root) {
		t.Error("README cron examples do not reference the repo path")
	}
}

func TestInitRepoUpgradePreservesExistingReadme(t *testing.T) {
	root := t.TempDir()
	// A pre-existing top-level README must never be clobbered; the guide then
	// lands in .journal/README.md instead.
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("my own readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := initRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.readmePath != filepath.Join(root, ".journal", "README.md") {
		t.Errorf("expected .journal/README.md fallback, got %s", res.readmePath)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "README.md")); string(data) != "my own readme\n" {
		t.Errorf("hand-written README was clobbered: %q", data)
	}
}

func TestInitRepoDoesNotClobberConfig(t *testing.T) {
	root := t.TempDir()
	if _, err := initRepo(root); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, ".journal", "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("embed_dim: 999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := initRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.created {
		t.Error("expected created=false when config already exists")
	}
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "999") {
		t.Errorf("existing config was clobbered:\n%s", data)
	}
}

func TestInitGitignoreNotDuplicated(t *testing.T) {
	root := t.TempDir()
	if _, err := initRepo(root); err != nil {
		t.Fatal(err)
	}
	if _, err := initRepo(root); err != nil {
		t.Fatal(err)
	}
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if n := strings.Count(string(gi), ".journal/index/"); n != 1 {
		t.Errorf(".journal/index/ appears %d times, want 1:\n%s", n, gi)
	}
}
