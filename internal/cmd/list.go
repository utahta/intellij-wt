package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees of the current repository",
	Args:  cobra.NoArgs,
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	root, err := git.MainRoot(".")
	if err != nil {
		return err
	}
	wts, err := git.Worktrees(root)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "BRANCH\tSTATE\tLAST COMMIT\tPATH")
	for _, wt := range wts {
		branch := wt.Branch
		if branch == "" {
			branch = wt.Head[:min(8, len(wt.Head))] + " (detached)"
		}
		if wt.Main {
			branch += " (main)"
		}
		state := "clean"
		if git.IsDirty(wt.Path) {
			state = "dirty"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", branch, state, git.LastCommitRel(wt.Path), wt.Path)
	}
	return w.Flush()
}
