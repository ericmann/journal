package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Interactive dashboard: today, todos, search, recent, meetings, stats",
	Long: "tui is a full-screen dashboard over your journal. Tabs: Today (rendered\n" +
		"daily notes), Todos (press d to complete one), Search (semantic), Recent,\n" +
		"Meetings, Stats. tab/shift+tab or 1-6 switch tabs; enter opens a note; esc\n" +
		"goes back; r refreshes; q quits.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		m := newTUIModel(cfg, newEmbedder(cfg))
		_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(cmd.Context())).Run()
		return err
	},
}

// --- tabs ---

type tuiTab int

const (
	tabToday tuiTab = iota
	tabTodos
	tabSearch
	tabRecent
	tabMeetings
	tabStats
	tabCount
)

var tabNames = []string{"Today", "Todos", "Search", "Recent", "Meetings", "Stats"}

// --- list items ---

// entry is one row in any TUI list: a chunk/meeting with enough to open it.
type entry struct {
	title, desc string
	path        string // repo-relative file to open on enter
	citation    string // for done; empty for non-todos
}

func (e entry) Title() string       { return e.title }
func (e entry) Description() string { return e.desc }
func (e entry) FilterValue() string { return e.title + " " + e.desc }

func resultEntries(results []Result) []list.Item {
	items := make([]list.Item, len(results))
	for i, r := range results {
		title := r.Citation()
		if r.Heading != "" {
			title += "  " + r.Heading
		}
		items[i] = entry{title: title, desc: r.Snippet, path: r.Path, citation: r.Citation()}
	}
	return items
}

// --- messages ---

type dataMsg struct {
	todayMD  string
	today    todayReport
	todos    []Result
	recent   []Result
	meetings []Meeting
	stats    string
	err      error
}

type searchMsg struct {
	results []Result
	err     error
}

type doneMsg struct {
	res Result
	err error
}

type detailMsg struct {
	path    string
	content string
	err     error
}

// --- model ---

type tuiModel struct {
	cfg *config.Config
	e   embed.Embedder

	tab           tuiTab
	width, height int
	status        string

	todayVP  viewport.Model
	todos    list.Model
	recent   list.Model
	meetings list.Model
	results  list.Model
	input    textinput.Model
	typing   bool // search input focused
	statsVP  viewport.Model

	detail   viewport.Model
	inDetail bool
	ready    bool
}

func newTUIModel(cfg *config.Config, e embed.Embedder) tuiModel {
	newList := func(title string) list.Model {
		l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
		l.Title = title
		l.SetShowHelp(false)
		l.SetShowStatusBar(false)
		l.DisableQuitKeybindings()
		return l
	}
	ti := textinput.New()
	ti.Placeholder = "semantic search across your notes…"
	ti.CharLimit = 200
	return tuiModel{
		cfg:      cfg,
		e:        e,
		todos:    newList("Open todos"),
		recent:   newList("Recent notes"),
		meetings: newList("Meetings"),
		results:  newList("Results"),
		input:    ti,
		status:   "loading…",
	}
}

func (m tuiModel) Init() tea.Cmd { return m.loadAll() }

// loadAll gathers every tab's data in one async command.
func (m tuiModel) loadAll() tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		ctx := context.Background()
		var msg dataMsg
		rep, notesMD, err := gatherToday(ctx, cfg)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.today = rep
		msg.todayMD = composeToday(rep, notesMD)
		if msg.todos, err = listTodos(ctx, cfg, []string{note.MarkerTodo}, "", ""); err != nil {
			msg.err = err
			return msg
		}
		if msg.recent, err = listFromStore(ctx, cfg, store.Filter{}, 50); err != nil {
			msg.err = err
			return msg
		}
		if msg.meetings, err = recentMeetings(ctx, cfg, time.Time{}, 50); err != nil {
			msg.err = err
			return msg
		}
		st, err := gatherStats(ctx, cfg, now())
		if err != nil {
			msg.err = err
			return msg
		}
		msg.stats = statsText(st)
		return msg
	}
}

