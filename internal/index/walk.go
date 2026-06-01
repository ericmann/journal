package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// File is a markdown file discovered by the walk, with its repo-relative path
// and modification time.
type File struct {
	RelPath string
	AbsPath string
	ModTime time.Time
}

// Walk returns the markdown files under root, in lexical order, skipping any
// repo-relative path matched by an exclude pattern. When since is non-zero,
// only files modified at or after since are returned.
func Walk(root string, excludes []string, since time.Time) ([]File, error) {
	var files []File
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if Excluded(rel, excludes) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		if Excluded(rel, excludes) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if !since.IsZero() && info.ModTime().Before(since) {
			return nil
		}
		files = append(files, File{RelPath: rel, AbsPath: p, ModTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// Excluded reports whether a repo-relative (slash-separated) path is matched by
// any exclude glob. Supported syntax:
//   - "dir/**"     matches dir and everything under it
//   - "*.ext"      matches by basename via filepath.Match
//   - "a/b/*.ext"  matches the full path via filepath.Match
func Excluded(rel string, patterns []string) bool {
	rel = filepath.ToSlash(rel)
	for _, pat := range patterns {
		pat = filepath.ToSlash(strings.TrimSpace(pat))
		if pat == "" {
			continue
		}
		if strings.HasSuffix(pat, "/**") {
			prefix := strings.TrimSuffix(pat, "/**")
			if rel == prefix || strings.HasPrefix(rel, prefix+"/") {
				return true
			}
			continue
		}
		if pat == "**" {
			return true
		}
		// Try matching the full path and the basename.
		if ok, _ := filepath.Match(pat, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(pat, filepath.Base(rel)); ok {
			return true
		}
	}
	return false
}

// ReadFile is a thin helper so callers can read a discovered file's content.
func ReadFile(f File) (string, error) {
	data, err := os.ReadFile(f.AbsPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
