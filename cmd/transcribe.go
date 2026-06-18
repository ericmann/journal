package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/synth"
	"github.com/ericmann/journal/internal/transcribe"
	"github.com/spf13/cobra"
)

var (
	transcribeTitle     string
	transcribeDate      string
	transcribeNoSummary bool
)

var transcribeCmd = &cobra.Command{
	Use:   "transcribe <whisperx.json>",
	Short: "Ingest a WhisperX JSON transcript: render Markdown, summarize, and index",
	Long: "transcribe turns a WhisperX JSON file (from a non-Quill recording) into an\n" +
		"indexed transcript. It renders speaker-labeled Markdown into the transcripts\n" +
		"landing zone, generates a `## Notes` summary with the configured synth provider\n" +
		"(so retrieval hits a concise entry point instead of trawling the whole meeting),\n" +
		"and indexes it. Produce the JSON first with scripts/transcribe.py — see\n" +
		"docs/TRANSCRIBE.md for the full audio/video → JSON → journal pipeline.\n\n" +
		"The summary uses synth_provider; if it's unavailable (no key / Ollama down) the\n" +
		"transcript is still ingested, just without the summary. --no-summary skips it.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		jsonPath := args[0]

		date := now()
		if transcribeDate != "" {
			date, err = time.ParseInLocation("2006-01-02", transcribeDate, time.Local)
			if err != nil {
				return fmt.Errorf("invalid --date %q (want YYYY-MM-DD)", transcribeDate)
			}
		}
		title := strings.TrimSpace(transcribeTitle)
		if title == "" {
			title = titleFromPath(jsonPath)
		}

		// Build the summary client unless suppressed. A missing provider/key is
		// not fatal — we ingest the transcript without a summary and say so.
		var client synth.Client
		if !transcribeNoSummary {
			c, cerr := synthClient(cfg)
			if cerr != nil {
				fmt.Fprintf(out, "summary disabled: %v\n", cerr)
			} else {
				client = c
			}
		}

		return hintOllama(cfg, runTranscribe(cmd.Context(), cfg, newEmbedder(cfg), client,
			transcribeOptions{jsonPath: jsonPath, title: title, date: date}, out))
	},
}

type transcribeOptions struct {
	jsonPath string
	title    string
	date     time.Time
}

// runTranscribe parses the WhisperX JSON, optionally summarizes (client != nil),
// renders the transcript Markdown into the landing zone, dates the file to the
// meeting date, and indexes it. Embedder and client are injected so tests run
// without a network.
func runTranscribe(ctx context.Context, cfg *config.Config, e embed.Embedder, client synth.Client, opts transcribeOptions, out io.Writer) error {
	data, err := os.ReadFile(opts.jsonPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", opts.jsonPath, err)
	}
	segs, err := transcribe.ParseWhisperX(data)
	if err != nil {
		return err
	}

	notes := ""
	if client != nil {
		resp, serr := client.Complete(ctx, synth.Request{
			Model:     cfg.ActiveSynthModel(),
			MaxTokens: cfg.SynthMaxTokens,
			Prompt:    transcribeSummaryPrompt(opts.title, transcribe.PlainText(segs)),
		})
		if serr != nil {
			fmt.Fprintf(out, "  (summary skipped: %v)\n", serr)
		} else {
			notes = strings.TrimSpace(resp.Text)
		}
	}

	var tags []string
	if t := strings.TrimSpace(cfg.Transcripts.Tag); t != "" {
		tags = []string{t}
	}
	md := transcribe.Render(opts.title, opts.date, segs, notes, tags)

	filename := transcribe.Filename(opts.date, opts.title)
	rel := filepath.ToSlash(filepath.Join(cfg.TranscriptsRelPath(), filename))
	abs := filepath.Join(cfg.TranscriptsAbsPath(), filename)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(abs, []byte(md), 0o644); err != nil {
		return err
	}
	// Transcripts take their date from the file mtime, so stamp it to the
	// meeting date (otherwise it'd look like it happened "now").
	if !opts.date.IsZero() {
		_ = os.Chtimes(abs, opts.date, opts.date)
	}

	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return err
	}
	defer s.Close()
	st, err := index.NewIndexer(s, e).IndexTranscript(ctx, rel, md, opts.date, cfg.Transcripts.Tag)
	if err != nil {
		return fmt.Errorf("indexing transcript: %w", err)
	}

	fmt.Fprintf(out, "transcribed %s\n", rel)
	fmt.Fprintf(out, "  %d segments, %d speaker(s); %d chunks embedded\n", len(segs), len(transcribe.Speakers(segs)), st.Embedded)
	if notes != "" {
		fmt.Fprintf(out, "  summary: %d chars via %s (%s)\n", len(notes), cfg.SynthProvider, cfg.ActiveSynthModel())
	} else {
		fmt.Fprintln(out, "  summary: none (search will rely on transcript windows only)")
	}
	return nil
}

// transcribeSummaryPrompt builds the summarization prompt. It asks for a compact,
// search-friendly summary and forbids invention.
func transcribeSummaryPrompt(title, transcript string) string {
	return "Summarize the following meeting transcript titled \"" + title + "\" for a searchable journal.\n" +
		"Write: a 2–3 sentence overview, then short bulleted sections for Key topics, " +
		"Decisions, and Action items (omit any section that has nothing). Be concise and " +
		"strictly factual — do not invent details or attendees.\n\nTranscript:\n\n" + transcript
}

// titleFromPath derives a human-ish title from a JSON filename stem when --title
// is omitted: "2026-06-02-acme-q2-planning.json" -> "2026 06 02 acme q2 planning".
func titleFromPath(p string) string {
	stem := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	stem = strings.NewReplacer("-", " ", "_", " ").Replace(stem)
	return strings.TrimSpace(stem)
}

func init() {
	transcribeCmd.Flags().StringVar(&transcribeTitle, "title", "", "meeting title (default: derived from the filename)")
	transcribeCmd.Flags().StringVar(&transcribeDate, "date", "", "meeting date YYYY-MM-DD (default: today); sets the transcript's timestamp")
	transcribeCmd.Flags().BoolVar(&transcribeNoSummary, "no-summary", false, "skip the AI-generated ## Notes summary")
	rootCmd.AddCommand(transcribeCmd)
}
