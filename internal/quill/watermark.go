package quill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// watermarkFile lives under .journal/ and records how far quill-sync has synced,
// so each run only emits new meetings.
const watermarkFile = "quill-sync.json"

type watermark struct {
	LastStart time.Time `json:"last_start"`
}

// WatermarkPath is the watermark file path under the repo's .journal directory.
func WatermarkPath(journalDir string) string {
	return filepath.Join(journalDir, watermarkFile)
}

// LoadWatermark returns the last-synced meeting start (zero if none yet).
func LoadWatermark(journalDir string) (time.Time, error) {
	data, err := os.ReadFile(WatermarkPath(journalDir))
	if os.IsNotExist(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	var w watermark
	if err := json.Unmarshal(data, &w); err != nil {
		return time.Time{}, err // corrupt: caller may choose to re-sync from zero
	}
	return w.LastStart, nil
}

// SaveWatermark persists the last-synced meeting start.
func SaveWatermark(journalDir string, lastStart time.Time) error {
	data, err := json.MarshalIndent(watermark{LastStart: lastStart.UTC()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(WatermarkPath(journalDir), data, 0o644)
}
