package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

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
	synthDate    string
)

var synthCmd = &cobra.Command{
	Use:   "synth weekly|daily|meetings|decisions|stale",
	Short: "Run an AI synthesis job (cloud Claude or local Ollama) over the indexed notes",
	Long: "synth assembles a prompt from the indexed notes and (with --write) calls the\n" +
		"Anthropic API to draft output. weekly -> reflections/YYYY-Www.md; daily -> \n" +
		"reflections/daily-<date>.md (--date, default today); decisions --project <slug>\n" +
		"-> a marked rollup block appended to that project's _index.md; stale -> \n" +
		"reflections/stale-<date>.md. --dry-run prints the prompt and target path without\n" +
		"calling the API or writing anything (the default if neither --dry-run nor\n" +
		"--write is given).\n\n" +
		"The provider is set by synth_provider in .journal/config.yaml: \"anthropic\"\n" +
		"(default; --write needs ANTHROPIC_API_KEY) or \"ollama\" (fully local, uses\n" +
		"synth_ollama_model — no key, nothing leaves the machine).",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{string(synth.KindWeekly), string(synth.KindDaily), string(synth.KindMeetings), string(synth.KindDecisions), string(synth.KindStale)},
	RunE: func(cmd *cobra.Command, args []string) error {
		kind := synth.Kind(args[0])
		switch kind {
		case synth.KindWeekly, synth.KindDaily, synth.KindMeetings, synth.KindDecisions, synth.KindStale:
		default:
			return fmt.Errorf("unknown synth job %q (want weekly|daily|meetings|decisions|stale)", args[0])
		}
		var date time.Time
		if synthDate != "" {
			var err error
			date, err = time.ParseInLocation("2006-01-02", synthDate, time.Local)
			if err != nil {
				return fmt.Errorf("invalid --date %q (want YYYY-MM-DD)", synthDate)
			}
		}
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		// Default to dry-run when neither flag is set: synthesis costs money and
		// makes a network call, so it must be explicit.
		dryRun := synthDryRun || !synthWrite
		// Leave Days at 0 unless explicitly set, so each kind applies its own
		// default (stale=14, meetings=7).
		days := synthDays
		if !cmd.Flags().Changed("days") {
			days = 0
		}
		return hintOllama(cfg, runSynth(cmd.Context(), cfg, synth.Options{
			Kind:    kind,
			Project: synthProject,
			Days:    days,
			Date:    date,
			DryRun:  dryRun,
			Write:   synthWrite && !synthDryRun,
		}, cmd.OutOrStdout()))
	},
}

// runSynth opens the store, builds the synthesis client per the configured
// provider (only when actually writing — dry-run needs no key or model), runs
// the job, and reports.
func runSynth(ctx context.Context, cfg *config.Config, opts synth.Options, out io.Writer) error {
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return err
	}
	defer s.Close()

	var client synth.Client
	if opts.Write {
		client, err = synthClient(cfg)
		if err != nil {
			return err // never includes the key; just surfaced
		}
	} else {
		client = noopClient{} // dry-run never calls it
	}

	r := synth.NewRunner(s, client, cfg.Root(), cfg.ActiveSynthModel(), cfg.SynthMaxTokens, readVoiceProfile(cfg))
	res, err := r.Run(ctx, opts)
	if err != nil {
		return err
	}

	if !res.Wrote {
		// Dry-run (the default): print the assembled prompt + intended path; no
		// API call, no write. The hint makes the next step obvious.
		fmt.Fprintf(out, "# synth %s — DRY RUN (no API call, nothing written)\n", res.Kind)
		fmt.Fprintf(out, "# intended output: %s\n", res.OutputPath)
		fmt.Fprintf(out, "# provider: %s, model: %s\n", cfg.SynthProvider, cfg.ActiveSynthModel())
		if cfg.SynthProvider == config.SynthProviderOllama {
			fmt.Fprintf(out, "# → re-run with --write to generate locally via Ollama and save the draft\n\n")
		} else {
			fmt.Fprintf(out, "# → re-run with --write to call the API and save the draft (needs ANTHROPIC_API_KEY)\n\n")
		}
		fmt.Fprint(out, res.Prompt)
		return nil
	}
	// One-line run summary (no secrets).
	fmt.Fprintf(out, "wrote %s — model %s, %d in / %d out tokens\n",
		res.OutputPath, cfg.ActiveSynthModel(), res.InputTokens, res.OutputTokens)
	return nil
}

// synthClient builds the write-mode synthesis client for the configured
// provider, enforcing the local_only egress guard.
func synthClient(cfg *config.Config) (synth.Client, error) {
	switch cfg.SynthProvider {
	case config.SynthProviderOllama:
		return synth.NewOllama(cfg.OllamaBaseURL, cfg.SynthNumCtx), nil
	default: // anthropic
		if cfg.LocalOnly {
			return nil, fmt.Errorf("local_only is enabled: cloud synthesis is disabled — set `synth_provider: ollama` in .journal/config.yaml (see docs/DATA-FLOWS.md)")
		}
		key, err := config.AnthropicAPIKey()
		if err != nil {
			return nil, err
		}
		return synth.NewAnthropic(key), nil
	}
}

// readVoiceProfile loads the configured voice profile, returning "" if none is
// configured or the file is absent (it's optional).
func readVoiceProfile(cfg *config.Config) string {
	path := cfg.VoiceProfileAbsPath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "" // optional: missing/unreadable profile just means no voice section
	}
	return string(data)
}

// noopClient stands in during dry-run; calling it is a programming error.
type noopClient struct{}

func (noopClient) Complete(context.Context, synth.Request) (synth.Response, error) {
	return synth.Response{}, fmt.Errorf("internal: synthesis client called during dry-run")
}

func init() {
	synthCmd.Flags().BoolVar(&synthDryRun, "dry-run", false, "print the assembled prompt + target path; no API call, no write (default)")
	synthCmd.Flags().BoolVar(&synthWrite, "write", false, "call the configured synthesis provider and write the output")
	synthCmd.Flags().StringVar(&synthProject, "project", "", "decisions: scope to (and write the rollup into) this project")
	synthCmd.Flags().IntVar(&synthDays, "days", 14, "stale: idle threshold in days")
	synthCmd.Flags().StringVar(&synthDate, "date", "", "daily: the day to summarize as YYYY-MM-DD (default today)")
	rootCmd.AddCommand(synthCmd)
}
