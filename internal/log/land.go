package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/note"
)

// Filename returns the landing filename for a voice note:
// YYYY-MM-DD-HHMM-<slug>.md. When title is empty, the first 8 words of
// fallback are used to generate the slug.
func Filename(t time.Time, title, fallback string) string {
	slug := note.Slugify(title)
	if slug == "" {
		words := strings.Fields(fallback)
		if len(words) > 8 {
			words = words[:8]
		}
		slug = note.Slugify(strings.Join(words, " "))
	}
	if slug == "" {
		slug = "voice"
	}
	return fmt.Sprintf("%s-%s.md", t.UTC().Format("2006-01-02-1504"), slug)
}

// Land writes content to absDir/filename, creating the directory if needed.
// Returns the absolute path of the written file.
func Land(absDir, filename string, content []byte) (string, error) {
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("creating voice note directory: %w", err)
	}
	abs := filepath.Join(absDir, filename)
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		return "", fmt.Errorf("writing voice note: %w", err)
	}
	return abs, nil
}

// AppendBacklink appends a one-line daily-backlink breadcrumb to dailyAbsPath.
// The line format: "- [Voice note](<relVoicePath>) — <HH:MM>"
// It is a no-op (nil error) when dailyAbsPath is empty or cannot be opened.
func AppendBacklink(dailyAbsPath, relVoicePath string, t time.Time) error {
	if dailyAbsPath == "" {
		return nil
	}
	line := fmt.Sprintf("\n- [Voice note](%s) — %s\n",
		relVoicePath, t.UTC().Format("15:04"))
	f, err := os.OpenFile(dailyAbsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		// Missing daily file is a soft failure — note is already landed.
		return nil
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}
