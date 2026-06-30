package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/index"
	jlog "github.com/ericmann/journal/internal/log"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/ericmann/journal/internal/synth"
	"github.com/spf13/cobra"
)

var logText string

var logCmd = &cobra.Command{
	Use:   "log [audio.wav]",
	Short: "Capture a voice note (--text for typed input; pass an audio file to transcribe)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if len(args) == 1 {
			// Audio file path provided — transcribe then land.
			return runLogAudio(cmd.Context(), cfg, newEmbedder(cfg), newTranscriber(cfg), nil, args[0], cmd.OutOrStdout())
		}

		if strings.TrimSpace(logText) == "" {
			return fmt.Errorf("--text is required when no audio file is provided")
		}
		return runLogText(cmd.Context(), cfg, newEmbedder(cfg), nil, logText, cmd.OutOrStdout())
	},
}

// newTranscriber builds the live Transcriber from cfg. Tests inject a
// FakeTranscriber directly into runLogAudio instead of calling this.
func newTranscriber(cfg *config.Config) jlog.Transcriber {
	return jlog.NewWhisperCPP(cfg.LogTranscriberModelDirAbs(), cfg.Log.Transcriber.Model)
}

// runLogAudio orchestrates the transcribe→shape→assemble→land→index pipeline
// for an audio file argument. tr is injectable for tests (pass nil to build
// from cfg). client is injectable for synthesis shaping (pass nil to build
// from cfg).
func runLogAudio(ctx context.Context, cfg *config.Config, e embed.Embedder, tr jlog.Transcriber, client synth.Client, audioPath string, out io.Writer) error {
	// Validate the audio file exists.
	if _, err := os.Stat(audioPath); err != nil {
		return fmt.Errorf("audio file %q not found: %w", audioPath, err)
	}

	capturedAt := now()

	// Transcribe. On error: notify, keep the WAV, return so the user can retry.
	text, durationSec, err := tr.Transcribe(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("transcription failed (audio kept, retryable): %w", err)
	}

	// Empty/silent transcript — skip the pipeline.
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(out, "  (empty transcript — nothing to log)")
		return nil
	}

	transcriberName := tr.Name()

	// Build synthesis client for shaping (unless disabled or already provided).
	var shaped bool
	var sr jlog.ShapeResult

	if cfg.Log.Shaping.Enabled {
		if client == nil {
			if c, err := synthClient(cfg); err == nil {
				client = c
			}
		}
		voiceProfile := readVoiceProfile(cfg)
		var err error
		sr, shaped, err = jlog.Shape(ctx, client, cfg.ActiveSynthModel(), cfg.SynthMaxTokens,
			text, voiceProfile, cfg.LocalOnly)
		if err != nil {
			fmt.Fprintf(out, "  (shaping failed: %v; landing raw transcript)\n", err)
		}
	}

	// Assemble the voice note document.
	in := jlog.AssembleInput{
		RawText:           text,
		DurationSec:       durationSec,
		Transcriber:       transcriberName,
		KeepRawTranscript: cfg.Log.Shaping.KeepRawTranscript,
		CapturedAt:        capturedAt,
	}
	if shaped {
		in.Title = sr.Title
		in.Summary = sr.Summary
		in.Body = sr.Body
		in.Tags = sr.Tags
		in.Markers = sr.Markers
	}
	doc := jlog.Assemble(in)

	// Compute filename and paths.
	filename := jlog.Filename(capturedAt, in.Title, text)
	absDir := cfg.LogAbsPath()
	relPath := filepath.ToSlash(filepath.Join(cfg.LogRelPath(), filename))

	// Land the note.
	absPath, err := jlog.Land(absDir, filename, []byte(doc))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "logged: %s\n", relPath)

	// Optional daily backlink.
	if cfg.Log.Landing.BacklinkDaily {
		dailyPath := note.DailyPath(cfg.Root(), capturedAt)
		if berr := jlog.AppendBacklink(dailyPath, relPath, capturedAt); berr != nil {
			fmt.Fprintf(out, "  (backlink failed: %v)\n", berr)
		}
	}

	// Index the note (non-fatal on failure — note is already landed).
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		fmt.Fprintf(out, "  (index skipped: %v — run `journal index` to index later)\n", err)
		return nil
	}
	defer s.Close()

	mtime := capturedAt
	if fi, err := os.Stat(absPath); err == nil {
		mtime = fi.ModTime()
	}
	ix := index.NewIndexer(s, e)
	st, err := jlog.IndexVoice(ctx, ix, relPath, doc, mtime)
	if err != nil {
		fmt.Fprintf(out, "  (index failed: %v — run `journal index` to index later)\n", err)
		return nil
	}
	fmt.Fprintf(out, "  indexed: %d chunk(s) embedded\n", st.Embedded)
	return nil
}

