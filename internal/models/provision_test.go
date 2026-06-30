package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// blobChecksum returns the SHA-256 hex digest of data.
func blobChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// fakeServer returns an httptest.Server that serves blob on any path, plus a
// cleanup function. The caller must invoke srv.Close().
func fakeServer(t *testing.T, blob []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
}

func TestPullDownloadsAndWritesManifest(t *testing.T) {
	blob := []byte("fake model weights v1")
	wantSum := blobChecksum(blob)
	srv := fakeServer(t, blob)
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	m, err := Pull(context.Background(), dl, "base.en", "main", wantSum, dir, srv.URL)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if m.ModelID != "base.en" || m.Revision != "main" || m.Checksum != wantSum {
		t.Errorf("manifest = %+v, want {base.en main %s}", m, wantSum)
	}

	// model.bin exists
	modelFile := filepath.Join(dir, "base.en", "model.bin")
	data, err := os.ReadFile(modelFile)
	if err != nil {
		t.Fatalf("model.bin not written: %v", err)
	}
	if string(data) != string(blob) {
		t.Errorf("model.bin content mismatch")
	}

	// manifest.json exists and is parseable
	manifest, err := readManifest(filepath.Join(dir, "base.en", "manifest.json"))
	if err != nil {
		t.Fatalf("manifest.json not written: %v", err)
	}
	if manifest.Checksum != wantSum {
		t.Errorf("manifest checksum = %s, want %s", manifest.Checksum, wantSum)
	}
}

func TestPullIdempotentOnChecksumMatch(t *testing.T) {
	blob := []byte("fake model weights v1")
	wantSum := blobChecksum(blob)

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_, _ = w.Write(blob)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	// First pull — downloads.
	if _, err := Pull(context.Background(), dl, "base.en", "main", wantSum, dir, srv.URL); err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 download call, got %d", callCount)
	}

	// Second pull — must be a no-op.
	if _, err := Pull(context.Background(), dl, "base.en", "main", wantSum, dir, srv.URL); err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Errorf("second pull made a network call; want no-op (calls=%d)", callCount)
	}
}

func TestPullChecksumMismatchError(t *testing.T) {
	blob := []byte("fake model weights v1")
	srv := fakeServer(t, blob)
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	_, err := Pull(context.Background(), dl, "base.en", "main", "deadbeef"+strings.Repeat("0", 56), dir, srv.URL)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error %q does not mention checksum mismatch", err)
	}

	// Partial/tmp file must be cleaned up.
	tmpFile := filepath.Join(dir, "base.en", "model.bin.tmp")
	if _, err2 := os.Stat(tmpFile); !os.IsNotExist(err2) {
		t.Errorf("tmp file not cleaned up after mismatch")
	}
}

func TestPullNoChecksumAcceptsAnything(t *testing.T) {
	blob := []byte("any blob")
	srv := fakeServer(t, blob)
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	m, err := Pull(context.Background(), dl, "base.en", "main", "", dir, srv.URL)
	if err != nil {
		t.Fatalf("Pull with empty checksum: %v", err)
	}
	if m.Checksum == "" {
		t.Error("manifest checksum should be computed even when none is expected")
	}
}

func TestListReturnsInstalledModels(t *testing.T) {
	blob := []byte("model data")
	srv := fakeServer(t, blob)
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	if _, err := Pull(context.Background(), dl, "base.en", "main", "", dir, srv.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := Pull(context.Background(), dl, "Systran/faster-whisper-small.en", "main", "", dir, srv.URL); err != nil {
		t.Fatal(err)
	}

	manifests, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) != 2 {
		t.Fatalf("List returned %d manifests, want 2", len(manifests))
	}
}

func TestListEmptyDirReturnsNil(t *testing.T) {
	dir := t.TempDir()
	manifests, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifests != nil {
		t.Errorf("empty dir: expected nil, got %v", manifests)
	}
}

func TestListNonexistentDirReturnsNil(t *testing.T) {
	manifests, err := List("/tmp/journal-test-nonexistent-dir-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if manifests != nil {
		t.Errorf("nonexistent dir: expected nil, got %v", manifests)
	}
}

func TestVerifyOKAndDrift(t *testing.T) {
	blob := []byte("model data")
	srv := fakeServer(t, blob)
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	if _, err := Pull(context.Background(), dl, "base.en", "main", "", dir, srv.URL); err != nil {
		t.Fatal(err)
	}

	// Clean verify.
	results, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected OK verify, got %+v", results)
	}

	// Tamper with the model file to simulate drift.
	modelFile := filepath.Join(dir, "base.en", "model.bin")
	if err := os.WriteFile(modelFile, []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err = Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].OK {
		t.Fatalf("expected drift, got %+v", results)
	}
	if !strings.Contains(results[0].Err.Error(), "checksum drift") {
		t.Errorf("drift error = %v, want 'checksum drift'", results[0].Err)
	}
}

func TestVerifyMissingModelFile(t *testing.T) {
	blob := []byte("model data")
	srv := fakeServer(t, blob)
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	if _, err := Pull(context.Background(), dl, "base.en", "main", "", dir, srv.URL); err != nil {
		t.Fatal(err)
	}

	// Remove the model file but leave the manifest.
	modelFile := filepath.Join(dir, "base.en", "model.bin")
	if err := os.Remove(modelFile); err != nil {
		t.Fatal(err)
	}

	results, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].OK {
		t.Fatalf("expected failure for missing file, got %+v", results)
	}
}
