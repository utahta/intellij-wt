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

// ShortRef renders a fully qualified remote-tracking ref for display.
func ShortRef(ref string) string {
	return strings.TrimPrefix(ref, "refs/remotes/")
}

// RemoteBranch is a branch a worktree could check out: a remote-tracking
// branch (its short name, the remote it belongs to, and the fully
// qualified tracking ref — short names can collide with local branches
// or tags; use ShortRef for display), or, as a candidate from
// CandidateBranches, a local branch with Remote and Ref empty.
type RemoteBranch struct {
	Remote string
	Name   string
	Ref    string
	// Ambiguous marks a ref that several remotes fetch into: it cannot
	// be offered as a candidate (its commit depends on fetch order),
	// but resolution must still see it — "exists but ambiguous" is not
	// "does not exist".
	Ambiguous bool
}

// remoteTrackingBranches lists every remote-tracking branch, resolved
// through each remote's fetch refspecs, one entry per remote and branch
// name (several fetch destinations of one remote mirror the same
// branches, so extras are equivalent, not distinct). It runs a constant
// number of git invocations regardless of the remote count.
func remoteTrackingBranches(dir string) []RemoteBranch {
	remotes := remoteNames(dir)
	if len(remotes) == 0 {
		return nil
	}
	maps, negs := trackingMapsByRemote(dir, remotes)

	args := []string{"for-each-ref", "--format=%(refname)\t%(symref)"}
	for _, r := range remotes {
		for _, m := range maps[r] {
			if !m.branchCapable() {
				continue // blockers resolve, but need no refs scanned
			}
			if m.exact {
				args = append(args, m.dstPrefix)
			} else {
				// for-each-ref globs stop at "/" (unlike refspec
				// globs), missing nested branches like feature/foo.
				// Scan the enclosing literal directory instead, which
				// matches recursively; branchName does the precise
				// matching.
				if i := strings.LastIndex(m.dstPrefix, "/"); i >= 0 {
					args = append(args, m.dstPrefix[:i+1])
				} else {
					args = append(args, "refs/")
				}
			}
		}
	}
	// Without a single usable mapping there is nothing to scan — and a
	// pattern-less for-each-ref would enumerate every ref in the repo.
	if len(args) == 2 {
		return nil
	}
	out, err := run(dir, args...)
	if err != nil || out == "" {
		return nil
	}

	var branches []RemoteBranch
	for _, l := range strings.Split(out, "\n") {
		ref, symref, _ := strings.Cut(strings.TrimSpace(l), "\t")
		// A symbolic ref (conventionally the remote's HEAD) is an alias
		// of another branch, not a branch of its own. The pathname does
		// not decide: an exact refspec may store a real branch at a
		// HEAD-like path, and a branch named .../HEAD may exist.
		if symref != "" {
			continue
		}
		// Each remote resolves the ref through its own refspecs, first
		// match in config order — git's own rule; the first refspec
		// whose destination covers the ref claims it, even when its
		// source is not a branch (a tags blocker): later refspecs must
		// not reinterpret. Every claiming remote is a writer, branch
		// interpretation or not. Remotes have no order between them.
		type hit struct {
			remote, name string
			idx          int
		}
		var writers []hit
		for _, r := range remotes {
			for i, m := range maps[r] {
				name, matched := m.branchName(ref)
				if !matched {
					continue
				}
				// A negated source is no longer fetched, but negatives
				// do not delete what earlier fetches wrote: the mapping
				// may have written this ref before the negative existed,
				// so its claim stands — it just cannot vouch for the
				// content anymore, so never as a candidate. Only branch
				// readings need checking (blockers have nothing to lose),
				// which also keeps the comparison on fully qualified
				// source refs — the resolved form git matches negatives
				// against verbatim, without DWIM.
				if name != "" && negated(negs[r], m.source(ref)) {
					name = ""
				}
				writers = append(writers, hit{r, name, i}) // name may be ""
				break
			}
		}
		// A ref that several remotes fetch into holds whichever remote
		// was fetched last: no interpretation can vouch for the commit
		// it points at. Its branch-shaped readings are kept, flagged,
		// so resolution can tell "ambiguous" from "does not exist".
		if len(writers) > 1 {
			for _, h := range writers {
				if validBranchName(h.name) {
					branches = append(branches, RemoteBranch{Remote: h.remote, Name: h.name, Ref: ref, Ambiguous: true})
				}
			}
			continue
		}
		if len(writers) == 0 {
			continue
		}
		// The candidate must also be the ref git resolves the name to —
		// the first refspec whose source matches it. When that
		// destination is missing (a stale later mirror is all that
		// remains), the name has no usable upstream and is not offered.
		h := writers[0]
		if validBranchName(h.name) && firstSourceMatch(maps[h.remote], h.name) == h.idx {
			branches = append(branches, RemoteBranch{Remote: h.remote, Name: h.name, Ref: ref})
		}
	}
	return branches
}

