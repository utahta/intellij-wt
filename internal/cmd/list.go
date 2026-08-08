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
	listRepos     bool
)

var listCmd = &cobra.Command{
	Use:   "list [path]",
	Short: "List worktrees of the current repository",
	Long: `List worktrees of the current repository, of the repository containing
[path], or — with --all or outside a repository — of every discovered
repository.

--repos lists repositories (their main worktree roots) instead of
worktrees. --porcelain prints a stable header-less tab-separated format
for scripts and fzf wrappers:

  <org/repo>	<branch>	<path>

The branch field is empty with --repos; the org/repo field is empty
when listing a single repository without [path].`,
	Args: cobra.MaximumNArgs(1),
	RunE: runList,
}

func init() {
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "list all repositories (shared root and $IWT_SEARCH_PATH)")
	listCmd.Flags().BoolVar(&listPorcelain, "porcelain", false, "machine-readable tab-separated output")
	listCmd.Flags().BoolVar(&listRepos, "repos", false, "list repositories instead of worktrees (implies --all)")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	if listRepos {
		if len(args) > 0 {
			return fmt.Errorf("--repos cannot be combined with a path argument")
		}
		return runListRepos()
	}

	var entries []worktreeEntry
	var err error
	if len(args) == 1 {
		entries, err = worktreesAt(args[0])
	} else {
		entries, err = gatherWorktrees(listAll)
	}
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

// runListRepos lists every discovered repository's main worktree root,
// without enumerating worktrees — org labels come from .git/config, so a
// large search path renders near-instantly.
func runListRepos() error {
	roots := discoverRoots()
	if len(roots) == 0 {
		return fmt.Errorf("no repositories found under the worktree root or $IWT_SEARCH_PATH")
	}
	labels := make([]string, len(roots))
	runParallel(len(roots), func(i int) {
		labels[i] = repoLabel(roots[i])
	})

	if listPorcelain {
		for i, r := range roots {
			fmt.Printf("%s\t\t%s\n", labels[i], r)
		}
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tPATH")
	for i, r := range roots {
		fmt.Fprintf(w, "%s\t%s\n", labels[i], r)
	}
	return w.Flush()
}
