package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/synth"
	"github.com/spf13/cobra"
)

var (
	synthDryRun  bool
	synthWrite   bool
	synthProject string
	synthDays    int
)

var synthCmd = &cobra.Command{
	Use:   "synth weekly|decisions|stale",
	Short: "Run an AI synthesis job (cloud Claude) over the indexed notes",
	Long: "synth assembles a prompt from the indexed notes and (with --write) calls the\n" +
		"Anthropic API to draft output. weekly -> reflections/YYYY-Www.md; decisions\n" +
		"--project <slug> -> a marked rollup block appended to that project's _index.md;\n" +
		"stale -> reflections/stale-<date>.md. --dry-run prints the prompt and target\n" +
		"path without calling the API or writing anything (the default if neither\n" +
		"--dry-run nor --write is given).\n\n" +
		"Requires ANTHROPIC_API_KEY in the environment (only for --write).",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{string(synth.KindWeekly), string(synth.KindDecisions), string(synth.KindStale)},
	RunE: func(cmd *cobra.Command, args []string) error {
		kind := synth.Kind(args[0])
		switch kind {
		case synth.KindWeekly, synth.KindDecisions, synth.KindStale:
		default:
			return fmt.Errorf("unknown synth job %q (want weekly|decisions|stale)", args[0])
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		// Default to dry-run when neither flag is set: synthesis costs money and
		// makes a network call, so it must be explicit.
		dryRun := synthDryRun || !synthWrite
		return runSynth(cmd.Context(), cfg, synth.Options{
			Kind:    kind,
			Project: synthProject,
			Days:    synthDays,
			DryRun:  dryRun,
			Write:   synthWrite && !synthDryRun,
		}, cmd.OutOrStdout())
	},
}

// runSynth opens the store, builds the synthesis client (only when actually
// writing — dry-run needs no key), runs the job, and reports.
func runSynth(ctx context.Context, cfg *config.Config, opts synth.Options, out io.Writer) error {
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return err
	}
	defer s.Close()

	var client synth.Client
	if opts.Write {
		key, err := config.AnthropicAPIKey()
		if err != nil {
			return err // never logged; just surfaced
		}
		client = synth.NewAnthropic(key)
	} else {
		client = noopClient{} // dry-run never calls it
	}

	r := synth.NewRunner(s, client, cfg.Root(), cfg.SynthModel, cfg.SynthMaxTokens)
	res, err := r.Run(ctx, opts)
	if err != nil {
		return err
	}

	if !res.Wrote {
		// Dry-run: print the assembled prompt and the intended output path.
		fmt.Fprintf(out, "# synth %s (dry-run)\n", res.Kind)
		fmt.Fprintf(out, "# intended output: %s\n", res.OutputPath)
		fmt.Fprintf(out, "# model: %s (no API call made)\n\n", cfg.SynthModel)
		fmt.Fprint(out, res.Prompt)
		return nil
	}
	// One-line run summary (no secrets).
	fmt.Fprintf(out, "wrote %s — model %s, %d in / %d out tokens\n",
		res.OutputPath, cfg.SynthModel, res.InputTokens, res.OutputTokens)
	return nil
}

// noopClient stands in during dry-run; calling it is a programming error.
type noopClient struct{}

func (noopClient) Complete(context.Context, synth.Request) (synth.Response, error) {
	return synth.Response{}, fmt.Errorf("internal: synthesis client called during dry-run")
}

func init() {
	synthCmd.Flags().BoolVar(&synthDryRun, "dry-run", false, "print the assembled prompt + target path; no API call, no write (default)")
	synthCmd.Flags().BoolVar(&synthWrite, "write", false, "call the Anthropic API and write the output")
	synthCmd.Flags().StringVar(&synthProject, "project", "", "decisions: scope to (and write the rollup into) this project")
	synthCmd.Flags().IntVar(&synthDays, "days", 14, "stale: idle threshold in days")
	rootCmd.AddCommand(synthCmd)
}
