package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/models"
)

// modelChecksum returns the SHA-256 hex digest of data.
func modelChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// modelFakeServer starts an httptest server that serves blob on every request.
func modelFakeServer(t *testing.T, blob []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// modelGatedServer starts an httptest server that only serves blob when the
// request's Bearer token matches wantToken, otherwise returning 401 — a
// stand-in for a gated HuggingFace repo.
func modelGatedServer(t *testing.T, blob []byte, wantToken string) *httptest.Server {
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

func TestRunModelsPullDownloadsAndUpdatesModelsMD(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("fake whisper model weights")
	wantSum := modelChecksum(blob)
	srv := modelFakeServer(t, blob)

	cfg.Transcriber.ModelID = "base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = wantSum
	cfg.Transcriber.ModelDir = t.TempDir()

	var out bytes.Buffer
	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &out); err != nil {
		t.Fatalf("runModelsPull: %v", err)
	}

	// model.bin written
	modelFile := filepath.Join(cfg.Transcriber.ModelDir, "base.en", "model.bin")
	data, err := os.ReadFile(modelFile)
	if err != nil {
		t.Fatalf("model.bin not written: %v", err)
	}
	if string(data) != string(blob) {
		t.Error("model.bin content mismatch")
	}

	// MODELS.md written at journal root
	mdPath := filepath.Join(cfg.Root(), "MODELS.md")
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("MODELS.md not written: %v", err)
	}
	for _, want := range []string{"base.en", "main", wantSum} {
		if !strings.Contains(string(md), want) {
			t.Errorf("MODELS.md missing %q:\n%s", want, md)
		}
	}

	// Output mentions model and checksum
	output := out.String()
	if !strings.Contains(output, "base.en") || !strings.Contains(output, wantSum) {
		t.Errorf("unexpected output: %s", output)
	}
}

func TestRunModelsPullIdempotent(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("fake model")
	wantSum := modelChecksum(blob)

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_, _ = w.Write(blob)
	}))
	t.Cleanup(srv.Close)

	cfg.Transcriber.ModelID = "base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = wantSum
	cfg.Transcriber.ModelDir = t.TempDir()

	// First pull downloads.
	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 download, got %d", callCount)
	}

	// Second pull must be a no-op.
	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Errorf("second pull made %d total downloads (want 1 — idempotent)", callCount)
	}
}

func TestRunModelsPullChecksumMismatch(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("some data")
	srv := modelFakeServer(t, blob)

	cfg.Transcriber.ModelID = "base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = "badhash" + strings.Repeat("0", 57) // wrong checksum
	cfg.Transcriber.ModelDir = t.TempDir()

	err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error %q does not mention checksum mismatch", err)
	}
}

func TestRunModelsListEmpty(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Transcriber.ModelDir = t.TempDir()

	var out bytes.Buffer
	if err := runModelsList(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no models installed") {
		t.Errorf("unexpected output: %s", out.String())
	}
}

func TestRunModelsListShowsInstalled(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("model data")
	srv := modelFakeServer(t, blob)

	cfg.Transcriber.ModelID = "base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = ""
	cfg.Transcriber.ModelDir = t.TempDir()

	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runModelsList(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "base.en") {
		t.Errorf("list output missing model: %s", out.String())
	}
}

func TestRunModelsVerifyOK(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("model data")
	srv := modelFakeServer(t, blob)

	cfg.Transcriber.ModelID = "base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = ""
	cfg.Transcriber.ModelDir = t.TempDir()

	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runModelsVerify(cfg, &out); err != nil {
		t.Fatalf("verify on clean model: %v", err)
	}
	if !strings.Contains(out.String(), "ok  base.en") {
		t.Errorf("verify output: %s", out.String())
	}
}

func TestRunModelsVerifyDrift(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("model data")
	srv := modelFakeServer(t, blob)

	cfg.Transcriber.ModelID = "base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = ""
	cfg.Transcriber.ModelDir = t.TempDir()

	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	// Tamper with model file.
	modelFile := filepath.Join(cfg.Transcriber.ModelDir, "base.en", "model.bin")
	if err := os.WriteFile(modelFile, []byte("corrupted!"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runModelsVerify(cfg, &out)
	if err == nil {
		t.Fatal("expected verify to fail on drifted model")
	}
	if !strings.Contains(out.String(), "ERR") {
		t.Errorf("verify output missing ERR: %s", out.String())
	}
}

func TestRunModelsPullNoModelIDError(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Transcriber.ModelID = ""
	cfg.Transcriber.ModelDir = t.TempDir()

	err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), "http://unused", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "model_id") {
		t.Errorf("expected model_id error, got: %v", err)
	}
}

