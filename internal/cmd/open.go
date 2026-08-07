package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
	"github.com/utahta/intellij-wt/internal/idea"
)

var openCmd = &cobra.Command{
	Use:   "open [branch|path]",
	Short: "Open a worktree in IDEA (raising the window if already open)",
	Long: `Open a worktree in IntelliJ IDEA, raising the window if already open.

With no argument, presents a fuzzy-finder selection. An argument is matched
against branch names of the current repository first, then treated as a
directory: the worktree containing it is opened, even when it belongs to a
different repository. This makes it usable from scripts and hooks, e.g.
iwt open "$(git rev-parse --show-toplevel)".`,
	Args: cobra.MaximumNArgs(1),
	RunE: runOpen,
}

func init() {
	rootCmd.AddCommand(openCmd)
}

func runOpen(cmd *cobra.Command, args []string) error {
	var wt git.Worktree
	if len(args) == 1 {
		var err error
		wt, err = resolveWorktree(args[0])
		if err != nil {
			return err
		}
	} else {
		root, err := git.MainRoot(".")
		if err != nil {
			return err
		}
		wts, err := git.Worktrees(root)
		if err != nil {
			return err
		}
		wt, err = selectWorktree(wts, "open in IntelliJ IDEA")
		if err != nil {
			return err
		}
	}
	if err := idea.Open(wt.Path); err != nil {
		return err
	}
	fmt.Println(wt.Path)
	return nil
}

// resolveWorktree finds the worktree for target: a branch name of the
// current repository, or a directory inside any worktree of any repository.
func resolveWorktree(target string) (git.Worktree, error) {
	if root, err := git.MainRoot("."); err == nil {
		if wts, err := git.Worktrees(root); err == nil {
			for _, w := range wts {
				if w.Branch == target {
					return w, nil
				}
			}
		}
	}

	abs, err := filepath.Abs(target)
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		if root, err := git.MainRoot(abs); err == nil {
			wts, err := git.Worktrees(root)
			if err == nil {
				// Deepest worktree containing abs wins (worktrees can nest).
				var best git.Worktree
				for _, w := range wts {
					if (abs == w.Path || strings.HasPrefix(abs, w.Path+string(filepath.Separator))) &&
						len(w.Path) > len(best.Path) {
						best = w
					}
				}
				if best.Path != "" {
					return best, nil
				}
			}
		}
	}
	return git.Worktree{}, fmt.Errorf("no worktree matches %q", target)
}
