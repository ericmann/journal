package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ericmann/journal/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// syncScript is the cron-friendly wrapper dropped into .journal/. readmeTemplate
// documents usage + cron setup; {{ROOT}} is replaced with the repo's absolute
// path so the cron/launchd examples are copy-paste ready.
//
//go:embed templates/sync.sh
var syncScript string

//go:embed templates/README.md
var readmeTemplate string

// voiceProfileExample is dropped into docs/ so `journal synth`'s voice feature
// is discoverable; the user copies it to docs/VOICE_PROFILE.md and edits.
//
//go:embed templates/VOICE_PROFILE.example.md
var voiceProfileExample string

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize a new journal repo (config + directory skeleton)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := "."
		if len(args) == 1 {
			target = args[0]
		}
		root, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		res, err := initRepo(root)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if res.created {
			fmt.Fprintf(out, "initialized journal repo at %s\n", root)
		} else if len(res.changes) > 0 {
			fmt.Fprintf(out, "upgraded journal repo at %s:\n", root)
			for _, c := range res.changes {
				fmt.Fprintf(out, "  • %s\n", c)
			}
			fmt.Fprintln(out, "  (your notes and existing config values are untouched)")
		} else {
			fmt.Fprintf(out, "journal repo at %s already up to date (schema %s)\n", root, config.SchemaVersion)
		}
		rel, err := filepath.Rel(root, res.readmePath)
		if err != nil {
			rel = res.readmePath
		}
		fmt.Fprintf(out, "  sync script: %s\n", filepath.Join(config.JournalDir, syncScriptName))
		fmt.Fprintf(out, "  see %s to wire it to an hourly cron (back up notes to a git remote)\n", rel)
		fmt.Fprintf(out, "  voice profile: copy %s → %s to shape `journal synth` drafts\n",
			filepath.Join("docs", voiceExampleName), filepath.Join("docs", "VOICE_PROFILE.md"))
		if res.created {
			def := config.Default()
			fmt.Fprintf(out, "\nNext steps (indexing & search need a local Ollama):\n")
			fmt.Fprintf(out, "  1. install Ollama:        https://ollama.com\n")
			fmt.Fprintf(out, "  2. pull the embed model:  ollama pull %s\n", def.EmbedModel)
			fmt.Fprintf(out, "  3. verify your setup:     journal doctor\n")
			fmt.Fprintf(out, "  4. capture your first note: journal capture \"hello, journal\"\n")
		}
		return nil
	},
}

// syncScriptName is the cron wrapper written into .journal/.
const syncScriptName = "sync.sh"

// voiceExampleName is the voice-profile template written into docs/.
const voiceExampleName = "VOICE_PROFILE.example.md"

// initResult reports what initRepo did. created is true only on a fresh repo
// (config newly written); readmePath is where the usage/cron README landed. On
// an upgrade, changes lists what was added and schemaFrom is the prior schema.
type initResult struct {
	created    bool
	readmePath string
	changes    []string
	schemaFrom string
}

// initRepo creates (or upgrades) a journal repo non-destructively. On a fresh
// repo it writes the full default config + skeleton. On an existing repo it
// applies only what's missing — new directories, new config keys (with their
// defaults, preserving the user's values), gitignore entries — bumps the config
// schema to the current version, and reports the changes. Managed files (sync
// script, README, voice example) are always refreshed; the user's notes and set
// config values are never touched.
func initRepo(root string) (initResult, error) {
	jdir := filepath.Join(root, config.JournalDir)
	cfgPath := filepath.Join(jdir, config.ConfigFile)
	_, statErr := os.Stat(cfgPath)
	configExisted := statErr == nil

	var priorKeys map[string]bool
	var priorSchema string
	if configExisted {
		priorKeys, priorSchema = readConfigKeys(cfgPath)
	}

	var res initResult

	// Directories: create what's missing. On an upgrade, note newly-created ones.
	for _, dir := range []string{
		filepath.Join(jdir, "index"),
		filepath.Join(root, "daily"),
		filepath.Join(root, "projects"),
		filepath.Join(root, "reflections"),
		filepath.Join(root, "docs"),
		filepath.Join(root, "transcripts"),
	} {
		_, derr := os.Stat(dir)
		newlyCreated := os.IsNotExist(derr)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return initResult{}, fmt.Errorf("creating %s: %w", dir, err)
		}
		if configExisted && newlyCreated {
			rel, _ := filepath.Rel(root, dir)
			res.changes = append(res.changes, "created "+filepath.ToSlash(rel)+"/")
		}
	}

	added, err := ensureGitignore(root)
	if err != nil {
		return initResult{}, err
	}
	if configExisted {
		for _, e := range added {
			res.changes = append(res.changes, "gitignored "+e)
		}
	}

	// Managed files: always refresh (the example never clobbers a real profile).
	if err := os.WriteFile(filepath.Join(jdir, syncScriptName), []byte(syncScript), 0o755); err != nil {
		return initResult{}, fmt.Errorf("writing sync script: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", voiceExampleName), []byte(voiceProfileExample), 0o644); err != nil {
		return initResult{}, fmt.Errorf("writing voice profile example: %w", err)
	}
	readmePath, rerr := writeReadme(root)
	if rerr != nil {
		return initResult{}, rerr
	}
	res.readmePath = readmePath

	if !configExisted {
		def := config.Default()
		data, err := def.Marshal()
		if err != nil {
			return initResult{}, err
		}
		if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
			return initResult{}, fmt.Errorf("writing config: %w", err)
		}
		res.created = true
		return res, nil
	}

	// Upgrade: re-marshal the loaded config (defaults fill missing keys; the
	// user's values are preserved) and bump the schema version.
	cfg, err := config.Load(root)
	if err != nil {
		return initResult{}, err
	}
	cfg.SchemaVer = config.SchemaVersion
	data, err := cfg.Marshal()
	if err != nil {
		return initResult{}, err
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		return initResult{}, fmt.Errorf("writing config: %w", err)
	}
	if newKeys := addedConfigKeys(priorKeys); len(newKeys) > 0 {
		res.changes = append(res.changes, "added config keys: "+strings.Join(newKeys, ", "))
	}
	res.schemaFrom = priorSchema
	if priorSchema != config.SchemaVersion {
		res.changes = append(res.changes, fmt.Sprintf("schema_version: %s → %s", schemaLabel(priorSchema), config.SchemaVersion))
	}
	return res, nil
}

