// Package models provisions and verifies local model files for transcription.
// Downloads are gated behind the Downloader interface so tests never touch the
// network — pass an HTTPDownloader in production, a fake in tests.
package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DefaultBaseURL is the HuggingFace model hub base used when no override is set.
const DefaultBaseURL = "https://huggingface.co"

// Downloader fetches a model blob from a URL to a local destination path.
type Downloader interface {
	Download(ctx context.Context, url, destPath string) error
}

// Manifest records installed model metadata persisted alongside model.bin.
type Manifest struct {
	ModelID  string `json:"model_id"`
	Revision string `json:"revision"`
	Checksum string `json:"checksum"` // SHA-256 hex
}

// VerifyResult holds the outcome of re-checking one installed model's checksum.
type VerifyResult struct {
	Manifest
	OK  bool
	Err error
}

// sanitize converts a model ID to a safe directory name component.
func sanitize(modelID string) string {
	r := strings.NewReplacer("/", "_", ":", "_", " ", "_")
	return r.Replace(modelID)
}

// modelDir returns the absolute directory for a specific model inside root.
func modelDir(root, modelID string) string {
	return filepath.Join(root, sanitize(modelID))
}

// checksumFile computes the SHA-256 hex digest of path, or returns "" on error.
func checksumFile(path string) string {
	s, _ := checksumFileErr(path)
	return s
}

func checksumFileErr(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func writeManifest(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Pull downloads the model into modelRoot if absent or if the checksum does not
// match. It is idempotent: when the file and manifest both exist and checksums
// agree, it returns immediately without network I/O. baseURL defaults to
// DefaultBaseURL when empty. On any failure after the download has started, the
// partial file is removed so no corrupt blob is left in place.
func Pull(ctx context.Context, dl Downloader, modelID, revision, expectedChecksum, modelRoot, baseURL string) (*Manifest, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	dir := modelDir(modelRoot, modelID)
	modelFile := filepath.Join(dir, "model.bin")
	manifestFile := filepath.Join(dir, "manifest.json")

	// Fast path: already present with a matching manifest + on-disk checksum.
	if existing, err := readManifest(manifestFile); err == nil {
		if expectedChecksum == "" || existing.Checksum == expectedChecksum {
			if got := checksumFile(modelFile); got == existing.Checksum && got != "" {
				return existing, nil
			}
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating model dir: %w", err)
	}

	url := baseURL + "/" + modelID + "/resolve/" + revision + "/model.bin"
	tmp := modelFile + ".tmp"
	if err := dl.Download(ctx, url, tmp); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("downloading model: %w", err)
	}

	got, err := checksumFileErr(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("computing checksum: %w", err)
	}
	if expectedChecksum != "" && got != expectedChecksum {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("checksum mismatch: want %s, got %s", expectedChecksum, got)
	}

	if err := os.Rename(tmp, modelFile); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("installing model: %w", err)
	}

	m := &Manifest{
		ModelID:  modelID,
		Revision: revision,
		Checksum: got,
	}
	if err := writeManifest(manifestFile, m); err != nil {
		return nil, fmt.Errorf("writing manifest: %w", err)
	}
	return m, nil
}

// List returns manifests for all models installed under modelRoot.
// Returns nil (not an error) when the directory does not exist yet.
func List(modelRoot string) ([]Manifest, error) {
	entries, err := os.ReadDir(modelRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading model dir: %w", err)
	}
	var out []Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := readManifest(filepath.Join(modelRoot, e.Name(), "manifest.json"))
		if err != nil {
			continue // skip entries with no valid manifest
		}
		out = append(out, *m)
	}
	return out, nil
}

// Verify re-checks the on-disk checksum of every installed model and reports
// drift (file modified or missing since the manifest was written).
func Verify(modelRoot string) ([]VerifyResult, error) {
	manifests, err := List(modelRoot)
	if err != nil {
		return nil, err
	}
	results := make([]VerifyResult, 0, len(manifests))
	for _, m := range manifests {
		modelFile := filepath.Join(modelRoot, sanitize(m.ModelID), "model.bin")
		r := VerifyResult{Manifest: m}
		got, ferr := checksumFileErr(modelFile)
		switch {
		case ferr != nil:
			r.Err = fmt.Errorf("reading model file: %w", ferr)
		case got != m.Checksum:
			r.Err = fmt.Errorf("checksum drift: manifest=%s on-disk=%s", m.Checksum, got)
		default:
			r.OK = true
		}
		results = append(results, r)
	}
	return results, nil
}

// HTTPDownloader implements Downloader using the standard http.Client.
type HTTPDownloader struct {
	Client *http.Client
}

// NewHTTPDownloader returns an HTTPDownloader; a nil client uses http.DefaultClient.
func NewHTTPDownloader(client *http.Client) *HTTPDownloader {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPDownloader{Client: client}
}

// Download fetches url and writes the response body to destPath.
func (d *HTTPDownloader) Download(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}
