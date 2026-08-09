// Package git wraps the git CLI for worktree management.
package git

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
// user. stdout goes to stderr so that iwt's own stdout stays script-friendly.
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

// OriginOwner returns the owner (organization or user) of the origin remote,
// or "" when origin is missing or its URL has no owner segment. The URL is
// read straight from .git/config (~100x faster than spawning git); when that
// yields nothing (config elsewhere, include directives, .git file), it falls
// back to git.
func OriginOwner(dir string) string {
	url := configOriginURL(filepath.Join(dir, ".git", "config"))
	if url == "" {
		url, _ = run(dir, "remote", "get-url", "origin")
	}
	return ownerFromURL(url)
}

// configOriginURL extracts remote.origin.url from a git config file,
// returning "" on any miss so the caller can fall back to git.
func configOriginURL(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	inOrigin := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			sec := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			name, sub, _ := strings.Cut(sec, " ")
			inOrigin = strings.EqualFold(name, "remote") && sub == `"origin"`
			continue
		}
		if inOrigin {
			if k, v, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(k), "url") {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// ownerFromURL extracts the owner from a git remote URL: the second-to-last
// path segment of scp-like (git@host:owner/repo.git) and URL
// (scheme://host/owner/repo) forms. Filesystem-path remotes yield "".
func ownerFromURL(url string) string {
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	if strings.HasPrefix(url, "file://") || strings.HasPrefix(url, "/") ||
		strings.HasPrefix(url, "./") || strings.HasPrefix(url, "../") {
		return ""
	}
	if i := strings.Index(url, "://"); i >= 0 {
		url = url[i+3:]
	} else if i := strings.Index(url, ":"); i >= 0 {
		url = url[:i] + "/" + url[i+1:]
	}
	seg := strings.Split(url, "/")
	if len(seg) < 3 { // need at least host/owner/repo
		return ""
	}
	return seg[len(seg)-2]
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

// Branch is a candidate for a new worktree: a local branch not attached
// to any worktree, or a remote-only branch (Ref then holds the remote
// ref, e.g. "origin/feat").
type Branch struct {
	Name string
	Ref  string
}

// CandidateBranches returns the branches a new worktree could check out:
// unattached local branches first, then remote-only branches. Branches
// already checked out in a worktree (including the main one) are
// excluded — adding them would fail anyway.
func CandidateBranches(dir string) []Branch {
	seen := make(map[string]bool)
	var locals, remotes []Branch
	if out, err := run(dir, "branch", "--format=%(refname:short)\t%(worktreepath)"); err == nil && out != "" {
		for _, l := range strings.Split(out, "\n") {
			name, wt, _ := strings.Cut(l, "\t")
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if strings.TrimSpace(wt) == "" {
				locals = append(locals, Branch{Name: name})
			}
		}
	}
	if out, err := run(dir, "branch", "-r", "--format=%(refname:short)"); err == nil && out != "" {
		for _, l := range strings.Split(out, "\n") {
			ref := strings.TrimSpace(l)
			// origin/HEAD shortens to a bare remote name; only lines
			// with a remote prefix are branches.
			_, name, ok := strings.Cut(ref, "/")
			if !ok || name == "" || name == "HEAD" || seen[name] {
				continue
			}
			seen[name] = true
			remotes = append(remotes, Branch{Name: name, Ref: ref})
		}
	}
	sort.Slice(locals, func(i, j int) bool { return locals[i].Name < locals[j].Name })
	sort.Slice(remotes, func(i, j int) bool { return remotes[i].Name < remotes[j].Name })
	return append(locals, remotes...)
}

// RemoteBranchRef returns "origin/<branch>" when the branch exists on
// origin, or "".
func RemoteBranchRef(dir, branch string) string {
	if _, err := run(dir, "show-ref", "--verify", "refs/remotes/origin/"+branch); err == nil {
		return "origin/" + branch
	}
	return ""
}

// AddWorktree checks out an existing branch into a new worktree at path.
// Quiet: git's feedback quotes arbitrary commit subjects ("HEAD is now at
// ..."), which reads as if iwt said it; the caller prints its own notice.
func AddWorktree(dir, path, branch string) error {
	return runLoud(dir, "worktree", "add", "--quiet", path, branch)
}

// AddWorktreeNewBranch creates branch off base and checks it out into a new
// worktree at path. Quiet for the same reason as AddWorktree.
func AddWorktreeNewBranch(dir, path, branch, base string) error {
	return runLoud(dir, "worktree", "add", "--quiet", "-b", branch, path, base)
}

// AddWorktreeTrack creates branch tracking the remote ref and checks it
// out into a new worktree at path.
func AddWorktreeTrack(dir, path, branch, remoteRef string) error {
	return runLoud(dir, "worktree", "add", "--quiet", "--track", "-b", branch, path, remoteRef)
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