func (m tuiModel) searchCmd(query string) tea.Cmd {
	cfg, e := m.cfg, m.e
	return func() tea.Msg {
		scored, err := searchChunks(context.Background(), cfg, e, query, 10, store.Filter{})
		if err != nil {
			return searchMsg{err: err}
		}
		return searchMsg{results: resultsFromScored(scored)}
	}
}

func (m tuiModel) doneCmd(citation string) tea.Cmd {
	cfg, e := m.cfg, m.e
	return func() tea.Msg {
		res, err := completeTodo(context.Background(), cfg, e, citation, "", nil)
		return doneMsg{res: res, err: err}
	}
}

func (m tuiModel) openCmd(relPath string) tea.Cmd {
	cfg := m.cfg
	width := m.width - 4
	return func() tea.Msg {
		data, err := os.ReadFile(filepath.Join(cfg.Root(), filepath.FromSlash(relPath)))
		if err != nil {
			return detailMsg{path: relPath, err: err}
		}
		return detailMsg{path: relPath, content: renderMarkdownString(string(data), width)}
	}
}

// --- update ---

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		m.ready = true
		return m, nil

	case dataMsg:
		if msg.err != nil {
			m.status = "load error: " + msg.err.Error()
			return m, nil
		}
		m.todayVP.SetContent(renderMarkdownString(msg.todayMD, m.width-4))
		m.todos.SetItems(resultEntries(msg.todos))
		m.recent.SetItems(resultEntries(msg.recent))
		items := make([]list.Item, len(msg.meetings))
		for i, mt := range msg.meetings {
			items[i] = entry{
				title: mt.Title, desc: mt.Timestamp + "  " + mt.Snippet,
				path: m.cfg.TranscriptsRelPath() + "/" + mt.Filename,
			}
		}
		m.meetings.SetItems(items)
		m.statsVP.SetContent(msg.stats)
		m.status = fmt.Sprintf("%s · %d open todos · %d meetings", m.cfg.Root(), len(msg.todos), len(msg.meetings))
		return m, nil

	case searchMsg:
		if msg.err != nil {
			m.status = "search error: " + msg.err.Error()
			return m, nil
		}
		m.results.SetItems(resultEntries(msg.results))
		m.status = fmt.Sprintf("%d result(s)", len(msg.results))
		m.typing = false
		m.input.Blur()
		return m, nil

	case doneMsg:
		if msg.err != nil {
			m.status = "done error: " + msg.err.Error()
			return m, nil
		}
		m.status = "done ✓ " + msg.res.Citation()
		return m, m.loadAll()

	case detailMsg:
		if msg.err != nil {
			m.status = "open error: " + msg.err.Error()
			return m, nil
		}
		m.detail.SetContent(msg.content)
		m.detail.GotoTop()
		m.inDetail = true
		m.status = msg.path + " (esc to go back)"
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m.routeComponent(msg)
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Always-on keys.
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.inDetail {
		if key == "esc" || key == "q" {
			m.inDetail = false
			m.status = ""
			return m, nil
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}
	// While typing in the search box, keys go to the input (esc blurs).
	if m.tab == tabSearch && m.typing {
		switch key {
		case "esc":
			m.typing = false
			m.input.Blur()
			return m, nil
		case "enter":
			q := strings.TrimSpace(m.input.Value())
			if q == "" {
				return m, nil
			}
			m.status = "searching…"
			return m, m.searchCmd(q)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "tab":
		m.tab = (m.tab + 1) % tabCount
		return m.enterTab()
	case "shift+tab":
		m.tab = (m.tab + tabCount - 1) % tabCount
		return m.enterTab()
	case "1", "2", "3", "4", "5", "6":
		m.tab = tuiTab(key[0] - '1')
		return m.enterTab()
	case "r":
		m.status = "refreshing…"
		return m, m.loadAll()
	case "/":
		if m.tab == tabSearch {
			m.typing = true
			return m, m.input.Focus()
		}
	case "d":
		if m.tab == tabTodos {
			if e, ok := m.todos.SelectedItem().(entry); ok && e.citation != "" {
				m.status = "completing " + e.citation + "…"
				return m, m.doneCmd(e.citation)
			}
		}
	case "enter":
		if e, ok := m.selectedEntry(); ok && e.path != "" {
			return m, m.openCmd(e.path)
		}
	}
	return m.routeComponent(msg)
}

// enterTab runs per-tab focus side effects.
func (m tuiModel) enterTab() (tea.Model, tea.Cmd) {
	if m.tab == tabSearch && len(m.results.Items()) == 0 {
		m.typing = true
		return m, m.input.Focus()
	}
	m.typing = false
	m.input.Blur()
	return m, nil
}

func (m tuiModel) selectedEntry() (entry, bool) {
	var it list.Item
	switch m.tab {
	case tabTodos:
		it = m.todos.SelectedItem()
	case tabRecent:
		it = m.recent.SelectedItem()
	case tabMeetings:
		it = m.meetings.SelectedItem()
	case tabSearch:
		it = m.results.SelectedItem()
	default:
		return entry{}, false
	}
	e, ok := it.(entry)
	return e, ok
}

// routeComponent forwards non-global messages to the active tab's component.
func (m tuiModel) routeComponent(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.tab {
	case tabToday:
		m.todayVP, cmd = m.todayVP.Update(msg)
	case tabTodos:
		m.todos, cmd = m.todos.Update(msg)
	case tabRecent:
		m.recent, cmd = m.recent.Update(msg)
	case tabMeetings:
		m.meetings, cmd = m.meetings.Update(msg)
	case tabSearch:
		m.results, cmd = m.results.Update(msg)
	case tabStats:
		m.statsVP, cmd = m.statsVP.Update(msg)
	}
	return m, cmd
}

func (m *tuiModel) resize() {
	w, h := m.width, m.height
	contentH := h - 4 // tab bar + status + padding
	if contentH < 3 {
		contentH = 3
	}
	m.todayVP = viewport.New(w, contentH)
	m.statsVP = viewport.New(w, contentH)
	m.detail = viewport.New(w, contentH)
	for _, l := range []*list.Model{&m.todos, &m.recent, &m.meetings, &m.results} {
		l.SetSize(w, contentH-2)
	}
	m.input.Width = w - 8
}

// --- view ---

var (
	tabActiveStyle   = lipgloss.NewStyle().Bold(true).Underline(true).Padding(0, 1)
	tabInactiveStyle = lipgloss.NewStyle().Faint(true).Padding(0, 1)
	statusStyle      = lipgloss.NewStyle().Faint(true)
)

func (m tuiModel) View() string {
	if !m.ready {
		return "loading…"
	}
	var tabs []string
	for i, name := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if tuiTab(i) == m.tab {
			tabs = append(tabs, tabActiveStyle.Render(label))
		} else {
			tabs = append(tabs, tabInactiveStyle.Render(label))
		}
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	var body string
	if m.inDetail {
		body = m.detail.View()
	} else {
		switch m.tab {
		case tabToday:
			body = m.todayVP.View()
		case tabTodos:
			body = m.todos.View() + "\n" + statusStyle.Render("d: mark done · enter: open · r: refresh")
		case tabSearch:
			body = m.input.View() + "\n\n" + m.results.View() + "\n" + statusStyle.Render("/: edit query · enter: search/open")
		case tabRecent:
			body = m.recent.View()
		case tabMeetings:
			body = m.meetings.View()
		case tabStats:
			body = m.statsVP.View()
		}
	}

	footer := statusStyle.Render(truncateLine(m.status, m.width))
	help := statusStyle.Render("tab/1-6: switch · q: quit")
	return header + "\n\n" + body + "\n" + footer + "\n" + help
}

func truncateLine(s string, w int) string {
	if w > 3 && len(s) > w {
		return s[:w-1] + "…"
	}
	return s
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
