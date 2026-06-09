package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ericmann/journal/internal/config"
	"github.com/ericmann/journal/internal/embed"
	"github.com/ericmann/journal/internal/note"
	"github.com/ericmann/journal/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var mcpRepo string

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run an MCP server exposing journal retrieval to MCP clients (e.g. Claude Desktop)",
	Long: "mcp runs a Model Context Protocol server over stdio, exposing search, recent,\n" +
		"decisions, threads, and capture as tools that return the same stable JSON as\n" +
		"the CLI's --json. Point an MCP client (e.g. the Claude desktop app) at\n" +
		"`journal mcp` with the working directory set to your journal repo, or pass\n" +
		"--repo. Search/embedding still use the local Ollama configured in the repo.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// --repo wins; otherwise honor --journal-dir / $JOURNAL_DIR / cwd.
		start := mcpRepo
		if strings.TrimSpace(start) == "" {
			start = resolveStart()
		}
		cfg, err := loadConfigFrom(start)
		if err != nil {
			return err
		}
		return runMCP(cmd.Context(), cfg, newEmbedder(cfg))
	},
}

// --- Tool input schemas (json tags name the params; jsonschema tags describe). ---

type searchInput struct {
	Query   string   `json:"query" jsonschema:"the natural-language search query"`
	K       int      `json:"k,omitempty" jsonschema:"max results to return (default 5)"`
	Tag     []string `json:"tag,omitempty" jsonschema:"only chunks having all of these tags"`
	Project string   `json:"project,omitempty" jsonschema:"restrict to this project slug"`
	Since   string   `json:"since,omitempty" jsonschema:"only notes within this window, e.g. 2w, 14d, 36h"`
	Source  string   `json:"source,omitempty" jsonschema:"restrict to a source: notes | transcript | all (default all)"`
}

type meetingsInput struct {
	K     int    `json:"k,omitempty" jsonschema:"max meetings to return (default 20)"`
	Since string `json:"since,omitempty" jsonschema:"only meetings within this window, e.g. 2w"`
}

type todosInput struct {
	Done    bool   `json:"done,omitempty" jsonschema:"list completed (@done) items instead of open @todo items"`
	Project string `json:"project,omitempty" jsonschema:"restrict to this project slug"`
	Since   string `json:"since,omitempty" jsonschema:"only items within this window, e.g. 2w"`
}

type doneInput struct {
	Ref string `json:"ref" jsonschema:"the todo to complete: a citation from the todos tool (path:line) or a unique text fragment"`
}

type recentInput struct {
	Tag     []string `json:"tag,omitempty" jsonschema:"only chunks having all of these tags"`
	Project string   `json:"project,omitempty" jsonschema:"restrict to this project slug"`
	Since   string   `json:"since,omitempty" jsonschema:"only notes within this window, e.g. 1w"`
}

type decisionsInput struct {
	Project string `json:"project,omitempty" jsonschema:"restrict to this project slug"`
	Since   string `json:"since,omitempty" jsonschema:"only decisions within this window, e.g. 4w"`
}

type threadsInput struct {
	Stale bool `json:"stale,omitempty" jsonschema:"only threads idle longer than days"`
	Days  int  `json:"days,omitempty" jsonschema:"staleness threshold in days (default 14)"`
}

type captureInput struct {
	Text    string   `json:"text" jsonschema:"the note text to append"`
	Tags    []string `json:"tags,omitempty" jsonschema:"tags to attach"`
	Project string   `json:"project,omitempty" jsonschema:"capture into this project instead of the daily file"`
	Marker  string   `json:"marker,omitempty" jsonschema:"one of: decision, question, todo"`
}

// runMCP registers the tools and serves over stdio until the client disconnects.
func runMCP(ctx context.Context, cfg *config.Config, e embed.Embedder) error {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "journal",
		Title:   "journal — local developer journal",
		Version: version,
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search",
		Description: "Semantic search over the journal (embed → vector KNN → rerank). Returns results with path:line citations. Prefer this for 'find/recall what I noted about X'.",
	}, toolHandler(func(ctx context.Context, in searchInput) (string, error) {
		return mcpSearch(ctx, cfg, e, in)
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "recent",
		Description: "List the most recent notes (newest first). Use for 'what have I been working on lately'.",
	}, toolHandler(func(ctx context.Context, in recentInput) (string, error) {
		return mcpRecent(ctx, cfg, in)
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "decisions",
		Description: "List @decision notes (newest first). Use for 'what did I decide about X'.",
	}, toolHandler(func(ctx context.Context, in decisionsInput) (string, error) {
		return mcpDecisions(ctx, cfg, in)
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "threads",
		Description: "Summarize project threads and their activity. Use stale=true for neglected projects.",
	}, toolHandler(func(ctx context.Context, in threadsInput) (string, error) {
		return mcpThreads(ctx, cfg, in)
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "capture",
		Description: "Append a timestamped note (append-only, no embedding). Returns the file path written.",
	}, toolHandler(func(ctx context.Context, in captureInput) (string, error) {
		return mcpCapture(cfg, in)
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "meetings",
		Description: "List recent meeting transcripts (newest first): filename, timestamp, title, and a snippet. Use for 'what meetings do I have notes from'.",
	}, toolHandler(func(ctx context.Context, in meetingsInput) (string, error) {
		return mcpMeetings(ctx, cfg, in)
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "todos",
		Description: "List open @todo notes (newest first) with path:line citations, or completed items with done=true. Use for 'what's on my plate'.",
	}, toolHandler(func(ctx context.Context, in todosInput) (string, error) {
		return mcpTodos(ctx, cfg, in)
	}))

	mcp.AddTool(s, &mcp.Tool{
		Name:        "done",
		Description: "Complete an open @todo: rewrites its @todo marker to @done with today's date in the note file. ref is a citation from the todos tool (path:line) or a unique text fragment.",
	}, toolHandler(func(ctx context.Context, in doneInput) (string, error) {
		return mcpDone(ctx, cfg, e, in)
	}))

	return s.Run(ctx, &mcp.StdioTransport{})
}

// toolHandler adapts a (ctx, In) -> (jsonText, error) function into the SDK's
// typed handler, returning the JSON as text content and surfacing errors as a
// tool error result with the same {"error":...} shape the CLI uses.
func toolHandler[In any](fn func(context.Context, In) (string, error)) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		text, err := fn(ctx, in)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: errorJSON(err)}},
			}, nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	}
}