func TestGenerateMDAfterPull(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("model data")
	wantSum := modelChecksum(blob)
	srv := modelFakeServer(t, blob)

	cfg.Transcriber.ModelID = "Systran/faster-whisper-base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = wantSum
	cfg.Transcriber.ModelDir = t.TempDir()

	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	mdPath := filepath.Join(cfg.Root(), "MODELS.md")
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Systran/faster-whisper-base.en",
		"main",
		wantSum,
		"journal models pull",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("MODELS.md missing %q", want)
		}
	}
}

func TestRunModelsPullGatedNoTokenFailsWithAcceptTermsMessage(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("gated model weights")
	srv := modelGatedServer(t, blob, "valid-token")

	t.Setenv(config.HFTokenEnv, "")
	cfg.Transcriber.ModelID = "pyannote/speaker-diarization-3.1"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.ModelDir = t.TempDir()
	cfg.Transcriber.Gated = true
	cfg.Transcriber.AcceptURL = "https://huggingface.co/pyannote/speaker-diarization-3.1"

	err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected gated pull with no token to fail")
	}
	if !strings.Contains(err.Error(), "accept terms at https://huggingface.co/pyannote/speaker-diarization-3.1") {
		t.Errorf("error %q does not mention accept-terms URL", err)
	}
	if !strings.Contains(err.Error(), "HF_TOKEN") {
		t.Errorf("error %q does not mention HF_TOKEN", err)
	}
}

func TestRunModelsPullGatedWithTokenSucceedsAndRecordsInModelsMD(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("gated model weights")
	wantSum := modelChecksum(blob)
	srv := modelGatedServer(t, blob, "valid-token")

	t.Setenv(config.HFTokenEnv, "valid-token")
	cfg.Transcriber.ModelID = "pyannote/speaker-diarization-3.1"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = wantSum
	cfg.Transcriber.ModelDir = t.TempDir()
	cfg.Transcriber.Gated = true
	cfg.Transcriber.AcceptURL = "https://huggingface.co/pyannote/speaker-diarization-3.1"

	var out bytes.Buffer
	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &out); err != nil {
		t.Fatalf("gated runModelsPull with valid token: %v", err)
	}

	mdPath := filepath.Join(cfg.Root(), "MODELS.md")
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"pyannote/speaker-diarization-3.1",
		"accept terms",
		"https://huggingface.co/pyannote/speaker-diarization-3.1",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("MODELS.md missing %q:\n%s", want, md)
		}
	}
}

func TestRunModelsPullBothTranscriberAndDiarizationConfigured(t *testing.T) {
	cfg := testRepo(t, nil)
	transcriberBlob := []byte("whisper model weights")
	transcriberSum := modelChecksum(transcriberBlob)
	diarizationBlob := []byte("pyannote config yaml")
	diarizationSum := modelChecksum(diarizationBlob)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "pyannote") {
			_, _ = w.Write(diarizationBlob)
			return
		}
		_, _ = w.Write(transcriberBlob)
	}))
	t.Cleanup(srv.Close)

	cfg.Transcriber.ModelID = "base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = transcriberSum
	cfg.Transcriber.ModelDir = t.TempDir()

	cfg.Diarization.ModelID = "pyannote/speaker-diarization-community-1"
	cfg.Diarization.Revision = "main"
	cfg.Diarization.Filename = "config.yaml"
	cfg.Diarization.Checksum = diarizationSum
	cfg.Diarization.ModelDir = cfg.Transcriber.ModelDir

	var out bytes.Buffer
	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &out); err != nil {
		t.Fatalf("runModelsPull: %v", err)
	}

	mdPath := filepath.Join(cfg.Root(), "MODELS.md")
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"base.en", transcriberSum, "pyannote/speaker-diarization-community-1", diarizationSum} {
		if !strings.Contains(string(md), want) {
			t.Errorf("MODELS.md missing %q:\n%s", want, md)
		}
	}
}

