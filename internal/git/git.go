// Package git wraps the git CLI for worktree management.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree is a single entry of `git worktree list --porcelain`.
type Worktree struct {
	Path   string
	Head   string
	Branch string // short branch name; empty when detached
	Main   bool   // true for the main worktree
}

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// runLoud is for mutating commands whose progress output is useful to the
// user. stdout goes to stderr so that wt's own stdout stays script-friendly.
func runLoud(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// MainRoot returns the root directory of the main worktree, even when dir is
// inside a linked worktree.
func MainRoot(dir string) (string, error) {
	common, err := run(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Dir(common), nil
}

// Worktrees returns all worktrees of the repository containing dir.
// The main worktree comes first.
func Worktrees(dir string) ([]Worktree, error) {
	out, err := run(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

func parseWorktrees(out string) []Worktree {
	var wts []Worktree
	var cur *Worktree
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			wts = append(wts, Worktree{Path: strings.TrimPrefix(line, "worktree ")})
			cur = &wts[len(wts)-1]
		case cur == nil:
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	if len(wts) > 0 {
		wts[0].Main = true
	}
	return wts
}

// DefaultBranch returns the short name of origin's default branch
// (e.g. "develop"), or "" when origin/HEAD is not set.
func DefaultBranch(dir string) string {
	out, err := run(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(out, "origin/")
}

func BranchExists(dir, branch string) bool {
	_, err := run(dir, "show-ref", "--verify", "refs/heads/"+branch)
	return err == nil
}

// AddWorktree checks out an existing branch into a new worktree at path.
func AddWorktree(dir, path, branch string) error {
	return runLoud(dir, "worktree", "add", path, branch)
}

// AddWorktreeNewBranch creates branch off base and checks it out into a new
// worktree at path.
func AddWorktreeNewBranch(dir, path, branch, base string) error {
	return runLoud(dir, "worktree", "add", "-b", branch, path, base)
}

func RemoveWorktree(dir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	return runLoud(dir, args...)
}

// PruneWorktrees cleans up stale administrative files.
func PruneWorktrees(dir string) error {
	return runLoud(dir, "worktree", "prune")
}

// IsDirty reports whether the worktree at path has uncommitted changes.
func IsDirty(path string) bool {
	out, err := run(path, "status", "--porcelain")
	return err == nil && out != ""
}

// LastCommitRel returns the relative time of the last commit (e.g. "2 days ago").
func LastCommitRel(path string) string {
	out, _ := run(path, "log", "-1", "--format=%cr")
	return out
}

// IsMerged reports whether branch is fully merged into the into branch.
func IsMerged(dir, branch, into string) bool {
	out, err := run(dir, "branch", "--merged", into, "--format=%(refname:short)")
	if err != nil {
		return false
	}
	for _, b := range strings.Split(out, "\n") {
		if strings.TrimSpace(b) == branch {
			return true
		}
	}
	return false
}

// DeleteBranch deletes a fully merged branch.
func DeleteBranch(dir, branch string) error {
	return runLoud(dir, "branch", "-d", branch)
}
