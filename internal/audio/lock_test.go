package audio

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadLockRoundTrip(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	want := LockState{PID: 4242, WAVPath: "/tmp/journal-log/note.wav", StartedAt: time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)}
	if err := WriteLock(want); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLock()
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != want.PID || got.WAVPath != want.WAVPath || !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("ReadLock() = %+v, want %+v", got, want)
	}
}

func TestWriteLockLeavesNoTempFile(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	if err := WriteLock(LockState{PID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(LockPath() + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected no leftover .tmp file, stat err = %v", err)
	}
}

func TestRemoveLockThenReadReportsNotExist(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	if err := WriteLock(LockState{PID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLock(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLock(); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadLock() after RemoveLock() err = %v, want os.ErrNotExist", err)
	}
	// Removing an already-absent lock is not an error.
	if err := RemoveLock(); err != nil {
		t.Errorf("RemoveLock() on missing lock should be a no-op, got: %v", err)
	}
}

func TestEnsureLockDirIdempotent(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	if err := EnsureLockDir(); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLockDir(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(LockPath()))
	if err != nil || !info.IsDir() {
		t.Errorf("lock dir not created: %v", err)
	}
}

func TestLockPathPrefersXDGRuntimeDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	if got, want := LockPath(), filepath.Join(dir, "journal-log.lock"); got != want {
		t.Errorf("LockPath() = %q, want %q", got, want)
	}
}

func TestPIDAliveReportsDeadProcess(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("could not run a throwaway process: %v", err)
	}
	if PIDAlive(cmd.Process.Pid) {
		t.Errorf("PIDAlive(%d) = true for an already-exited process", cmd.Process.Pid)
	}
}

func TestPIDAliveReportsLiveProcess(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Error("PIDAlive(os.Getpid()) = false, want true")
	}
}

func TestPIDAliveRejectsNonPositive(t *testing.T) {
	if PIDAlive(0) || PIDAlive(-1) {
		t.Error("PIDAlive should reject non-positive pids")
	}
}
