package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/editor"
	"github.com/ericmann/journal/internal/note"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show [date|path]",
	Short: "Render a day's notes (or any note file) in the terminal",
	Long: "show renders a note file with formatting (plain markdown when piped). The\n" +
		"argument is a date (YYYY-MM-DD, today, yesterday) resolving to that daily file,\n" +
		"or a repo-relative path. Defaults to today.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		arg := ""
		if len(args) == 1 {
			arg = args[0]
		}
		abs, rel, err := resolveNotePath(cfg, arg)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(abs)
		if os.IsNotExist(err) {
			fmt.Fprintf(cmd.OutOrStdout(), "no notes at %s\n", rel)
			return nil
		}
		if err != nil {
			return err
		}
		renderMarkdown(cmd.OutOrStdout(), string(data))
		return nil
	},
}

var todayJSON bool

// todayReport is the --json shape for the today dashboard.
type todayReport struct {
	Date     string    `json:"date"`
	Path     string    `json:"path"`
	Notes    bool      `json:"notes"` // whether the daily file exists
	Todos    []Result  `json:"todos"`
	Meetings []Meeting `json:"meetings"`
}

var todayCmd = &cobra.Command{
	Use:   "today",
	Short: "Your day at a glance: today's notes, open todos, today's meetings",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return renderError(out, err, todayJSON)
		}
		rep, notesMD, err := gatherToday(cmd.Context(), cfg)
		if err != nil {
			return renderError(out, err, todayJSON)
		}
		if todayJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}
		renderMarkdown(out, composeToday(rep, notesMD))
		return nil
	},
}

// gatherToday collects the dashboard data: today's daily file (raw markdown),
// open todos (top 10), and today's meetings.
func gatherToday(ctx context.Context, cfg *config.Config) (todayReport, string, error) {
	t := now()
	abs := note.DailyPath(cfg.Root(), t)
	rel, _ := filepath.Rel(cfg.Root(), abs)
	rep := todayReport{Date: t.Format("2006-01-02"), Path: filepath.ToSlash(rel)}

	var notesMD string
	if data, err := os.ReadFile(abs); err == nil {
		rep.Notes = true
		notesMD = string(data)
	}

	todos, err := listTodos(ctx, cfg, []string{note.MarkerTodo}, "", "")
	if err != nil {
		return rep, "", err
	}
	if len(todos) > 10 {
		todos = todos[:10]
	}
	rep.Todos = todos

	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	meetings, err := recentMeetings(ctx, cfg, midnight, 20)
	if err != nil {
		return rep, "", err
	}
	rep.Meetings = meetings
	return rep, notesMD, nil
}

// composeToday assembles the dashboard as one markdown doc so glamour styles it
// uniformly. Empty sections are omitted.
func composeToday(rep todayReport, notesMD string) string {
	var b strings.Builder
	if rep.Notes {
		b.WriteString(strings.TrimRight(notesMD, "\n"))
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "# %s\n\n_No notes yet today — `journal capture \"…\"` to start._\n", rep.Date)
	}
	if len(rep.Todos) > 0 {
		fmt.Fprintf(&b, "\n---\n\n## Open todos (%d)\n\n", len(rep.Todos))
		for _, r := range rep.Todos {
			fmt.Fprintf(&b, "- [ ] %s · `%s`\n", r.Snippet, r.Citation())
		}
	}
	if len(rep.Meetings) > 0 {
		fmt.Fprintf(&b, "\n---\n\n## Today's meetings (%d)\n\n", len(rep.Meetings))
		for _, m := range rep.Meetings {
			fmt.Fprintf(&b, "- **%s** · `%s`\n", m.Title, m.Filename)
		}
	}
	return b.String()
}

// openPathEditor is the seam for `journal edit` so tests can fake the editor.
var openPathEditor = editor.OpenPath

var editCmd = &cobra.Command{
	Use:   "edit [date]",
	Short: "Open a daily note file in your editor (created if needed)",
	Long: "edit opens the daily file for the given date (YYYY-MM-DD, today, yesterday;\n" +
		"default today) in your editor — the `editor` config key, then $JOURNAL_EDITOR,\n" +
		"$VISUAL, $EDITOR, then nano — creating it with its date header if missing. The\n" +
		"change is auto-committed when you close the editor; the watcher re-indexes.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		arg := ""
		if len(args) == 1 {
			arg = args[0]
		}
		abs, rel, err := resolveNotePath(cfg, arg)
		if err != nil {
			return err
		}
		// Create the daily skeleton if this is the first note of that day.
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return err
			}
			day, derr := dateForArg(arg)
			if derr != nil {
				day = now()
			}
			if err := os.WriteFile(abs, []byte(note.DailyH1(day)+"\n"), 0o644); err != nil {
				return err
			}
		}
		if err := openPathEditor(editor.Resolve(cfg.Editor), abs); err != nil {
			return err
		}
		fmt.Fprintf(out, "edited %s\n", rel)
		committed, cerr := autoCommitCapture(cfg, cfg.Root(), now())
		switch {
		case cerr != nil:
			fmt.Fprintf(out, "  (auto-commit skipped: %v)\n", cerr)
		case committed:
			fmt.Fprintln(out, "  committed ✓")
		}
		return nil
	},
}

// resolveNotePath maps a date or repo-relative path argument to the note file.
// "" and "today" → today's daily file; "yesterday" and YYYY-MM-DD likewise; any
// other argument is treated as a repo-relative path.
func resolveNotePath(cfg *config.Config, arg string) (abs, rel string, err error) {
	if day, derr := dateForArg(arg); derr == nil {
		abs = note.DailyPath(cfg.Root(), day)
		r, _ := filepath.Rel(cfg.Root(), abs)
		return abs, filepath.ToSlash(r), nil
	}
	rel = filepath.ToSlash(strings.TrimPrefix(arg, "./"))
	abs = filepath.Join(cfg.Root(), filepath.FromSlash(rel))
	// Keep the path inside the repo.
	if r, rerr := filepath.Rel(cfg.Root(), abs); rerr != nil || strings.HasPrefix(r, "..") {
		return "", "", fmt.Errorf("path %q is outside the journal repo", arg)
	}
	return abs, rel, nil
}

// dateForArg parses "", "today", "yesterday", or YYYY-MM-DD into a date; any
// other value errors (the caller then treats the argument as a path).
func dateForArg(arg string) (time.Time, error) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "", "today":
		return now(), nil
	case "yesterday":
		return now().AddDate(0, 0, -1), nil
	}
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(arg), time.Local)
}

func init() {
	todayCmd.Flags().BoolVar(&todayJSON, "json", false, "emit JSON ({date, path, todos, meetings})")
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(todayCmd)
	rootCmd.AddCommand(editCmd)
}
