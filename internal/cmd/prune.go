package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
)

var (
	pruneForce  bool
	pruneMerged bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune [target]",
	Short: "Select worktrees to remove (Tab to multi-select)",
	Long: `Select worktrees to remove. The main worktree is never listed.

Dirty worktrees require confirmation (or --force). After removal, if the
branch is fully merged into origin's default branch, offers to delete it.

A target argument skips the selection: a branch name of the current
repository, or a directory inside any worktree of any repository. The
main worktree is refused.

With --merged, skips the selection and removes every worktree whose branch
is merged into origin's default branch, deleting the branch as well. Dirty
worktrees are skipped unless --force.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPrune,
}

func init() {
	pruneCmd.Flags().BoolVarP(&pruneForce, "force", "f", false, "remove dirty worktrees without confirmation")
	pruneCmd.Flags().BoolVarP(&pruneMerged, "merged", "m", false, "remove all worktrees merged into origin's default branch")
	rootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, args []string) error {
	if len(args) == 1 {
		if pruneMerged {
			return fmt.Errorf("--merged cannot be combined with a target argument")
		}
		return pruneTarget(args[0])
	}

	root, err := git.MainRoot(".")
	if err != nil {
		return err
	}
	all, err := git.Worktrees(root)
	if err != nil {
		return err
	}
	var wts []git.Worktree
	for _, w := range all {
		if !w.Main {
			wts = append(wts, w)
		}
	}
	if len(wts) == 0 {
		fmt.Fprintln(os.Stderr, paint("33", "no removable worktrees"))
		return nil
	}

	defaultBranch := git.DefaultBranch(root)

	var selected []git.Worktree
	if pruneMerged {
		if defaultBranch == "" {
			return fmt.Errorf("--merged requires origin's default branch (run: git remote set-head origin --auto)")
		}
		for _, w := range wts {
			if w.Branch != "" && w.Branch != defaultBranch && git.IsMerged(root, w.Branch, defaultBranch) {
				selected = append(selected, w)
			}
		}
		if len(selected) == 0 {
			fmt.Fprintln(os.Stderr, paint("33", "no merged worktrees"))
			return git.PruneWorktrees(root)
		}
	} else {
		selected, err = selectWorktrees(wts, "remove worktrees (Tab to multi-select)")
		if err != nil {
			return err
		}
	}

	removeWorktrees(root, selected)
	return git.PruneWorktrees(root)
}

// pruneTarget removes a single worktree resolved from a branch name or a
// directory, with the same confirmations as the interactive flow.
func pruneTarget(target string) error {
	wt, err := resolveWorktree(target)
	if err != nil {
		return err
	}
	if wt.Main {
		return fmt.Errorf("cannot remove the main worktree %s", wt.Path)
	}
	root, err := git.MainRoot(wt.Path)
	if err != nil {
		return err
	}
	removeWorktrees(root, []git.Worktree{wt})
	return git.PruneWorktrees(root)
}

// removeWorktrees removes the given worktrees of root: dirty ones need
// confirmation (or --force), and fully merged branches are deleted after
// a confirmation (automatically in --merged mode). Failures are reported
// and skipped.
func removeWorktrees(root string, wts []git.Worktree) {
	defaultBranch := git.DefaultBranch(root)
	for _, w := range wts {
		force := pruneForce
		if !force && git.IsDirty(w.Path) {
			if pruneMerged {
				fmt.Fprintln(os.Stderr, paint("33", "skipped (dirty): "+w.Path))
				continue
			}
			if !confirm(fmt.Sprintf("%s has uncommitted changes. Remove anyway?", w.Path)) {
				fmt.Fprintln(os.Stderr, paint("33", "skipped: "+w.Path))
				continue
			}
			force = true
		}
		if err := git.RemoveWorktree(root, w.Path, force); err != nil {
			fmt.Fprintln(os.Stderr, paint("1;31", fmt.Sprintf("iwt: %v", err)))
			continue
		}
		fmt.Fprintln(os.Stderr, paint("1;32", "removed: "+w.Path))

		if w.Branch != "" && defaultBranch != "" && git.IsMerged(root, w.Branch, defaultBranch) {
			if pruneMerged || confirm(fmt.Sprintf("branch %q is merged into %s. Delete it?", w.Branch, defaultBranch)) {
				if err := git.DeleteBranch(root, w.Branch); err != nil {
					fmt.Fprintln(os.Stderr, paint("1;31", fmt.Sprintf("iwt: %v", err)))
				} else {
					fmt.Fprintln(os.Stderr, paint("1;32", "deleted branch: "+w.Branch))
				}
			}
		}
	}
}