func errorJSON(err error) string {
	b, _ := json.Marshal(errorEnvelope{Error: err.Error()})
	return string(b)
}

func sinceFilter(base store.Filter, sinceStr string) (store.Filter, error) {
	d, err := parseSince(sinceStr)
	if err != nil {
		return base, err
	}
	if d > 0 {
		base.Since = now().Add(-d)
	}
	return base, nil
}

func mcpSearch(ctx context.Context, cfg *config.Config, e embed.Embedder, in searchInput) (string, error) {
	if strings.TrimSpace(in.Query) == "" {
		return "", fmt.Errorf("query must not be empty")
	}
	src, err := parseSourceFilter(in.Source)
	if err != nil {
		return "", err
	}
	f, err := sinceFilter(store.Filter{Tags: in.Tag, Project: in.Project, Source: src}, in.Since)
	if err != nil {
		return "", err
	}
	results, err := runSearch(ctx, cfg, e, in.Query, in.K, f)
	if err != nil {
		return "", err
	}
	return marshalResults(results)
}

func mcpTodos(ctx context.Context, cfg *config.Config, in todosInput) (string, error) {
	markers := []string{note.MarkerTodo}
	if in.Done {
		markers = []string{note.MarkerDone}
	}
	results, err := listTodos(ctx, cfg, markers, in.Project, in.Since)
	if err != nil {
		return "", err
	}
	return marshalResults(results)
}

func mcpDone(ctx context.Context, cfg *config.Config, e embed.Embedder, in doneInput) (string, error) {
	res, err := completeTodo(ctx, cfg, e, in.Ref, nil)
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(struct {
		Completed Result `json:"completed"`
	}{Completed: res}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func mcpMeetings(ctx context.Context, cfg *config.Config, in meetingsInput) (string, error) {
	limit := in.K
	if limit <= 0 {
		limit = 20
	}
	since, err := parseSince(in.Since)
	if err != nil {
		return "", err
	}
	var sinceT time.Time
	if since > 0 {
		sinceT = now().Add(-since)
	}
	meetings, err := recentMeetings(ctx, cfg, sinceT, limit)
	if err != nil {
		return "", err
	}
	return marshalMeetings(meetings)
}

func mcpRecent(ctx context.Context, cfg *config.Config, in recentInput) (string, error) {
	f, err := sinceFilter(store.Filter{Tags: in.Tag, Project: in.Project}, in.Since)
	if err != nil {
		return "", err
	}
	results, err := listFromStore(ctx, cfg, f, 50)
	if err != nil {
		return "", err
	}
	return marshalResults(results)
}

func mcpDecisions(ctx context.Context, cfg *config.Config, in decisionsInput) (string, error) {
	f, err := sinceFilter(store.Filter{Project: in.Project, Markers: []string{note.MarkerDecision}}, in.Since)
	if err != nil {
		return "", err
	}
	results, err := listFromStore(ctx, cfg, f, 100)
	if err != nil {
		return "", err
	}
	return marshalResults(results)
}

func mcpThreads(ctx context.Context, cfg *config.Config, in threadsInput) (string, error) {
	threads, err := threadsFromStore(ctx, cfg, in.Stale, in.Days)
	if err != nil {
		return "", err
	}
	if threads == nil {
		threads = []Thread{}
	}
	b, err := json.MarshalIndent(threadsEnvelope{Threads: threads}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func mcpCapture(cfg *config.Config, in captureInput) (string, error) {
	path, err := capture(cfg.Root(), now(), in.Text, in.Tags, in.Project, in.Marker)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]string{"captured": relTo(cfg.Root(), path)})
	return string(b), nil
}

func init() {
	mcpCmd.Flags().StringVar(&mcpRepo, "repo", ".", "path to the journal repo (defaults to the working directory)")
	rootCmd.AddCommand(mcpCmd)
}
