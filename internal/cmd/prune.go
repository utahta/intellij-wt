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
branch is fully merged into origin's default branch, offers to delete it;
when that cannot be determined (origin's default branch is unset), asks
explicitly, still deleting with git branch -d so unmerged branches are
refused.

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

	if pruneMerged {
		return pruneMergedWorktrees(root)
	}

	selected, err := selectWorktrees(wts, "remove worktrees (Tab to multi-select)")
	if err != nil {
		return err
	}
	removeWorktrees(root, selected, false)
	return git.PruneWorktrees(root)
}

// pruneMergedWorktrees removes every worktree of root whose branch is
// merged into origin's default branch, without prompting.
func pruneMergedWorktrees(root string) error {
	all, err := git.Worktrees(root)
	if err != nil {
		return err
	}
	defaultBranch := git.DefaultBranch(root)
	if defaultBranch == "" {
		return fmt.Errorf("pruning merged worktrees requires origin's default branch (run: git remote set-head origin --auto)")
	}
	var selected []git.Worktree
	for _, w := range all {
		if !w.Main && w.Branch != "" && w.Branch != defaultBranch && git.IsMerged(root, w.Branch, defaultBranch) {
			selected = append(selected, w)
		}
	}
	if len(selected) == 0 {
		fmt.Fprintln(os.Stderr, paint("33", "no merged worktrees"))
		return git.PruneWorktrees(root)
	}
	removeWorktrees(root, selected, true)
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
	removeWorktrees(root, []git.Worktree{wt}, false)
	return git.PruneWorktrees(root)
}

// removeWorktrees removes the given worktrees of root: dirty ones need
// confirmation (or --force), and fully merged branches are deleted after
// a confirmation. In merged mode (a bulk removal of merged worktrees)
// nothing prompts: dirty worktrees are skipped and merged branches are
// deleted. Failures are reported and skipped.
func removeWorktrees(root string, wts []git.Worktree, merged bool) {
	defaultBranch := git.DefaultBranch(root)
	for _, w := range wts {
		force := pruneForce
		if !force && git.IsDirty(w.Path) {
			if merged {
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

		if w.Branch == "" {
			continue
		}
		// Offer to delete the branch: automatically confirmed in --merged
		// mode, asked when it is fully merged, and asked explicitly when
		// that cannot be determined (no origin default branch) — the
		// deletion below uses git branch -d, so git still refuses a
		// branch that turns out to be unmerged.
		del := false
		switch {
		case defaultBranch != "" && git.IsMerged(root, w.Branch, defaultBranch):
			del = merged || confirm(fmt.Sprintf("branch %q is merged into %s. Delete it?", w.Branch, defaultBranch))
		case defaultBranch == "" && !merged:
			del = confirm(fmt.Sprintf("branch %q: cannot tell whether it is merged (origin's default branch is unset). Delete it anyway?", w.Branch))
		}
		if del {
			if err := git.DeleteBranch(root, w.Branch); err != nil {
				fmt.Fprintln(os.Stderr, paint("1;31", fmt.Sprintf("iwt: %v", err)))
			} else {
				fmt.Fprintln(os.Stderr, paint("1;32", "deleted branch: "+w.Branch))
			}
		}
	}
}
