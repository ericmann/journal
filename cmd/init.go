package cmd

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ericmann/journal/internal/config"
	"github.com/spf13/cobra"
)

// syncScript is the cron-friendly wrapper dropped into .journal/. readmeTemplate
// documents usage + cron setup; {{ROOT}} is replaced with the repo's absolute
// path so the cron/launchd examples are copy-paste ready.
//
//go:embed templates/sync.sh
var syncScript string

//go:embed templates/README.md
var readmeTemplate string

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
		} else {
			fmt.Fprintf(out, "upgraded journal repo at %s (config left untouched)\n", root)
		}
		rel, err := filepath.Rel(root, res.readmePath)
		if err != nil {
			rel = res.readmePath
		}
		fmt.Fprintf(out, "  sync script: %s\n", filepath.Join(config.JournalDir, syncScriptName))
		fmt.Fprintf(out, "  see %s to wire it to an hourly cron (back up notes to a git remote)\n", rel)
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

// initResult reports what initRepo did. created is true only on a fresh repo
// (config newly written); readmePath is where the usage/cron README landed.
type initResult struct {
	created    bool
	readmePath string
}

// initRepo creates (or upgrades) a journal repo: the .journal directory with a
// default config.yaml and the daily/projects/reflections skeleton, a .gitignore
// entry for the index, the cron sync wrapper, and a usage README. It never
// overwrites an existing config.yaml, so re-running it on an initialized repo
// safely "upgrades" it with the latest script and docs. created is false when
// config already existed.
func initRepo(root string) (initResult, error) {
	jdir := filepath.Join(root, config.JournalDir)
	for _, dir := range []string{
		filepath.Join(jdir, "index"),
		filepath.Join(root, "daily"),
		filepath.Join(root, "projects"),
		filepath.Join(root, "reflections"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return initResult{}, fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	if err := ensureGitignore(root); err != nil {
		return initResult{}, err
	}

	// The sync script and README are generated, managed files: always refresh
	// them so an upgrade picks up the latest version.
	if err := os.WriteFile(filepath.Join(jdir, syncScriptName), []byte(syncScript), 0o755); err != nil {
		return initResult{}, fmt.Errorf("writing sync script: %w", err)
	}
	readmePath, err := writeReadme(root)
	if err != nil {
		return initResult{}, err
	}

	res := initResult{readmePath: readmePath}
	cfgPath := filepath.Join(jdir, config.ConfigFile)
	if _, err := os.Stat(cfgPath); err == nil {
		return res, nil // do not clobber an existing committed config
	}
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

// ensureGitignore makes sure the disposable index is ignored.
const gitignoreEntry = ".journal/index/"

func ensureGitignore(root string) error {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == gitignoreEntry {
			return nil
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + "# disposable, rebuildable vector index\n" + gitignoreEntry + "\n")
	return err
}

func init() {
	rootCmd.AddCommand(initCmd)
}
