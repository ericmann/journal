package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

var statsJSON bool

// TagCount is one entry in the top-tags list.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// StatsReport is the stable --json shape for `journal stats` (also the TUI tab).
type StatsReport struct {
	NoteChunks       int        `json:"note_chunks"`
	TranscriptChunks int        `json:"transcript_chunks"`
	Projects         int        `json:"projects"`
	Meetings         int        `json:"meetings"` // distinct transcript files
	OpenTodos        int        `json:"open_todos"`
	DoneTodos        int        `json:"done_todos"`
	Decisions        int        `json:"decisions"`
	Questions        int        `json:"questions"`
	CurrentStreak    int        `json:"current_streak_days"`
	LongestStreak    int        `json:"longest_streak_days"`
	NotesThisWeek    int        `json:"notes_this_week"`
	NotesLastWeek    int        `json:"notes_last_week"`
	ActiveDays       int        `json:"active_days"`
	FirstNote        string     `json:"first_note,omitempty"` // YYYY-MM-DD
	TopTags          []TagCount `json:"top_tags"`
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Capture stats: volume, streaks, todos, top tags",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		cfg, err := loadConfig()
		if err != nil {
			return renderError(out, err, statsJSON)
		}
		rep, err := gatherStats(cmd.Context(), cfg, now())
		if err != nil {
			return renderError(out, err, statsJSON)
		}
		if statsJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(rep)
		}
		renderStats(out, rep)
		return nil
	},
}

// gatherStats aggregates the whole index in one pass. At journal scale (10²–10⁴
// chunks) this is milliseconds; shared by the CLI and the TUI Stats tab.
func gatherStats(ctx context.Context, cfg *config.Config, ref time.Time) (StatsReport, error) {
	s, err := store.Open(cfg.StoreAbsPath(), cfg.EmbedDim)
	if err != nil {
		return StatsReport{}, err
	}
	defer s.Close()
	chunks, err := s.Recent(ctx, store.Filter{}, 0)
	if err != nil {
		return StatsReport{}, err
	}

	var rep StatsReport
	projects := map[string]bool{}
	meetings := map[string]bool{}
	tagCounts := map[string]int{}
	noteDays := map[string]bool{} // YYYY-MM-DD with ≥1 note chunk
	var firstDay string

	weekStart := startOfDay(ref).AddDate(0, 0, -int((ref.Weekday()+6)%7)) // Monday
	lastWeekStart := weekStart.AddDate(0, 0, -7)

	for _, c := range chunks {
		if c.Source == store.SourceTranscript {
			rep.TranscriptChunks++
			meetings[c.Path] = true
		} else {
			rep.NoteChunks++
			if !c.CreatedAt.IsZero() {
				day := c.CreatedAt.Local().Format("2006-01-02")
				noteDays[day] = true
				if firstDay == "" || day < firstDay {
					firstDay = day
				}
				if !c.CreatedAt.Before(weekStart) {
					rep.NotesThisWeek++
				} else if !c.CreatedAt.Before(lastWeekStart) {
					rep.NotesLastWeek++
				}
			}
			for _, tg := range c.Tags {
				tagCounts[tg]++
			}
		}
		if c.Project != "" {
			projects[c.Project] = true
		}
		for _, m := range c.Markers {
			switch m {
			case note.MarkerTodo:
				rep.OpenTodos++
			case note.MarkerDone:
				rep.DoneTodos++
			case note.MarkerDecision:
				rep.Decisions++
			case note.MarkerQuestion:
				rep.Questions++
			}
		}
	}
	rep.Projects = len(projects)
	rep.Meetings = len(meetings)
	rep.ActiveDays = len(noteDays)
	rep.FirstNote = firstDay
	rep.CurrentStreak, rep.LongestStreak = streaks(noteDays, ref)
	rep.TopTags = topTags(tagCounts, 10)
	return rep, nil
}

// streaks computes the current streak (consecutive days with notes ending today
// or yesterday — an in-progress day doesn't break it) and the longest ever.
func streaks(days map[string]bool, ref time.Time) (current, longest int) {
	if len(days) == 0 {
		return 0, 0
	}
	// Current: walk backward from today; allow the streak to start yesterday.
	d := startOfDay(ref)
	if !days[d.Format("2006-01-02")] {
		d = d.AddDate(0, 0, -1)
	}
	for days[d.Format("2006-01-02")] {
		current++
		d = d.AddDate(0, 0, -1)
	}
	// Longest: sort the days and scan for consecutive runs.
	sorted := make([]string, 0, len(days))
	for k := range days {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	run := 1
	longest = 1
	for i := 1; i < len(sorted); i++ {
		prev, _ := time.Parse("2006-01-02", sorted[i-1])
		cur, _ := time.Parse("2006-01-02", sorted[i])
		if cur.Sub(prev) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
	}
	return current, longest
}

func topTags(counts map[string]int, n int) []TagCount {
	out := make([]TagCount, 0, len(counts))
	for t, c := range counts {
		out = append(out, TagCount{Tag: t, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// renderStats prints the aligned text report (markdown-ish; reused raw by the TUI).
func renderStats(out io.Writer, r StatsReport) {
	fmt.Fprint(out, statsText(r))
}

func statsText(r StatsReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Notes\n")
	fmt.Fprintf(&b, "  note chunks        %d\n", r.NoteChunks)
	fmt.Fprintf(&b, "  transcript chunks  %d (%d meetings)\n", r.TranscriptChunks, r.Meetings)
	fmt.Fprintf(&b, "  projects           %d\n", r.Projects)
	if r.FirstNote != "" {
		fmt.Fprintf(&b, "  active days        %d (since %s)\n", r.ActiveDays, r.FirstNote)
	}
	fmt.Fprintf(&b, "\nMomentum\n")
	fmt.Fprintf(&b, "  current streak     %d day(s)\n", r.CurrentStreak)
	fmt.Fprintf(&b, "  longest streak     %d day(s)\n", r.LongestStreak)
	fmt.Fprintf(&b, "  notes this week    %d (last week %d)\n", r.NotesThisWeek, r.NotesLastWeek)
	fmt.Fprintf(&b, "\nMarkers\n")
	fmt.Fprintf(&b, "  open todos         %d\n", r.OpenTodos)
	fmt.Fprintf(&b, "  done todos         %d\n", r.DoneTodos)
	fmt.Fprintf(&b, "  decisions          %d\n", r.Decisions)
	fmt.Fprintf(&b, "  questions          %d\n", r.Questions)
	if len(r.TopTags) > 0 {
		fmt.Fprintf(&b, "\nTop tags\n")
		for _, t := range r.TopTags {
			fmt.Fprintf(&b, "  #%-16s %d\n", t.Tag, t.Count)
		}
	}
	return b.String()
}

func init() {
	statsCmd.Flags().BoolVar(&statsJSON, "json", false, "emit JSON (stable StatsReport schema)")
	rootCmd.AddCommand(statsCmd)
}
