package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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

// worktreeEntry couples a worktree with its repository label. Repo is
// "org/repo" in cross-repository listings and empty in single-repository
// mode, where a shared prefix would only add fuzzy-match noise.
type worktreeEntry struct {
	git.Worktree
	Repo string
}

func entryLabel(e worktreeEntry) string {
	if e.Repo != "" {
		return e.Repo + "  " + worktreeLabel(e.Worktree)
	}
	return worktreeLabel(e.Worktree)
}

// gatherWorktrees returns the current repository's worktrees, or — when all
// is true or the working directory is outside a repository — those of every
// discovered repository.
func gatherWorktrees(all bool) ([]worktreeEntry, error) {
	if !all {
		if root, err := git.MainRoot("."); err == nil {
			wts, err := git.Worktrees(root)
			if err != nil {
				return nil, err
			}
			entries := make([]worktreeEntry, len(wts))
			for i, w := range wts {
				entries[i] = worktreeEntry{Worktree: w}
			}
			return entries, nil
		}
	}
	return allWorktrees()
}

// discoverRoots returns the sorted main worktree roots of every discovered
// repository: the current one (when inside a repository), those with
// worktrees under the shared root, and those found under $IWT_SEARCH_PATH.
func discoverRoots() []string {
	candidates := []string{"."}
	if root, err := iwtRoot(); err == nil {
		candidates = append(candidates, scanIwtRoot(root)...)
	}
	for _, dir := range filepath.SplitList(os.Getenv("IWT_SEARCH_PATH")) {
		if dir != "" {
			candidates = append(candidates, scanRepos(dir, 0)...)
		}
	}

	resolved := make([]string, len(candidates))
	runParallel(len(candidates), func(i int) {
		resolved[i] = resolveRoot(candidates[i])
	})

	seen := make(map[string]bool)
	var roots []string
	for _, root := range resolved {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

// repoLabel returns the org/repo label for a repository root.
func repoLabel(root string) string {
	org := git.OriginOwner(root)
	if org == "" {
		org = "_local"
	}
	return org + "/" + filepath.Base(root)
}

// worktreesAt returns the worktrees of the repository containing dir,
// labeled with org/repo.
func worktreesAt(dir string) ([]worktreeEntry, error) {
	root, err := git.MainRoot(dir)
	if err != nil {
		return nil, err
	}
	ws, err := git.Worktrees(root)
	if err != nil {
		return nil, err
	}
	prefix := repoLabel(root)
	entries := make([]worktreeEntry, len(ws))
	for i, w := range ws {
		entries[i] = worktreeEntry{Worktree: w, Repo: prefix}
	}
	return entries, nil
}

// allWorktrees returns the worktrees of every repository discovered under
// the shared worktree root and $IWT_SEARCH_PATH, labeled with org/repo.
// The current repository (when inside one) is always included, keeping the
// list a superset of the default single-repository selection.
func allWorktrees() ([]worktreeEntry, error) {
	roots := discoverRoots()

	type repoList struct {
		wts    []git.Worktree
		prefix string
	}
	lists := make([]repoList, len(roots))
	runParallel(len(roots), func(i int) {
		ws, err := git.Worktrees(roots[i])
		if err != nil {
			return
		}
		lists[i] = repoList{wts: ws, prefix: repoLabel(roots[i])}
	})

	var entries []worktreeEntry
	for _, l := range lists {
		for _, w := range l.wts {
			entries = append(entries, worktreeEntry{Worktree: w, Repo: l.prefix})
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no repositories found under the worktree root or $IWT_SEARCH_PATH")
	}
	return entries, nil
}

// resolveRoot returns the main worktree root for a candidate directory, or
// "" when it has none. A candidate whose .git entry is a directory is its
// own main root, so the git call is skipped; a .git file (linked worktree)
// still resolves through git.
func resolveRoot(dir string) string {
	if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil && fi.IsDir() {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return ""
		}
		if r, err := filepath.EvalSymlinks(abs); err == nil {
			abs = r
		}
		return abs
	}
	root, err := git.MainRoot(dir)
	if err != nil {
		return ""
	}
	return root
}

// scanParallelism bounds concurrent git invocations during discovery.
const scanParallelism = 16

func runParallel(n int, fn func(i int)) {
	sem := make(chan struct{}, scanParallelism)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}()
	}
	wg.Wait()
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
