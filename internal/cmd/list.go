package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
)

var (
	listAll       bool
	listPorcelain bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees of the current repository",
	Long: `List worktrees of the current repository.

With --all (or outside a git repository), lists worktrees of every
discovered repository. --porcelain prints a stable header-less
tab-separated format for scripts and fzf wrappers:

  <org/repo>	<branch>	<path>

The first field is empty when listing a single repository.`,
	Args: cobra.NoArgs,
	RunE: runList,
}

func init() {
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "list all repositories (shared root and $IWT_SEARCH_PATH)")
	listCmd.Flags().BoolVar(&listPorcelain, "porcelain", false, "machine-readable tab-separated output")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	entries, err := gatherWorktrees(listAll)
	if err != nil {
		return err
	}

	if listPorcelain {
		for _, e := range entries {
			branch := e.Branch
			if branch == "" {
				branch = e.Head[:min(8, len(e.Head))]
			}
			fmt.Printf("%s\t%s\t%s\n", e.Repo, branch, e.Path)
		}
		return nil
	}

	// State and last-commit need two git calls per worktree; fetch them
	// concurrently so --all stays fast across many repositories.
	type info struct{ state, rel string }
	infos := make([]info, len(entries))
	runParallel(len(entries), func(i int) {
		state := "clean"
		if git.IsDirty(entries[i].Path) {
			state = "dirty"
		}
		infos[i] = info{state, git.LastCommitRel(entries[i].Path)}
	})

	showRepo := false
	for _, e := range entries {
		if e.Repo != "" {
			showRepo = true
			break
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if showRepo {
		fmt.Fprintln(w, "REPO\tBRANCH\tSTATE\tLAST COMMIT\tPATH")
	} else {
		fmt.Fprintln(w, "BRANCH\tSTATE\tLAST COMMIT\tPATH")
	}
	for i, e := range entries {
		branch := e.Branch
		if branch == "" {
			branch = e.Head[:min(8, len(e.Head))] + " (detached)"
		}
		if e.Main {
			branch += " (main)"
		}
		if showRepo {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Repo, branch, infos[i].state, infos[i].rel, e.Path)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", branch, infos[i].state, infos[i].rel, e.Path)
		}
	}
	return w.Flush()
}