// runLogText orchestrates the shape→assemble→land→index pipeline for --text input.
// When client is nil and shaping is enabled, a synthesis client is built from cfg.
func runLogText(ctx context.Context, cfg *config.Config, e embed.Embedder, client synth.Client, rawText string, out io.Writer) error {
	capturedAt := now()

	// Build synthesis client for shaping (unless disabled or already provided).
	var shaped bool
	var sr jlog.ShapeResult

	if cfg.Log.Shaping.Enabled {
		if client == nil {
			// Attempt to build a synthesis client; failure is non-fatal (raw fallback).
			if c, err := synthClient(cfg); err == nil {
				client = c
			}
		}
		voiceProfile := readVoiceProfile(cfg)
		var err error
		sr, shaped, err = jlog.Shape(ctx, client, cfg.ActiveSynthModel(), cfg.SynthMaxTokens,
			rawText, voiceProfile, cfg.LocalOnly)
		if err != nil {
			// Non-fatal: warn and fall back to raw land.
			fmt.Fprintf(out, "  (shaping failed: %v; landing raw text)\n", err)
		}
	}

	// Assemble the voice note document.
	in := jlog.AssembleInput{
		RawText:           rawText,
		DurationSec:       0,
		Transcriber:       "text",
		KeepRawTranscript: cfg.Log.Shaping.KeepRawTranscript,
		CapturedAt:        capturedAt,
	}
	if shaped {
		in.Title = sr.Title
		in.Summary = sr.Summary
		in.Body = sr.Body
		in.Tags = sr.Tags
		in.Markers = sr.Markers
	}
	doc := jlog.Assemble(in)

	// Compute filename and paths.
	filename := jlog.Filename(capturedAt, in.Title, rawText)
	absDir := cfg.LogAbsPath()
	relPath := filepath.ToSlash(filepath.Join(cfg.LogRelPath(), filename))

	// Land the note.
	absPath, err := jlog.Land(absDir, filename, []byte(doc))
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "logged: %s\n", relPath)

	// Optional daily backlink.
	if cfg.Log.Landing.BacklinkDaily {
		dailyPath := note.DailyPath(cfg.Root(), capturedAt)
		if berr := jlog.AppendBacklink(dailyPath, relPath, capturedAt); berr != nil {
			fmt.Fprintf(out, "  (backlink failed: %v)\n", berr)
		}
	}

	// Index the note (non-fatal on failure — note is already landed).
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		fmt.Fprintf(out, "  (index skipped: %v — run `journal index` to index later)\n", err)
		return nil
	}
	defer s.Close()

	mtime := capturedAt
	if fi, err := os.Stat(absPath); err == nil {
		mtime = fi.ModTime()
	}
	ix := index.NewIndexer(s, e)
	st, err := jlog.IndexVoice(ctx, ix, relPath, doc, mtime)
	if err != nil {
		fmt.Fprintf(out, "  (index failed: %v — run `journal index` to index later)\n", err)
		return nil
	}
	fmt.Fprintf(out, "  indexed: %d chunk(s) embedded\n", st.Embedded)
	return nil
}

func init() {
	logCmd.Flags().StringVar(&logText, "text", "", "typed text to capture as a voice note")
	rootCmd.AddCommand(logCmd)
}
