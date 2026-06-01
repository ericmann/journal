package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ericmann/journal/internal/config"
	"github.com/spf13/cobra"
)

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
		created, err := initRepo(root)
		if err != nil {
			return err
		}
		if created {
			fmt.Fprintf(cmd.OutOrStdout(), "initialized journal repo at %s\n", root)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "journal repo already initialized at %s (left config untouched)\n", root)
		}
		return nil
	},
}

// initRepo creates the .journal directory with a default config.yaml and the
// daily/projects/reflections skeleton, plus a .gitignore entry for the index.
// It never overwrites an existing config.yaml. created is false if config
// already existed.
func initRepo(root string) (bool, error) {
	jdir := filepath.Join(root, config.JournalDir)
	for _, dir := range []string{
		filepath.Join(jdir, "index"),
		filepath.Join(root, "daily"),
		filepath.Join(root, "projects"),
		filepath.Join(root, "reflections"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	if err := ensureGitignore(root); err != nil {
		return false, err
	}

	cfgPath := filepath.Join(jdir, config.ConfigFile)
	if _, err := os.Stat(cfgPath); err == nil {
		return false, nil // do not clobber an existing committed config
	}
	def := config.Default()
	data, err := def.Marshal()
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		return false, fmt.Errorf("writing config: %w", err)
	}
	return true, nil
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
