package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/models"
	"github.com/spf13/cobra"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Manage local models for voice transcription",
	Long: "models provisions, lists, and verifies local model files used by\n" +
		"`journal transcribe` (whisper) and the meeting pipeline's optional\n" +
		"diarization model. Pull a model once; subsequent pulls are no-ops when\n" +
		"the checksum matches. See docs/CONFIGURATION.md for the transcriber/\n" +
		"diarization keys.",
}

var modelsPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Download every configured model (idempotent)",
	Long: "pull downloads each model with a non-empty model_id in .journal/config.yaml\n" +
		"(transcriber.*, and diarization.* when set) into its model_dir\n" +
		"(~/.cache/journal/models by default). Unconfigured models (empty model_id)\n" +
		"are skipped — no error, no network call. If a file already exists and its\n" +
		"checksum matches, nothing is downloaded. Every configured model is attempted;\n" +
		"the command exits non-zero if any of them failed. After the run MODELS.md in\n" +
		"the journal root is updated.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runModelsPull(cmd.Context(), cfg, models.NewHTTPDownloader(nil), models.DefaultBaseURL, cmd.OutOrStdout())
	},
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed models with id and revision",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runModelsList(cfg, cmd.OutOrStdout())
	},
}

var modelsVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Re-check installed model checksums and report drift",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		return runModelsVerify(cfg, cmd.OutOrStdout())
	},
}

// pullOne provisions a single named model slot (e.g. "transcriber" or
// "diarization") and prints its pull/gated progress line, matching the
// single-model output journal has always produced.
func pullOne(ctx context.Context, cfg *config.Config, name string, t config.Transcriber, dl models.Downloader, baseURL string, out io.Writer) (*models.Manifest, error) {
	revision := t.Revision
	if revision == "" {
		revision = "main"
	}
	modelRoot := cfg.TranscriberModelDirAbs()

	if t.Gated {
		fmt.Fprintf(out, "pulling %s @ %s → %s (gated)\n", t.ModelID, revision, modelRoot)
	} else {
		fmt.Fprintf(out, "pulling %s @ %s → %s\n", t.ModelID, revision, modelRoot)
	}

	auth := models.GatedAuth{
		Gated:     t.Gated,
		AcceptURL: t.AcceptURL,
		Token:     config.HuggingFaceToken(),
	}
	m, err := models.PullFile(ctx, dl, t.ModelID, revision, t.Filename, t.Checksum, modelRoot, baseURL, auth)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	fmt.Fprintf(out, "ok  %s (%s) sha256:%s\n", m.ModelID, m.Revision, m.Checksum)
	return m, nil
}

// runModelsPull attempts every configured model slot (transcriber, and
// diarization when set) with a non-empty model_id, skipping unconfigured
// slots entirely. It attempts all configured slots even if one fails, then
// regenerates MODELS.md from whatever is now installed, and finally returns
// a joined error if any slot failed.
func runModelsPull(ctx context.Context, cfg *config.Config, dl models.Downloader, baseURL string, out io.Writer) error {
	slots := []struct {
		name string
		t    config.Transcriber
	}{
		{"transcriber", cfg.Transcriber},
		{"diarization", cfg.Diarization},
	}

	var attempted int
	var errs []error
	for _, s := range slots {
		if s.t.ModelID == "" {
			continue
		}
		attempted++
		if _, err := pullOne(ctx, cfg, s.name, s.t, dl, baseURL, out); err != nil {
			errs = append(errs, err)
		}
	}
	if attempted == 0 {
		return fmt.Errorf("no models configured to pull: set transcriber.model_id (and/or diarization.model_id) in .journal/config.yaml")
	}

	// Regenerate MODELS.md from whatever is installed, even when a sibling
	// model failed — a successful pull should still be reflected.
	modelRoot := cfg.TranscriberModelDirAbs()
	manifests, err := models.List(modelRoot)
	if err != nil {
		return fmt.Errorf("listing models: %w", err)
	}
	md := models.GenerateMD(manifests)
	mdPath := filepath.Join(cfg.Root(), "MODELS.md")
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return fmt.Errorf("writing MODELS.md: %w", err)
	}
	fmt.Fprintf(out, "updated %s\n", relTo(cfg.Root(), mdPath))

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func runModelsList(cfg *config.Config, out io.Writer) error {
	modelRoot := cfg.TranscriberModelDirAbs()
	manifests, err := models.List(modelRoot)
	if err != nil {
		return err
	}
	if len(manifests) == 0 {
		fmt.Fprintln(out, "no models installed (run `journal models pull`)")
		return nil
	}
	for _, m := range manifests {
		fmt.Fprintf(out, "%s\t%s\t%s\n", m.ModelID, m.Revision, m.Checksum)
	}
	return nil
}

func runModelsVerify(cfg *config.Config, out io.Writer) error {
	modelRoot := cfg.TranscriberModelDirAbs()
	results, err := models.Verify(modelRoot)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "no models installed (run `journal models pull`)")
		return nil
	}
	var failed int
	for _, r := range results {
		if r.OK {
			fmt.Fprintf(out, "ok  %s (%s)\n", r.ModelID, r.Revision)
		} else {
			fmt.Fprintf(out, "ERR %s (%s): %v\n", r.ModelID, r.Revision, r.Err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d model(s) failed verification", failed)
	}
	return nil
}

func init() {
	modelsCmd.AddCommand(modelsPullCmd)
	modelsCmd.AddCommand(modelsListCmd)
	modelsCmd.AddCommand(modelsVerifyCmd)
	rootCmd.AddCommand(modelsCmd)
}
