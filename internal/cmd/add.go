package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
	"github.com/utahta/intellij-wt/internal/idea"
)

var addNoOpen bool

var addCmd = &cobra.Command{
	Use:   "add <branch> [base]",
	Short: "Create a worktree for the branch and open it in IDEA",
	Long: `Create a worktree under <repo>-wt/<branch> and open it in IntelliJ IDEA.

An existing branch is checked out as is. A new branch is created off [base]
(default: origin's default branch, falling back to HEAD). Any .envrc found
directly under the worktree or one level below is direnv-allowed.

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

	path := worktreePath(root, branch)
	if git.BranchExists(root, branch) {
		err = git.AddWorktree(root, path, branch)
	} else {
		base := ""
		if len(args) == 2 {
			base = args[1]
		} else {
			base = git.DefaultBranch(root)
		}
		if base == "" {
			base = "HEAD"
		}
		err = git.AddWorktreeNewBranch(root, path, branch, base)
	}
	if err != nil {
		return err
	}

	allowDirenv(path)

	if !addNoOpen {
		if err := idea.Open(path); err != nil {
			fmt.Fprintln(os.Stderr, "wt: failed to open IDEA:", err)
		}
	}
	fmt.Println(path)
	return nil
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