// validBranchName reports whether name can be a local branch: never
// empty, never ending in "/" (a bare namespace ref), and not a ref name
// git refuses as a branch — "HEAD" alone (nested names like release/HEAD
// remain fine) or a leading dash.
func validBranchName(name string) bool {
	return name != "" && name != "HEAD" && name[0] != '-' && !strings.HasSuffix(name, "/")
}

// firstSourceMatch returns the index of the first refspec whose source
// covers branch name n — the one git resolves the name through.
func firstSourceMatch(ms []refspecMap, n string) int {
	full := "refs/heads/" + n
	for i, m := range ms {
		if m.exact {
			if m.srcPrefix == full {
				return i
			}
			continue
		}
		if len(full) >= len(m.srcPrefix)+len(m.srcSuffix) &&
			strings.HasPrefix(full, m.srcPrefix) && strings.HasSuffix(full, m.srcSuffix) {
			return i
		}
	}
	return -1
}

// branchName reports whether the map's destination covers ref (matched)
// and, if so, the branch name it represents — empty when the fetched
// source is not a branch, in which case the ref is claimed without
// becoming a candidate.
func (m refspecMap) branchName(ref string) (name string, matched bool) {
	if m.exact {
		if ref != m.dstPrefix {
			return "", false
		}
	} else if len(ref) < len(m.dstPrefix)+len(m.dstSuffix) ||
		!strings.HasPrefix(ref, m.dstPrefix) || !strings.HasSuffix(ref, m.dstSuffix) {
		return "", false
	}
	// Reconstruct the source ref the fetch used (the src parts are kept
	// verbatim, so a wildcard may consume any part of "refs/heads/");
	// only sources under refs/heads/ are branches — a broad glob also
	// covers tags and other namespaces, and an exact source may name a
	// tag or an unqualified DWIM ref.
	if n, ok := strings.CutPrefix(m.source(ref), "refs/heads/"); ok {
		return n, true
	}
	return "", true
}

// source returns the remote source ref the fetch stores at ref; the
// map's destination must cover ref (branchName matched).
func (m refspecMap) source(ref string) string {
	if m.exact {
		return m.srcPrefix
	}
	x := ref[len(m.dstPrefix) : len(ref)-len(m.dstSuffix)]
	return m.srcPrefix + x + m.srcSuffix
}

// branchCapable reports whether the map can ever yield a branch; maps
// that cannot (e.g. tags-only refspecs) stay out of the ref scan but
// still participate in resolution as blockers.
func (m refspecMap) branchCapable() bool {
	if m.exact {
		return strings.HasPrefix(m.srcPrefix, "refs/heads/")
	}
	return strings.HasPrefix(m.srcPrefix, "refs/heads/") || strings.HasPrefix("refs/heads/", m.srcPrefix)
}

