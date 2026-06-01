package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

var doctorJSON bool

// modelLister is the slice of the Ollama client doctor needs; tests inject a
// fake so the health checks run without a network.
type modelLister interface {
	Tags(ctx context.Context) ([]string, error)
}

// check is one health-check result.
type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// doctorReport is the --json shape for doctor.
type doctorReport struct {
	OK     bool    `json:"ok"`
	Checks []check `json:"checks"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check Ollama, models, the index, and repo/config health",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			// No repo/config is itself a failed check.
			rep := doctorReport{Checks: []check{{Name: "repo/config", OK: false, Detail: err.Error()}}}
			renderDoctor(out, rep, doctorJSON)
			return errSilent
		}
		rep := runDoctor(cmd.Context(), cfg, embed.NewOllama(cfg.OllamaBaseURL, cfg.EmbedModel, cfg.Reranker))
		renderDoctor(out, rep, doctorJSON)
		if !rep.OK {
			return errSilent
		}
		return nil
	},
}

// runDoctor runs all health checks and returns a report. It is network-free
// except through the provided modelLister.
func runDoctor(ctx context.Context, cfg *config.Config, lister modelLister) doctorReport {
	var checks []check

	checks = append(checks, check{
		Name:   "repo/config",
		OK:     true,
		Detail: fmt.Sprintf("root %s; embed_model=%s reranker=%s embed_dim=%d", cfg.Root(), cfg.EmbedModel, cfg.Reranker, cfg.EmbedDim),
	})

	tags, err := lister.Tags(ctx)
	if err != nil {
		checks = append(checks, check{Name: "ollama", OK: false, Detail: err.Error()})
	} else {
		checks = append(checks, check{Name: "ollama", OK: true, Detail: fmt.Sprintf("reachable at %s; %d models", cfg.OllamaBaseURL, len(tags))})
		checks = append(checks, modelCheck("embed_model", cfg.EmbedModel, tags))
		checks = append(checks, modelCheck("reranker", cfg.Reranker, tags))
	}

	checks = append(checks, storeCheck(ctx, cfg))

	// Informational: the synthesis key is optional (only Phase 5 needs it) and
	// never affects the overall health verdict.
	if _, kerr := config.AnthropicAPIKey(); kerr != nil {
		checks = append(checks, check{Name: "anthropic_key (synth)", OK: true, Detail: "not set — only needed for `journal synth`"})
	} else {
		checks = append(checks, check{Name: "anthropic_key (synth)", OK: true, Detail: "set"})
	}

	rep := doctorReport{OK: true, Checks: checks}
	for _, c := range checks {
		if !c.OK {
			rep.OK = false
		}
	}
	return rep
}

func modelCheck(name, model string, tags []string) check {
	if embed.HasModel(tags, model) {
		return check{Name: name, OK: true, Detail: fmt.Sprintf("%s present", model)}
	}
	return check{Name: name, OK: false, Detail: fmt.Sprintf("%s missing — run `ollama pull %s`", model, model)}
}

func storeCheck(ctx context.Context, cfg *config.Config) check {
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return check{Name: "index", OK: false, Detail: err.Error()}
	}
	defer s.Close()
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		return check{Name: "index", OK: false, Detail: err.Error()}
	}
	if v != store.SchemaVersion {
		return check{Name: "index", OK: false, Detail: fmt.Sprintf("schema version %d != expected %d; run `journal index --rebuild`", v, store.SchemaVersion)}
	}
	n, _ := s.Count(ctx)
	detail := fmt.Sprintf("schema v%d, %d chunks", v, n)
	if n == 0 {
		detail += " (run `journal index`)"
	}
	return check{Name: "index", OK: true, Detail: detail}
}

func renderDoctor(out io.Writer, rep doctorReport, jsonMode bool) {
	if jsonMode {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return
	}
	for _, c := range rep.Checks {
		mark := "ok  "
		if !c.OK {
			mark = "FAIL"
		}
		fmt.Fprintf(out, "[%s] %-22s %s\n", mark, c.Name, c.Detail)
	}
	if rep.OK {
		fmt.Fprintln(out, "\nall checks passed")
	} else {
		fmt.Fprintln(out, "\nsome checks failed — see above")
	}
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "emit JSON health report")
	rootCmd.AddCommand(doctorCmd)
}