// readConfigKeys returns the top-level keys present in an existing config file
// and its schema_version (best-effort; a parse error yields empty sets).
func readConfigKeys(cfgPath string) (map[string]bool, string) {
	keys := map[string]bool{}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return keys, ""
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return keys, ""
	}
	for k := range raw {
		keys[k] = true
	}
	schema, _ := raw["schema_version"].(string)
	return keys, schema
}

// addedConfigKeys reports default config keys absent from priorKeys (sorted) —
// the general "what's new" diff that works for any future config addition.
func addedConfigKeys(priorKeys map[string]bool) []string {
	def := config.Default()
	data, err := def.Marshal()
	if err != nil {
		return nil
	}
	var full map[string]any
	if err := yaml.Unmarshal(data, &full); err != nil {
		return nil
	}
	var added []string
	for k := range full {
		if !priorKeys[k] {
			added = append(added, k)
		}
	}
	sort.Strings(added)
	return added
}

func schemaLabel(s string) string {
	if s == "" {
		return "(pre-2.0)"
	}
	return s
}

// writeReadme writes the usage/cron guide. It prefers a discoverable top-level
// README.md, but only creates one when none exists — never clobbering a
// hand-written README. Otherwise it writes .journal/README.md (always managed).
// {{ROOT}} in the template is replaced with the repo's absolute path so the
// cron and launchd examples are copy-paste ready. It returns the path written.
func writeReadme(root string) (string, error) {
	content := strings.ReplaceAll(readmeTemplate, "{{ROOT}}", root)
	rootReadme := filepath.Join(root, "README.md")
	if _, err := os.Stat(rootReadme); os.IsNotExist(err) {
		if err := os.WriteFile(rootReadme, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("writing README: %w", err)
		}
		return rootReadme, nil
	} else if err != nil {
		return "", err
	}
	jdirReadme := filepath.Join(root, config.JournalDir, "README.md")
	if err := os.WriteFile(jdirReadme, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing README: %w", err)
	}
	return jdirReadme, nil
}

// gitignoreEntries are the paths journal manages in .gitignore: the disposable
// vector index and the ephemeral transcript landing zone (rebuilt from Quill).
var gitignoreEntries = []struct{ comment, entry string }{
	{"disposable, rebuildable vector index", ".journal/index/"},
	{"ephemeral meeting transcripts (re-synced from Quill)", "transcripts/"},
}

// ensureGitignore appends any managed entries not already present, returning the
// entries it added.
func ensureGitignore(root string) ([]string, error) {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	present := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		present[strings.TrimSpace(line)] = true
	}

	var toAdd []struct{ comment, entry string }
	for _, e := range gitignoreEntries {
		if !present[e.entry] {
			toAdd = append(toAdd, e)
		}
	}
	if len(toAdd) == 0 {
		return nil, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var b strings.Builder
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		b.WriteString("\n")
	}
	var added []string
	for _, e := range toAdd {
		fmt.Fprintf(&b, "# %s\n%s\n", e.comment, e.entry)
		added = append(added, e.entry)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		return nil, err
	}
	return added, nil
}

func init() {
	rootCmd.AddCommand(initCmd)
}
