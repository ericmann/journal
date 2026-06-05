package synth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
)

// Options controls a synthesis run.
type Options struct {
	Kind    Kind
	Project string    // decisions: scope to this project slug (also the write target)
	Days    int       // stale: idle threshold (default 14)
	Date    time.Time // daily: the day to summarize (defaults to Now's date)
	Now     time.Time // reference time (defaults to time.Now)
	DryRun  bool      // print prompt + path; no network, no write
	Write   bool      // call the API and write output
}

// Result is the outcome of a synthesis run.
type Result struct {
	Kind         Kind
	Prompt       string
	OutputPath   string // repo-relative intended/actual output path
	Wrote        bool
	Text         string // model output (empty on dry-run)
	InputTokens  int
	OutputTokens int
}

// Runner gathers notes, assembles prompts, and runs synthesis jobs.
type Runner struct {
	store     *store.Store
	client    Client
	root      string
	model     string
	maxTokens int
	voice     string // author voice profile injected into prompts (may be "")
}

// NewRunner constructs a Runner. voiceProfile (may be "") is injected into
// synthesis prompts so drafts match the author's writing voice.
func NewRunner(s *store.Store, client Client, root, model string, maxTokens int, voiceProfile string) *Runner {
	return &Runner{store: s, client: client, root: root, model: model, maxTokens: maxTokens, voice: voiceProfile}
}

// Run executes the job described by opts. On DryRun it assembles the prompt and
// computes the output path but makes no network call and no file write.
func (r *Runner) Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Days <= 0 {
		// Meetings digests default to a tighter window than stale-thread review.
		if opts.Kind == KindMeetings {
			opts.Days = 7
		} else {
			opts.Days = 14
		}
	}

	var res Result
	res.Kind = opts.Kind
	var err error
	switch opts.Kind {
	case KindWeekly:
		res.Prompt, res.OutputPath, err = r.weekly(ctx, opts)
	case KindDaily:
		res.Prompt, res.OutputPath, err = r.daily(ctx, opts)
	case KindDecisions:
		res.Prompt, res.OutputPath, err = r.decisions(ctx, opts)
	case KindStale:
		res.Prompt, res.OutputPath, err = r.stale(ctx, opts)
	case KindMeetings:
		res.Prompt, res.OutputPath, err = r.meetings(ctx, opts)
	default:
		return res, fmt.Errorf("unknown synth kind %q (want weekly|daily|meetings|decisions|stale)", opts.Kind)
	}
	if err != nil {
		return res, err
	}

	if opts.DryRun || !opts.Write {
		// Dry-run (and the safe default): no API call, no write.
		return res, nil
	}

	resp, err := r.client.Complete(ctx, Request{Model: r.model, MaxTokens: r.maxTokens, Prompt: res.Prompt})
	if err != nil {
		return res, fmt.Errorf("synthesis API call failed: %w", err)
	}
	res.Text = resp.Text
	res.InputTokens = resp.InputTokens
	res.OutputTokens = resp.OutputTokens

	if err := r.write(opts, res); err != nil {
		return res, err
	}
	res.Wrote = true
	return res, nil
}

func (r *Runner) weekly(ctx context.Context, opts Options) (prompt, outPath string, err error) {
	label := isoWeekLabel(opts.Now)
	start := isoWeekStart(opts.Now)
	chunks, err := r.store.Recent(ctx, store.Filter{Since: start}, 0)
	if err != nil {
		return "", "", err
	}
	chunks = chronological(chunks)
	prompt = AssembleWeekly(label, r.voice, chunks)
	outPath = filepath.ToSlash(filepath.Join("reflections", label+".md"))
	return prompt, outPath, nil
}