func remoteNames(dir string) []string {
	out, err := run(dir, "remote")
	if err != nil || out == "" {
		return nil
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names
}

// refspecMap is one fetch refspec's branch-to-tracking-ref mapping. Git
// allows a single wildcard anywhere in a pattern refspec, so both sides
// split around it, kept verbatim: source refs named srcPrefix+<x>+
// srcSuffix are tracked at dstPrefix+<x>+dstSuffix. An exact (glob-less)
// refspec maps the single branch srcPrefix to the ref dstPrefix.
type refspecMap struct {
	srcPrefix, srcSuffix string
	dstPrefix, dstSuffix string
	exact                bool
}

// negRefspec is one negative fetch refspec ("^refs/heads/private/*").
// Git fetches a source ref only when it matches a positive refspec and
// no negative one, so a negative is an exclusion over the remote's whole
// positive mapping, not an ordered blocker. It is kept verbatim: git
// matches it against fully qualified source refs without DWIM-resolving
// it (an unqualified "^foo" matches nothing).
type negRefspec struct {
	prefix, suffix string
	exact          bool
}

// matches reports whether the negative refspec excludes source ref src.
func (n negRefspec) matches(src string) bool {
	if n.exact {
		return src == n.prefix
	}
	return len(src) >= len(n.prefix)+len(n.suffix) &&
		strings.HasPrefix(src, n.prefix) && strings.HasSuffix(src, n.suffix)
}

// negated reports whether src is excluded by any negative fetch refspec.
func negated(negs []negRefspec, src string) bool {
	for _, n := range negs {
		if n.matches(src) {
			return true
		}
	}
	return false
}

// trackingMapsByRemote maps each remote to its branch-tracking layout,
// parsed from all fetch refspecs in one git invocation. Narrowed globs
// ("+refs/heads/release/*:refs/remotes/vendor/*") shift the branch-name
// prefix, so both sides of the refspec matter. Refspecs outside these
// shapes are skipped, and a remote without usable refspecs yields no
// mappings at all: git resolves @{upstream} through the configured
// refspecs, so refs iwt merely assumed a layout for could never track.
// Negative refspecs come back separately per remote: they exclude
// sources from every positive mapping rather than mapping anything.
func trackingMapsByRemote(dir string, remotes []string) (map[string][]refspecMap, map[string][]negRefspec) {
	maps := make(map[string][]refspecMap, len(remotes))
	negs := make(map[string][]negRefspec)
	if out, err := run(dir, "config", "--get-regexp", `^remote\..*\.fetch$`); err == nil && out != "" {
		for _, l := range strings.Split(out, "\n") {
			key, spec, ok := strings.Cut(strings.TrimSpace(l), " ")
			if !ok {
				continue
			}
			name := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".fetch")
			if src, ok := strings.CutPrefix(spec, "^"); ok {
				// A negative refspec has a source only; one with a
				// destination or several wildcards is not one git
				// accepts, so it cannot mean anything here either.
				if strings.Contains(src, ":") || strings.Count(src, "*") > 1 {
					continue
				}
				p, s, glob := strings.Cut(src, "*")
				negs[name] = append(negs[name], negRefspec{prefix: p, suffix: s, exact: !glob})
				continue
			}
			src, dst, ok := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
			if !ok || dst == "" {
				continue
			}
			// The source stays verbatim in both shapes: branchName later
			// reconstructs the fetched source ref and keeps only
			// refs/heads/ matches, which uniformly handles qualified,
			// narrowed, broad, and mid-pattern globs — and exact sources
			// that are not branches (tags, or unqualified names resolved
			// by git's DWIM on the remote, which may pick refs/tags/v1 —
			// locally unverifiable). Maps that cannot yield branches are
			// kept regardless: an earlier refspec must block later ones
			// from reinterpreting its refs as branches.
			switch {
			case strings.Count(src, "*") == 1 && strings.Count(dst, "*") == 1:
				sp, ss, _ := strings.Cut(src, "*")
				dp, ds, _ := strings.Cut(dst, "*")
				maps[name] = append(maps[name], refspecMap{
					srcPrefix: sp,
					srcSuffix: ss,
					dstPrefix: dp,
					dstSuffix: ds,
				})
			case !strings.Contains(src, "*") && !strings.Contains(dst, "*"):
				maps[name] = append(maps[name], refspecMap{
					srcPrefix: src,
					dstPrefix: dst,
					exact:     true,
				})
			}
		}
	}
	return maps, negs
}

