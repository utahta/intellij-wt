package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var pathAll bool

var pathCmd = &cobra.Command{
	Use:   "path",
	Short: "Select a worktree and print its path (for cd wrappers)",
	Long: `Select a worktree and print its path to stdout.

With --all (or outside a git repository), selects from every discovered
repository instead of the current one.

Pair it with a shell function to cd into a worktree:

  function iwtcd() {
    local p
    p=$(iwt path) && cd "$p"
  }`,
	Args: cobra.NoArgs,
	RunE: runPath,
}

func init() {
	pathCmd.Flags().BoolVarP(&pathAll, "all", "a", false, "select from all repositories (shared root and $IWT_SEARCH_PATH)")
	rootCmd.AddCommand(pathCmd)
}

func runPath(cmd *cobra.Command, args []string) error {
	entries, err := gatherWorktrees(pathAll)
	if err != nil {
		return err
	}
	wt, err := selectEntry(entries, "print worktree path")
	if err != nil {
		return err
	}
	fmt.Println(wt.Path)
	return nil
}
