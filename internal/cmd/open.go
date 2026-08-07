package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
	"github.com/utahta/intellij-wt/internal/idea"
)

var openCmd = &cobra.Command{
	Use:   "open",
	Short: "Select a worktree and open it in IDEA (raising the window if already open)",
	Args:  cobra.NoArgs,
	RunE:  runOpen,
}

func init() {
	rootCmd.AddCommand(openCmd)
}

func runOpen(cmd *cobra.Command, args []string) error {
	root, err := git.MainRoot(".")
	if err != nil {
		return err
	}
	wts, err := git.Worktrees(root)
	if err != nil {
		return err
	}
	wt, err := selectWorktree(wts, "open in IntelliJ IDEA")
	if err != nil {
		return err
	}
	if err := idea.OpenOrFocus(wt.Path); err != nil {
		return err
	}
	fmt.Println(wt.Path)
	return nil
}
