package cmd

import (
	"context"
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
	Long: "models provisions, lists, and verifies local whisper model files used\n" +
		"by `journal transcribe`. Pull a model once; subsequent pulls are no-ops\n" +
		"when the checksum matches. See docs/CONFIGURATION.md for the transcriber keys.",
}

var modelsPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Download the configured transcribe model (idempotent)",
	Long: "pull downloads the model defined by transcriber.model_id / revision / checksum\n" +
		"in .journal/config.yaml into transcriber.model_dir (~/.cache/journal/models by\n" +
		"default). If the file already exists and its checksum matches, nothing is\n" +
		"downloaded. After a successful pull MODELS.md in the journal root is updated.",
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

func runModelsPull(ctx context.Context, cfg *config.Config, dl models.Downloader, baseURL string, out io.Writer) error {
	modelID := cfg.Transcriber.ModelID
	if modelID == "" {
		return fmt.Errorf("transcriber.model_id is not set in .journal/config.yaml")
	}
	revision := cfg.Transcriber.Revision
	if revision == "" {
		revision = "main"
	}
	modelRoot := cfg.TranscriberModelDirAbs()

	if cfg.Transcriber.Gated {
		fmt.Fprintf(out, "pulling %s @ %s → %s (gated)\n", modelID, revision, modelRoot)
	} else {
		fmt.Fprintf(out, "pulling %s @ %s → %s\n", modelID, revision, modelRoot)
	}

	auth := models.GatedAuth{
		Gated:     cfg.Transcriber.Gated,
		AcceptURL: cfg.Transcriber.AcceptURL,
		Token:     config.HuggingFaceToken(),
	}
	m, err := models.Pull(ctx, dl, modelID, revision, cfg.Transcriber.Checksum, modelRoot, baseURL, auth)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "ok  %s (%s) sha256:%s\n", m.ModelID, m.Revision, m.Checksum)

	// Regenerate MODELS.md in the journal root.
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
