package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/utahta/intellij-wt/internal/git"
)

// iwtRoot returns the shared worktree root directory.
func iwtRoot() (string, error) {
	if p := os.Getenv("IWT_ROOT"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve worktree root: %w (set IWT_ROOT)", err)
	}
	return filepath.Join(home, ".intellij-wt", "worktrees"), nil
}

// gatherWorktrees returns the current repository's worktrees, or — when all
// is true or the working directory is outside a repository — those of every
// discovered repository.
func gatherWorktrees(all bool) ([]git.Worktree, []string, error) {
	if !all {
		if root, err := git.MainRoot("."); err == nil {
			wts, err := git.Worktrees(root)
			if err != nil {
				return nil, nil, err
			}
			return wts, defaultLabels(wts), nil
		}
	}
	return allWorktrees()
}

// allWorktrees returns the worktrees of every repository discovered under
// the shared worktree root and $IWT_SEARCH_PATH, labeled with org/repo.
// The current repository (when inside one) is always included, keeping the
// list a superset of the default single-repository selection.
func allWorktrees() ([]git.Worktree, []string, error) {
	candidates := []string{"."}
	if root, err := iwtRoot(); err == nil {
		candidates = append(candidates, scanIwtRoot(root)...)
	}
	for _, dir := range filepath.SplitList(os.Getenv("IWT_SEARCH_PATH")) {
		if dir != "" {
			candidates = append(candidates, scanRepos(dir, 0)...)
		}
	}

	seen := make(map[string]bool)
	var roots []string
	for _, c := range candidates {
		root, err := git.MainRoot(c)
		if err != nil || seen[root] {
			continue
		}
		seen[root] = true
		roots = append(roots, root)
	}
	sort.Strings(roots)

	var wts []git.Worktree
	var labels []string
	for _, root := range roots {
		ws, err := git.Worktrees(root)
		if err != nil {
			continue
		}
		org := git.OriginOwner(root)
		if org == "" {
			org = "_local"
		}
		prefix := org + "/" + filepath.Base(root)
		for _, w := range ws {
			wts = append(wts, w)
			labels = append(labels, prefix+"  "+worktreeLabel(w))
		}
	}
	if len(wts) == 0 {
		return nil, nil, fmt.Errorf("no repositories found under the worktree root or $IWT_SEARCH_PATH")
	}
	return wts, labels, nil
}

// scanIwtRoot returns the worktree directories under the shared root's
// fixed <org>/<repo>/<worktree> layout.
func scanIwtRoot(root string) []string {
	var dirs []string
	for _, org := range readDirs(root) {
		for _, repo := range readDirs(filepath.Join(root, org)) {
			for _, wt := range readDirs(filepath.Join(root, org, repo)) {
				dirs = append(dirs, filepath.Join(root, org, repo, wt))
			}
		}
	}
	return dirs
}

func readDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// maxScanDepth caps scanRepos against trees that contain no repositories
// at all (repositories themselves stop the descent much earlier).
const maxScanDepth = 5

// scanRepos returns git repositories under dir: directories containing a
// .git entry. It does not descend into repositories, hidden directories,
// symlinks, or deeper than maxScanDepth levels.
func scanRepos(dir string, depth int) []string {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return []string{dir}
	}
	if depth >= maxScanDepth {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var repos []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			repos = append(repos, scanRepos(filepath.Join(dir, e.Name()), depth+1)...)
		}
	}
	return repos
}
