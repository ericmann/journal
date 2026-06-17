package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/quill"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

var doctorJSON bool

// ollamaChecker is the slice of the Ollama client doctor needs: list models and
// embed a probe. Tests inject a fake so the health checks run without a network.
type ollamaChecker interface {
	Tags(ctx context.Context) ([]string, error)
	Embed(ctx context.Context, texts []string, instruction string) ([][]float32, error)
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
// except through the provided checker.
func runDoctor(ctx context.Context, cfg *config.Config, checker ollamaChecker) doctorReport {
	var checks []check

	checks = append(checks, check{
		Name:   "repo/config",
		OK:     true,
		Detail: fmt.Sprintf("root %s; embed_provider=%s embed_dim=%d reranker=%s", cfg.Root(), cfg.EmbedProvider, cfg.EmbedDim, cfg.Reranker),
	})

	// Ollama is contacted only if embeddings, synthesis, or reranking use it.
	ollamaUsed := cfg.EmbedProvider == config.EmbedProviderOllama ||
		cfg.SynthProvider == config.SynthProviderOllama ||
		cfg.Reranker != ""
	if ollamaUsed {
		tags, err := checker.Tags(ctx)
		if err != nil {
			detail := err.Error()
			if errors.Is(err, embed.ErrUnreachable) {
				detail += " — is Ollama running? install: https://ollama.com"
			}
			checks = append(checks, check{Name: "ollama", OK: false, Detail: detail})
		} else {
			checks = append(checks, check{Name: "ollama", OK: true, Detail: fmt.Sprintf("reachable at %s; %d models", cfg.OllamaBaseURL, len(tags))})
			if cfg.EmbedProvider == config.EmbedProviderOllama {
				checks = append(checks, modelCheck("embed_model", cfg.EmbedModel, tags))
				checks = append(checks, embedDimCheck(ctx, cfg, checker, tags))
				checks = append(checks, rerankerCheck(cfg.Reranker, tags))
			}
			if cfg.SynthProvider == config.SynthProviderOllama {
				checks = append(checks, modelCheck("synth_ollama_model", cfg.SynthOllamaModel, tags))
			}
		}
	} else {
		checks = append(checks, check{Name: "ollama", OK: true, Detail: "not used (embeddings + synthesis are remote)"})
	}
	if cfg.EmbedProvider == config.EmbedProviderOpenAI {
		checks = append(checks, embedOpenAICheck(cfg))
	}

	checks = append(checks, storeCheck(ctx, cfg))

	if cfg.Transcripts.Enabled {
		checks = append(checks, transcriptsCheck(cfg))
		checks = append(checks, quillCheck(ctx, cfg))
	}

	checks = append(checks, synthCheck(cfg))
	checks = append(checks, egressCheck(cfg))

	rep := doctorReport{OK: true, Checks: checks}
	for _, c := range checks {
		if !c.OK {
			rep.OK = false
		}
	}
	return rep
}

// embedOpenAICheck reports the OpenAI-compatible embedding config. The API key
// is required for indexing/search, so a missing key fails the verdict (like an
// unreachable Ollama). No live probe — embed_dim mismatches surface at index
// time with the exact number to set.
func embedOpenAICheck(cfg *config.Config) check {
	if _, err := config.OpenAIAPIKey(); err != nil {
		return check{Name: "embed", OK: false, Detail: fmt.Sprintf(
			"openai-compatible (%s @ %s) but %s is not set — required to index and search",
			cfg.EmbedOpenAIModel, cfg.EmbedOpenAIBaseURL, config.OpenAIKeyEnv)}
	}
	return check{Name: "embed", OK: true, Detail: fmt.Sprintf(
		"openai-compatible: %s @ %s (%s set); ensure embed_dim=%d matches the model's output (rebuild after changing)",
		cfg.EmbedOpenAIModel, cfg.EmbedOpenAIBaseURL, config.OpenAIKeyEnv, cfg.EmbedDim)}
}

// synthCheck reports which synthesis provider is active and how to switch to
// the other one, so both the cloud and local-first paths are discoverable.
// Informational — synthesis is optional and never affects the verdict.
func synthCheck(cfg *config.Config) check {
	switch cfg.SynthProvider {
	case config.SynthProviderOllama:
		return check{Name: "synth", OK: true, Detail: fmt.Sprintf(
			"local: %s via Ollama — no API key, nothing leaves the machine (set `synth_provider: anthropic`/`openai` for a cloud model)",
			cfg.SynthOllamaModel)}
	case config.SynthProviderOpenAI:
		key := config.OpenAIKeyEnv + " set"
		if _, err := config.OpenAIAPIKey(); err != nil {
			key = config.OpenAIKeyEnv + " not set — needed for `journal synth --write`"
		}
		return check{Name: "synth", OK: true, Detail: fmt.Sprintf(
			"openai-compatible: %s @ %s (%s); set `synth_provider: ollama` to run fully local",
			cfg.SynthOpenAIModel, cfg.SynthOpenAIBaseURL, key)}
	default:
		key := "ANTHROPIC_API_KEY set"
		if _, err := config.AnthropicAPIKey(); err != nil {
			key = "ANTHROPIC_API_KEY not set — needed only for `journal synth --write`"
		}
		return check{Name: "synth", OK: true, Detail: fmt.Sprintf(
			"cloud: %s (%s); set `synth_provider: ollama` to run fully local",
			cfg.SynthModel, key)}
	}
}

// egressCheck reports the repo's network-egress posture in one line, so "does
// anything leave this machine?" is answerable at a glance. Informational —
// egress is a policy choice, not an error.
func egressCheck(cfg *config.Config) check {
	if cfg.LocalOnly {
		mcpClean := cfg.LocalOnlyMCP != config.LocalOnlyMCPAllow
		mcpDesc := "mcp blocked"
		if !mcpClean {
			mcpDesc = "mcp allowed by attestation (local_only_mcp: allow — egress depends on your MCP client)"
		}
		// local_only guarantees no cloud-AI egress; git sync (to your own
		// remote) is governed by sync_enabled, not local_only.
		syncDesc := "sync off"
		if cfg.SyncEnabled {
			syncDesc = "sync on → your git remote"
		}
		detail := fmt.Sprintf("local_only: no cloud-AI egress (synth local: %s); %s; %s", cfg.SynthOllamaModel, mcpDesc, syncDesc)
		if mcpClean && !cfg.SyncEnabled {
			detail += " — nothing leaves this machine"
		}
		return check{Name: "egress", OK: true, Detail: detail}
	}
	synthDesc := fmt.Sprintf("synth provider ollama (local: %s)", cfg.SynthOllamaModel)
	switch cfg.SynthProvider {
	case config.SynthProviderAnthropic:
		synthDesc = fmt.Sprintf("synth provider anthropic (cloud: %s)", cfg.SynthModel)
	case config.SynthProviderOpenAI:
		synthDesc = fmt.Sprintf("synth provider openai (cloud: %s @ %s)", cfg.SynthOpenAIModel, cfg.SynthOpenAIBaseURL)
	}
	syncDesc := "sync off"
	if cfg.SyncEnabled {
		syncDesc = "sync on"
	}
	return check{Name: "egress", OK: true,
		Detail: fmt.Sprintf("%s, %s, mcp available — set `local_only: true` to block cloud-AI egress", synthDesc, syncDesc)}
}

func modelCheck(name, model string, tags []string) check {
	if embed.HasModel(tags, model) {
		return check{Name: name, OK: true, Detail: fmt.Sprintf("%s present", model)}
	}
	return check{Name: name, OK: false, Detail: fmt.Sprintf("%s missing — run `ollama pull %s`", model, model)}
}

// rerankerCheck is informational: reranking is optional, so a missing or unset
// reranker never fails the overall verdict.
func rerankerCheck(model string, tags []string) check {
	if model == "" {
		return check{Name: "reranker", OK: true, Detail: "disabled (set `reranker` to a generate model to enable LLM-as-reranker)"}
	}
	if embed.HasModel(tags, model) {
		return check{Name: "reranker", OK: true, Detail: fmt.Sprintf("%s present", model)}
	}
	return check{Name: "reranker", OK: true, Detail: fmt.Sprintf("%s not found — reranking will fall back to vector order; `ollama pull %s` to enable", model, model)}
}

// embedDimCheck probes the embed model's actual output dimension and compares it
// to config, so a mismatch is caught (with the exact number to set) before
// indexing. Skipped if the embed model isn't present.
func embedDimCheck(ctx context.Context, cfg *config.Config, checker ollamaChecker, tags []string) check {
	if !embed.HasModel(tags, cfg.EmbedModel) {
		return check{Name: "embed_dim", OK: false, Detail: "cannot verify — embed model not present"}
	}
	vecs, err := checker.Embed(ctx, []string{"dimension probe"}, "")
	if err != nil || len(vecs) == 0 {
		return check{Name: "embed_dim", OK: false, Detail: fmt.Sprintf("could not probe embedding dimension: %v", err)}
	}
	got := len(vecs[0])
	if got != cfg.EmbedDim {
		return check{Name: "embed_dim", OK: false, Detail: fmt.Sprintf("%s outputs %d-dim vectors but embed_dim is %d; set `embed_dim: %d` and run `journal index --rebuild`", cfg.EmbedModel, got, cfg.EmbedDim, got)}
	}
	return check{Name: "embed_dim", OK: true, Detail: fmt.Sprintf("%d matches %s", got, cfg.EmbedModel)}
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

// transcriptsCheck confirms the transcript landing zone exists and is writable.
func transcriptsCheck(cfg *config.Config) check {
	dir := cfg.TranscriptsAbsPath()
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return check{Name: "transcripts", OK: false, Detail: dir + " missing — run `journal init` to scaffold it"}
	}
	if err != nil {
		return check{Name: "transcripts", OK: false, Detail: err.Error()}
	}
	if !info.IsDir() {
		return check{Name: "transcripts", OK: false, Detail: dir + " is not a directory"}
	}
	probe := filepath.Join(dir, ".journal-write-probe")
	if werr := os.WriteFile(probe, []byte("ok"), 0o644); werr != nil {
		return check{Name: "transcripts", OK: false, Detail: dir + " is not writable: " + werr.Error()}
	}
	_ = os.Remove(probe)
	return check{Name: "transcripts", OK: true, Detail: dir + " (writable)"}
}

// quillCheck reports on the Quill database (informational — never fails the
// verdict, since Quill is macOS/Windows-only and optional).
func quillCheck(ctx context.Context, cfg *config.Config) check {
	if !cfg.Quill.Enabled {
		return check{Name: "quill", OK: true, Detail: "disabled (quill.enabled: false)"}
	}
	dbPath := cfg.QuillDBPath()
	if dbPath == "" {
		return check{Name: "quill", OK: true, Detail: "no database configured — Quill runs on macOS/Windows only; use .qm import on Linux"}
	}
	if _, err := os.Stat(dbPath); err != nil {
		return check{Name: "quill", OK: true, Detail: fmt.Sprintf("no database at %s (Quill not installed here?) — set quill.db_path or use .qm import", dbPath)}
	}
	r, err := quill.Open(dbPath)
	if err != nil {
		return check{Name: "quill", OK: true, Detail: "found but unreadable: " + err.Error()}
	}
	defer r.Close()
	count, cerr := r.Count(ctx)
	if cerr != nil {
		return check{Name: "quill", OK: true, Detail: "found at " + dbPath + " but query failed: " + cerr.Error()}
	}
	synced, _ := quill.LoadWatermark(filepath.Join(cfg.Root(), config.JournalDir))
	pending := "up to date"
	if !synced.IsZero() {
		pending = "last synced " + synced.UTC().Format("2006-01-02")
	} else if count > 0 {
		pending = fmt.Sprintf("%d meeting(s) not yet synced — run `journal quill-sync`", count)
	}
	return check{Name: "quill", OK: true, Detail: fmt.Sprintf("%s: %d meeting(s); %s", dbPath, count, pending)}
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