func TestRunModelsPullOnlyDiarizationConfiguredStillPullsDefaultTranscriber(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("model data")
	srv := modelFakeServer(t, blob)

	// cfg.Transcriber keeps its non-empty default ModelID from testRepo/Default.
	cfg.Transcriber.ModelDir = t.TempDir()
	cfg.Diarization.ModelID = "pyannote/speaker-diarization-community-1"
	cfg.Diarization.Filename = "config.yaml"
	cfg.Diarization.ModelDir = cfg.Transcriber.ModelDir

	var out bytes.Buffer
	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &out); err != nil {
		t.Fatalf("runModelsPull: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, cfg.Transcriber.ModelID) {
		t.Errorf("output missing transcriber pull: %s", output)
	}
	if !strings.Contains(output, "pyannote/speaker-diarization-community-1") {
		t.Errorf("output missing diarization pull: %s", output)
	}
}

func TestRunModelsPullGatedDiarizationMissingTokenFailsButTranscriberSucceeds(t *testing.T) {
	cfg := testRepo(t, nil)
	transcriberBlob := []byte("whisper model weights")
	transcriberSum := modelChecksum(transcriberBlob)
	diarizationBlob := []byte("gated pyannote config")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "pyannote") {
			if r.Header.Get("Authorization") != "Bearer valid-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write(diarizationBlob)
			return
		}
		_, _ = w.Write(transcriberBlob)
	}))
	t.Cleanup(srv.Close)

	t.Setenv(config.HFTokenEnv, "")
	cfg.Transcriber.ModelID = "base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = transcriberSum
	cfg.Transcriber.ModelDir = t.TempDir()

	cfg.Diarization.ModelID = "pyannote/speaker-diarization-community-1"
	cfg.Diarization.Revision = "main"
	cfg.Diarization.Filename = "config.yaml"
	cfg.Diarization.Gated = true
	cfg.Diarization.AcceptURL = "https://huggingface.co/pyannote/speaker-diarization-community-1"
	cfg.Diarization.ModelDir = cfg.Transcriber.ModelDir

	var out bytes.Buffer
	err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &out)
	if err == nil {
		t.Fatal("expected error: diarization pull should fail with no HF_TOKEN")
	}
	if !strings.Contains(err.Error(), "diarization") {
		t.Errorf("error %q should scope the failure to diarization", err)
	}
	if !strings.Contains(err.Error(), "accept terms at") {
		t.Errorf("error %q should still carry the accept-terms message", err)
	}

	// Transcriber succeeded despite diarization failing — MODELS.md reflects it.
	mdPath := filepath.Join(cfg.Root(), "MODELS.md")
	md, mderr := os.ReadFile(mdPath)
	if mderr != nil {
		t.Fatalf("MODELS.md should still be written for the successful transcriber pull: %v", mderr)
	}
	if !strings.Contains(string(md), "base.en") {
		t.Errorf("MODELS.md missing successful transcriber pull:\n%s", md)
	}
}

func TestRunModelsPullNoModelsConfiguredError(t *testing.T) {
	cfg := testRepo(t, nil)
	cfg.Transcriber.ModelID = ""
	cfg.Diarization.ModelID = ""
	cfg.Transcriber.ModelDir = t.TempDir()

	err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), "http://unused", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "model_id") {
		t.Errorf("expected model_id error, got: %v", err)
	}
}

func TestRunModelsPullUngatedNoRegressionWithNoToken(t *testing.T) {
	cfg := testRepo(t, nil)
	blob := []byte("ungated model weights")
	wantSum := modelChecksum(blob)
	srv := modelFakeServer(t, blob)

	t.Setenv(config.HFTokenEnv, "")
	cfg.Transcriber.ModelID = "base.en"
	cfg.Transcriber.Revision = "main"
	cfg.Transcriber.Checksum = wantSum
	cfg.Transcriber.ModelDir = t.TempDir()

	if err := runModelsPull(context.Background(), cfg, models.NewHTTPDownloader(nil), srv.URL, &bytes.Buffer{}); err != nil {
		t.Fatalf("ungated pull with no HF_TOKEN set: %v", err)
	}
}
