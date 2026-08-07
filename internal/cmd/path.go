package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
)

var pathCmd = &cobra.Command{
	Use:   "path",
	Short: "Select a worktree and print its path (for cd wrappers)",
	Long: `Select a worktree and print its path to stdout.

Pair it with a shell function to cd into a worktree:

  function wtcd() {
    local p
    p=$(wt path) && cd "$p"
  }`,
	Args: cobra.NoArgs,
	RunE: runPath,
}

func init() {
	rootCmd.AddCommand(pathCmd)
}

func runPath(cmd *cobra.Command, args []string) error {
	root, err := git.MainRoot(".")
	if err != nil {
		return err
	}
	wts, err := git.Worktrees(root)
	if err != nil {
		return err
	}
	wt, err := selectWorktree(wts, "print worktree path")
	if err != nil {
		return err
	}
	fmt.Println(wt.Path)
	return nil
}
