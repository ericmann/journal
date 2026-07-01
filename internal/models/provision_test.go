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

	m, err := Pull(context.Background(), dl, "base.en", "main", wantSum, dir, srv.URL, GatedAuth{})
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
	if _, err := Pull(context.Background(), dl, "base.en", "main", wantSum, dir, srv.URL, GatedAuth{}); err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 download call, got %d", callCount)
	}

	// Second pull — must be a no-op.
	if _, err := Pull(context.Background(), dl, "base.en", "main", wantSum, dir, srv.URL, GatedAuth{}); err != nil {
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

	_, err := Pull(context.Background(), dl, "base.en", "main", "deadbeef"+strings.Repeat("0", 56), dir, srv.URL, GatedAuth{})
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

	m, err := Pull(context.Background(), dl, "base.en", "main", "", dir, srv.URL, GatedAuth{})
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

	if _, err := Pull(context.Background(), dl, "base.en", "main", "", dir, srv.URL, GatedAuth{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Pull(context.Background(), dl, "Systran/faster-whisper-small.en", "main", "", dir, srv.URL, GatedAuth{}); err != nil {
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

	if _, err := Pull(context.Background(), dl, "base.en", "main", "", dir, srv.URL, GatedAuth{}); err != nil {
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

	if _, err := Pull(context.Background(), dl, "base.en", "main", "", dir, srv.URL, GatedAuth{}); err != nil {
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

// gatedServer serves blob only when the request carries wantToken as a
// Bearer Authorization header; otherwise it returns 401, mimicking a gated
// HuggingFace repo.
func gatedServer(t *testing.T, blob []byte, wantToken string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+wantToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(blob)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPullGatedNoTokenFailsWithAcceptTermsMessage(t *testing.T) {
	blob := []byte("gated model weights")
	srv := gatedServer(t, blob, "valid-token")

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)
	auth := GatedAuth{Gated: true, AcceptURL: "https://huggingface.co/pyannote/speaker-diarization-3.1"}

	_, err := Pull(context.Background(), dl, "pyannote/speaker-diarization-3.1", "main", "", dir, srv.URL, auth)
	if err == nil {
		t.Fatal("expected error for gated model with no token, got nil")
	}
	if !strings.Contains(err.Error(), "accept terms at https://huggingface.co/pyannote/speaker-diarization-3.1") {
		t.Errorf("error %q does not mention accept-terms URL", err)
	}
	if !strings.Contains(err.Error(), "HF_TOKEN") {
		t.Errorf("error %q does not mention HF_TOKEN", err)
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("error %q should still surface the underlying 401 (wrapped)", err)
	}

	// No partial file left behind.
	tmpFile := filepath.Join(dir, "pyannote_speaker-diarization-3.1", "model.bin.tmp")
	if _, statErr := os.Stat(tmpFile); !os.IsNotExist(statErr) {
		t.Errorf("tmp file not cleaned up after gated auth failure")
	}
}

func TestPullGatedInvalidTokenFailsWithAcceptTermsMessage(t *testing.T) {
	blob := []byte("gated model weights")
	srv := gatedServer(t, blob, "valid-token")

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)
	auth := GatedAuth{
		Gated:     true,
		AcceptURL: "https://huggingface.co/pyannote/speaker-diarization-3.1",
		Token:     "wrong-token",
	}

	_, err := Pull(context.Background(), dl, "pyannote/speaker-diarization-3.1", "main", "", dir, srv.URL, auth)
	if err == nil {
		t.Fatal("expected error for gated model with invalid token, got nil")
	}
	if !strings.Contains(err.Error(), "accept terms at") {
		t.Errorf("error %q does not mention accept-terms message", err)
	}
}

func TestPullGatedWithValidTokenSucceeds(t *testing.T) {
	blob := []byte("gated model weights")
	wantSum := blobChecksum(blob)
	srv := gatedServer(t, blob, "valid-token")

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)
	auth := GatedAuth{
		Gated:     true,
		AcceptURL: "https://huggingface.co/pyannote/speaker-diarization-3.1",
		Token:     "valid-token",
	}

	m, err := Pull(context.Background(), dl, "pyannote/speaker-diarization-3.1", "main", wantSum, dir, srv.URL, auth)
	if err != nil {
		t.Fatalf("Pull with valid token: %v", err)
	}
	if !m.Gated || m.AcceptURL != auth.AcceptURL {
		t.Errorf("manifest = %+v, want Gated=true AcceptURL=%s", m, auth.AcceptURL)
	}
}

func TestPullFileCustomFilenameDownloadsAndRecordsManifest(t *testing.T) {
	blob := []byte("pyannote config yaml contents")
	wantSum := blobChecksum(blob)
	srv := fakeServer(t, blob)
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	m, err := PullFile(context.Background(), dl, "pyannote/speaker-diarization-community-1", "main", "config.yaml", wantSum, dir, srv.URL, GatedAuth{})
	if err != nil {
		t.Fatalf("PullFile: %v", err)
	}
	if m.Filename != "config.yaml" {
		t.Errorf("manifest.Filename = %q, want config.yaml", m.Filename)
	}

	modelFile := filepath.Join(dir, "pyannote_speaker-diarization-community-1", "config.yaml")
	data, err := os.ReadFile(modelFile)
	if err != nil {
		t.Fatalf("config.yaml not written: %v", err)
	}
	if string(data) != string(blob) {
		t.Errorf("config.yaml content mismatch")
	}
}

func TestVerifyManifestWithFilenameChecksumsRightPath(t *testing.T) {
	blob := []byte("pyannote config yaml contents")
	wantSum := blobChecksum(blob)
	srv := fakeServer(t, blob)
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	if _, err := PullFile(context.Background(), dl, "pyannote/speaker-diarization-community-1", "main", "config.yaml", wantSum, dir, srv.URL, GatedAuth{}); err != nil {
		t.Fatal(err)
	}

	results, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected OK verify against config.yaml, got %+v", results)
	}
}

func TestVerifyManifestWithoutFilenameDefaultsToModelBin(t *testing.T) {
	blob := []byte("model data")
	srv := fakeServer(t, blob)
	defer srv.Close()

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	if _, err := Pull(context.Background(), dl, "base.en", "main", "", dir, srv.URL, GatedAuth{}); err != nil {
		t.Fatal(err)
	}

	// Simulate a manifest written before Filename existed.
	manifestPath := filepath.Join(dir, "base.en", "manifest.json")
	m, err := readManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	m.Filename = ""
	if err := writeManifest(manifestPath, m); err != nil {
		t.Fatal(err)
	}

	results, err := Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("expected OK verify defaulting to model.bin, got %+v", results)
	}
}

func TestPullFileGatedNoTokenFailsWithAcceptTermsMessage(t *testing.T) {
	blob := []byte("gated diarization config")
	srv := gatedServer(t, blob, "valid-token")

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)
	auth := GatedAuth{Gated: true, AcceptURL: "https://huggingface.co/pyannote/speaker-diarization-community-1"}

	_, err := PullFile(context.Background(), dl, "pyannote/speaker-diarization-community-1", "main", "config.yaml", "", dir, srv.URL, auth)
	if err == nil {
		t.Fatal("expected error for gated model with no token, got nil")
	}
	if !strings.Contains(err.Error(), "accept terms at https://huggingface.co/pyannote/speaker-diarization-community-1") {
		t.Errorf("error %q does not mention accept-terms URL", err)
	}
	if !strings.Contains(err.Error(), "HF_TOKEN") {
		t.Errorf("error %q does not mention HF_TOKEN", err)
	}
}

func TestPullUngatedNoRegressionWithNoToken(t *testing.T) {
	blob := []byte("ungated model weights")
	wantSum := blobChecksum(blob)
	srv := fakeServer(t, blob)

	dir := t.TempDir()
	dl := NewHTTPDownloader(nil)

	m, err := Pull(context.Background(), dl, "base.en", "main", wantSum, dir, srv.URL, GatedAuth{})
	if err != nil {
		t.Fatalf("ungated Pull with no token: %v", err)
	}
	if m.Gated {
		t.Errorf("ungated manifest should not be marked gated: %+v", m)
	}
}