// CandidateBranches returns the branches a new worktree could check out:
// unattached local branches first, then remote-only branches. Branches
// already checked out in a worktree (including the main one) are
// excluded — adding them would fail anyway. A name existing on several
// remotes yields one candidate per remote: the selection is what
// disambiguates which ref to track.
func CandidateBranches(dir string) []RemoteBranch {
	local := make(map[string]bool)
	var locals, remotes []RemoteBranch
	if out, err := run(dir, "branch", "--format=%(refname:short)\t%(worktreepath)"); err == nil && out != "" {
		for _, l := range strings.Split(out, "\n") {
			name, wt, _ := strings.Cut(l, "\t")
			name = strings.TrimSpace(name)
			if name == "" || local[name] {
				continue
			}
			local[name] = true
			if strings.TrimSpace(wt) == "" {
				locals = append(locals, RemoteBranch{Name: name})
			}
		}
	}
	// A local branch of the same name wins: it would be checked out
	// regardless. Ambiguous refs are never offered.
	for _, rb := range remoteTrackingBranches(dir) {
		if rb.Ambiguous || local[rb.Name] {
			continue
		}
		remotes = append(remotes, rb)
	}
	sort.Slice(locals, func(i, j int) bool { return locals[i].Name < locals[j].Name })
	sort.Slice(remotes, func(i, j int) bool {
		if remotes[i].Name != remotes[j].Name {
			return remotes[i].Name < remotes[j].Name
		}
		if remotes[i].Ref != remotes[j].Ref {
			return remotes[i].Ref < remotes[j].Ref
		}
		return remotes[i].Remote < remotes[j].Remote
	})
	return append(locals, remotes...)
}

// RemoteBranchRefs returns the remote-tracking branches named branch,
// resolved through each remote's fetch refspecs, including entries
// flagged Ambiguous — callers must not treat those as absent. When
// nothing is ambiguous and origin has the branch, only origin's entry is
// returned, so more than one result means the name is genuinely
// ambiguous. Origin is matched by its exact remote name — remote names
// may contain slashes (e.g. a remote called "origin/team").
func RemoteBranchRefs(dir, branch string) []RemoteBranch {
	var rbs []RemoteBranch
	ambiguous := false
	for _, rb := range remoteTrackingBranches(dir) {
		if rb.Name != branch {
			continue
		}
		ambiguous = ambiguous || rb.Ambiguous
		rbs = append(rbs, rb)
	}
	if !ambiguous {
		for _, rb := range rbs {
			if rb.Remote == "origin" {
				return []RemoteBranch{rb}
			}
		}
	}
	return rbs
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

// AddWorktreeTracking creates branch at the remote-tracking ref rb.Ref,
// sets it up to track rb's remote branch, and checks it out into a new
// worktree at path — in that order, so post-checkout hooks observe the
// upstream, as they did under --track. Tracking is configured explicitly
// rather than via --track/autoSetupMerge: when fetch destinations
// overlap, git cannot map the start ref back to a single remote and
// aborts as ambiguous — the caller already knows which remote the
// selection belongs to.
func AddWorktreeTracking(dir, path, branch string, rb RemoteBranch) error {
	// "--" everywhere a candidate-derived name is passed: branch names
	// may legitimately begin with "-" and would otherwise parse as
	// options.
	if err := runLoud(dir, "branch", "--no-track", "--", branch, rb.Ref); err != nil {
		return err
	}
	if err := configureTracking(dir, branch, rb); err != nil {
		_, _ = run(dir, "branch", "-D", "--", branch)
		return err
	}
	if err := runLoud(dir, "worktree", "add", "--quiet", "--", path, branch); err != nil {
		_, _ = run(dir, "branch", "-D", "--", branch)
		return err
	}
	return nil
}

func configureTracking(dir, branch string, rb RemoteBranch) error {
	if err := runLoud(dir, "config", "branch."+branch+".remote", rb.Remote); err != nil {
		return err
	}
	if err := runLoud(dir, "config", "branch."+branch+".merge", "refs/heads/"+rb.Name); err != nil {
		return err
	}
	// --track would have applied branch.autoSetupRebase; replicate it.
	// The start point is always a remote-tracking ref here, so "local"
	// does not apply.
	if v, _ := run(dir, "config", "branch.autoSetupRebase"); v == "remote" || v == "always" {
		return runLoud(dir, "config", "branch."+branch+".rebase", "true")
	}
	return nil
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
