package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
	"github.com/utahta/intellij-wt/internal/idea"
	"github.com/utahta/intellij-wt/internal/picker"
)

var addNoOpen bool

var addCmd = &cobra.Command{
	Use:   "add <branch> [base]",
	Short: "Create a worktree for the branch and open it in IDEA",
	Long: `Create a worktree and open it in IntelliJ IDEA.

Worktrees live under a shared root (default ~/.intellij-wt/worktrees,
overridable with IWT_ROOT), organized as <org>/<repo>/<repo>--<branch>.
The org comes from the origin remote URL ("_local" when there is none).

An existing branch is checked out as is; a branch that only exists on
origin is checked out tracking it. Otherwise a new branch is created off
[base] (default: origin's default branch, falling back to HEAD). Any
.envrc found directly under the worktree or one level below is
direnv-allowed.

The created worktree path is printed to stdout.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runAdd,
}

func init() {
	addCmd.Flags().BoolVarP(&addNoOpen, "no-open", "n", false, "do not open IDEA")
	rootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	branch := args[0]
	root, err := git.MainRoot(".")
	if err != nil {
		return err
	}

	base := ""
	if len(args) == 2 {
		base = args[1]
	}
	path, err := createWorktree(root, branch, base, nil)
	if err != nil {
		return err
	}

	if !addNoOpen {
		if err := idea.Open(path); err != nil {
			fmt.Fprintln(os.Stderr, paint("1;31", fmt.Sprintf("iwt: failed to open IDEA: %v", err)))
		}
	}
	fmt.Println(path)
	return nil
}

// createWorktree creates a worktree for branch in the repository at root
// and returns its path. An existing branch is checked out as is. At most
// one of base and track may be set: track is a picked remote branch to
// check out tracking it, while base starts a new branch off it under
// git's own upstream rules (autosetupmerge). With neither, a branch
// existing on a remote is checked out tracking it (origin preferred),
// and otherwise a new branch is created off origin's default branch,
// falling back to HEAD.
func createWorktree(root, branch, base string, track *git.RemoteBranch) (string, error) {
	path, err := worktreePath(root, branch)
	if err != nil {
		return "", err
	}
	switch {
	case git.BranchExists(root, branch):
		err = git.AddWorktree(root, path, branch)
	case track != nil:
		err = git.AddWorktreeTracking(root, path, branch, *track)
	case base != "":
		err = git.AddWorktreeNewBranch(root, path, branch, base)
	default:
		rbs := git.RemoteBranchRefs(root, branch)
		ambiguous := false
		for _, rb := range rbs {
			ambiguous = ambiguous || rb.Ambiguous
		}
		switch {
		// "Exists but ambiguous" is not "does not exist": silently
		// creating a fresh branch would shadow the remote one.
		case ambiguous:
			return "", fmt.Errorf("branch %q exists on a remote, but several remotes fetch into its tracking ref; pass a base to disambiguate", branch)
		// More than one entry means distinct non-origin remotes have
		// the branch (origin would have been preferred): guessing would
		// silently check out the wrong commit.
		case len(rbs) > 1:
			short := make([]string, len(rbs))
			for i, rb := range rbs {
				short[i] = git.ShortRef(rb.Ref)
			}
			return "", fmt.Errorf("branch %q exists on multiple remotes (%s); pass a base to disambiguate", branch, strings.Join(short, ", "))
		case len(rbs) == 1:
			err = git.AddWorktreeTracking(root, path, branch, rbs[0])
		default:
			def := git.DefaultBranch(root)
			if def == "" {
				def = "HEAD"
			}
			err = git.AddWorktreeNewBranch(root, path, branch, def)
		}
	}
	if err != nil {
		return "", err
	}
	allowDirenv(path)
	fmt.Fprintln(os.Stderr, paint("1;32", "created: "+picker.Sanitize(path)))
	return path, nil
}

// allowDirenv pre-approves .envrc files in the new worktree (root and one
// level deep) so tools work immediately. Failures are non-fatal.
func allowDirenv(path string) {
	if _, err := exec.LookPath("direnv"); err != nil {
		return
	}
	for _, pattern := range []string{
		filepath.Join(path, ".envrc"),
		filepath.Join(path, "*", ".envrc"),
	} {
		matches, _ := filepath.Glob(pattern)
		for _, rc := range matches {
			c := exec.Command("direnv", "allow", rc)
			c.Stdout = os.Stderr
			c.Stderr = os.Stderr
			_ = c.Run()
		}
	}
}