func (r *Runner) daily(ctx context.Context, opts Options) (prompt, outPath string, err error) {
	day := opts.Date
	if day.IsZero() {
		day = opts.Now
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	end := start.AddDate(0, 0, 1)
	label := start.Format("2006-01-02")

	// store.Recent filters CreatedAt >= start; drop anything from the next day so
	// the window is exactly [start, end).
	all, err := r.store.Recent(ctx, store.Filter{Since: start}, 0)
	if err != nil {
		return "", "", err
	}
	var chunks []store.Chunk
	for _, c := range all {
		// store.Recent already filters CreatedAt >= start (excluding undated
		// chunks); keep only those before the next day's midnight.
		if !c.CreatedAt.IsZero() && c.CreatedAt.Before(end) {
			chunks = append(chunks, c)
		}
	}
	chunks = chronological(chunks)
	prompt = AssembleDaily(label, r.voice, chunks)
	outPath = filepath.ToSlash(filepath.Join("reflections", "daily-"+label+".md"))
	return prompt, outPath, nil
}

func (r *Runner) meetings(ctx context.Context, opts Options) (prompt, outPath string, err error) {
	start := opts.Now.AddDate(0, 0, -opts.Days)
	chunks, err := r.store.Recent(ctx, store.Filter{Source: store.SourceTranscript, Since: start}, 0)
	if err != nil {
		return "", "", err
	}
	chunks = chronological(chunks)
	prompt = AssembleMeetings(opts.Days, r.voice, chunks)
	outPath = filepath.ToSlash(filepath.Join("reflections", "meetings-"+opts.Now.Format("2006-01-02")+".md"))
	return prompt, outPath, nil
}

func (r *Runner) decisions(ctx context.Context, opts Options) (prompt, outPath string, err error) {
	scope := "all projects"
	f := store.Filter{Markers: []string{note.MarkerDecision}}
	if opts.Project != "" {
		scope = opts.Project
		f.Project = opts.Project
	}
	chunks, err := r.store.Recent(ctx, f, 0)
	if err != nil {
		return "", "", err
	}
	chunks = chronological(chunks)
	prompt = AssembleDecisions(scope, r.voice, chunks)
	if opts.Project != "" {
		outPath = filepath.ToSlash(filepath.Join("projects", opts.Project, "_index.md"))
	} else {
		outPath = filepath.ToSlash(filepath.Join("reflections", "decisions-"+opts.Now.Format("2006-01-02")+".md"))
	}
	return prompt, outPath, nil
}

func (r *Runner) stale(ctx context.Context, opts Options) (prompt, outPath string, err error) {
	infos, err := r.store.Projects(ctx)
	if err != nil {
		return "", "", err
	}
	cutoff := opts.Now.AddDate(0, 0, -opts.Days)
	var lines []string
	for _, pi := range infos {
		stale := pi.LastActivity.IsZero() || pi.LastActivity.Before(cutoff)
		if !stale {
			continue
		}
		last := "never"
		if !pi.LastActivity.IsZero() {
			last = pi.LastActivity.Format("2006-01-02")
		}
		lines = append(lines, fmt.Sprintf("%s — last activity %s, %d notes, %d open question(s)",
			pi.Slug, last, pi.Chunks, pi.OpenQuestions))
	}
	prompt = AssembleStale(opts.Days, r.voice, lines)
	outPath = filepath.ToSlash(filepath.Join("reflections", "stale-"+opts.Now.Format("2006-01-02")+".md"))
	return prompt, outPath, nil
}

// write persists the synthesis output. Weekly/stale write a new file (refusing
// to clobber an existing one — those may hold human edits). Decisions append a
// clearly-marked rollup block to the project _index.md, never mutating existing
// content.
func (r *Runner) write(opts Options, res Result) error {
	abs := filepath.Join(r.root, filepath.FromSlash(res.OutputPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}

	if opts.Kind == KindDecisions && opts.Project != "" {
		return appendRollup(abs, opts.Now, res.Text)
	}

	header := draftHeader(opts.Kind, opts.Now, dailyDay(opts))
	return writeNewFile(abs, header+res.Text+"\n", res.OutputPath)
}

// dailyDay resolves the day a daily summary covers: --date if given, else Now.
func dailyDay(opts Options) time.Time {
	if !opts.Date.IsZero() {
		return opts.Date
	}
	return opts.Now
}

// draftHeader builds the file header. now is when the draft was generated; day is
// the subject day for a daily summary (ignored by other kinds).
func draftHeader(kind Kind, now, day time.Time) string {
	var title string
	switch kind {
	case KindWeekly:
		title = "# Weekly reflection — " + isoWeekLabel(now)
	case KindDaily:
		title = "# Daily summary — " + day.Format("2006-01-02")
	case KindStale:
		title = "# Stale-thread review — " + now.Format("2006-01-02")
	case KindMeetings:
		title = "# Meeting digest — " + now.Format("2006-01-02")
	default:
		title = "# Decision rollup — " + now.Format("2006-01-02")
	}
	return fmt.Sprintf("%s\n\n> **DRAFT** — generated by `journal synth %s` on %s. Edit before sharing.\n\n",
		title, kind, now.Format("2006-01-02"))
}

func writeNewFile(abs, content, relForMsg string) error {
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("%s already exists; edit it or remove it to regenerate", relForMsg)
	}
	return os.WriteFile(abs, []byte(content), 0o644)
}

// rollupStart/rollupEnd delimit a generated block in a project _index.md so it
// is unambiguously machine-generated and never confused with hand-written notes.
const rollupStart = "<!-- journal:decisions-rollup generated="
const rollupEnd = "<!-- /journal:decisions-rollup -->"

func appendRollup(abs string, now time.Time, text string) error {
	block := fmt.Sprintf("%s%s -->\n## Decision rollup — %s\n\n%s\n%s\n",
		rollupStart, now.Format("2006-01-02"), now.Format("2006-01-02"), strings.TrimRight(text, "\n"), rollupEnd)

	existing, err := os.ReadFile(abs)
	if os.IsNotExist(err) {
		// Create a minimal _index.md with the rollup appended.
		head := "# " + projectTitle(abs) + "\n\n"
		return os.WriteFile(abs, []byte(head+block), 0o644)
	}
	if err != nil {
		return err
	}
	// Append-only: preserve every existing byte; add one blank-line separator.
	trimmed := strings.TrimRight(string(existing), "\n")
	return os.WriteFile(abs, []byte(trimmed+"\n\n"+block), 0o644)
}

func projectTitle(abs string) string {
	// .../projects/<slug>/_index.md -> <slug>
	return filepath.Base(filepath.Dir(abs))
}

// chronological returns chunks oldest-first (store.Recent yields newest-first),
// which reads more naturally in a synthesis prompt.
func chronological(chunks []store.Chunk) []store.Chunk {
	out := make([]store.Chunk, len(chunks))
	for i, c := range chunks {
		out[len(chunks)-1-i] = c
	}
	return out
}

func isoWeekLabel(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// isoWeekStart returns midnight on the Monday of t's ISO week.
func isoWeekStart(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7 // days since Monday (Mon=0)
	d := t.AddDate(0, 0, -offset)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, t.Location())
}
