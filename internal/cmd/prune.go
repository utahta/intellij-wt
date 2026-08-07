package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
)

var pruneForce bool

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Select worktrees to remove (Tab to multi-select)",
	Long: `Select worktrees to remove. The main worktree is never listed.

Dirty worktrees require confirmation (or --force). After removal, if the
branch is fully merged into origin's default branch, offers to delete it.`,
	Args: cobra.NoArgs,
	RunE: runPrune,
}

func init() {
	pruneCmd.Flags().BoolVarP(&pruneForce, "force", "f", false, "remove dirty worktrees without confirmation")
	rootCmd.AddCommand(pruneCmd)
}

func runPrune(cmd *cobra.Command, args []string) error {
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
		fmt.Fprintln(os.Stderr, "no removable worktrees")
		return nil
	}

	selected, err := selectWorktrees(wts, "remove worktrees (Tab to multi-select)")
	if err != nil {
		return err
	}

	defaultBranch := git.DefaultBranch(root)
	for _, w := range selected {
		force := pruneForce
		if !force && git.IsDirty(w.Path) {
			if !confirm(fmt.Sprintf("%s has uncommitted changes. Remove anyway?", w.Path)) {
				fmt.Fprintf(os.Stderr, "skipped: %s\n", w.Path)
				continue
			}
			force = true
		}
		if err := git.RemoveWorktree(root, w.Path, force); err != nil {
			fmt.Fprintf(os.Stderr, "wt: %v\n", err)
			continue
		}
		fmt.Fprintf(os.Stderr, "removed: %s\n", w.Path)

		if w.Branch != "" && defaultBranch != "" && git.IsMerged(root, w.Branch, defaultBranch) {
			if confirm(fmt.Sprintf("branch %q is merged into %s. Delete it?", w.Branch, defaultBranch)) {
				if err := git.DeleteBranch(root, w.Branch); err != nil {
					fmt.Fprintf(os.Stderr, "wt: %v\n", err)
				}
			}
		}
	}
	return git.PruneWorktrees(root)
}
