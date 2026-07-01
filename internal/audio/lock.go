// Package audio provides the process-lifecycle primitives for `journal log`'s
// recording toggle: a lockfile shared between the "start" and "stop" presses,
// and an injectable Recorder boundary so the mic/ffmpeg dependency never runs
// in tests.
package audio

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// LockState is the JSON payload of the lockfile that makes `journal log`
// stateless between invocations: the first press writes it, the second press
// reads it to decide whether to signal a live recorder or recover a stale one.
type LockState struct {
	PID       int       `json:"pid"`
	WAVPath   string    `json:"wav_path"`
	StartedAt time.Time `json:"started_at"`
}

// PIDAlive reports whether pid names a live, signalable process. Injectable
// for tests so lock-lifecycle logic never depends on real process state.
var PIDAlive = defaultPIDAlive

// defaultPIDAlive probes liveness via signal 0, which the OS delivers to no
// one — it only reports whether the target process exists and is
// signalable, without affecting it.
func defaultPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// LockPath returns the lockfile location: $XDG_RUNTIME_DIR/journal-log.lock
// when set, else <tmp>/journal-log/journal-log.lock.
func LockPath() string {
	if dir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); dir != "" {
		return filepath.Join(dir, "journal-log.lock")
	}
	return filepath.Join(os.TempDir(), "journal-log", "journal-log.lock")
}

// EnsureLockDir creates the lockfile's parent directory if it doesn't exist.
func EnsureLockDir() error {
	return os.MkdirAll(filepath.Dir(LockPath()), 0o755)
}

// ReadLock reads and parses the lockfile. When no recording is active the
// returned error wraps os.ErrNotExist (check with errors.Is).
func ReadLock() (*LockState, error) {
	data, err := os.ReadFile(LockPath())
	if err != nil {
		return nil, err
	}
	var st LockState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parsing lockfile: %w", err)
	}
	return &st, nil
}

// WriteLock atomically writes state to the lockfile (write to a sibling temp
// file, then rename into place) so a concurrent reader never observes a
// partial write.
func WriteLock(state LockState) error {
	if err := EnsureLockDir(); err != nil {
		return fmt.Errorf("creating lock dir: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encoding lockfile: %w", err)
	}
	path := LockPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing lockfile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("finalizing lockfile: %w", err)
	}
	return nil
}

// RemoveLock deletes the lockfile. A missing lockfile is not an error.
func RemoveLock() error {
	err := os.Remove(LockPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
