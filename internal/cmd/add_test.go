package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateWorktreeRefusesUnusableBranchNames(t *testing.T) {
	muteStderr(t)
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	gitRun(t, tmp, "init", "-q", "-b", "main", repo)
	gitRun(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	t.Setenv("IWT_ROOT", filepath.Join(tmp, "wtroot"))

	for _, name := range []string{"release/*", "a b", "HEAD", "-x", "x..y"} {
		if _, err := createWorktree(repo, name, "", nil); err == nil {
			t.Errorf("createWorktree(%q) succeeded, want a refusal", name)
		}
	}
	if _, err := createWorktree(repo, "feature/ok", "", nil); err != nil {
		t.Errorf("createWorktree(feature/ok) = %v, want it created", err)
	}
}

// fixtureWithRemoteBranch builds a repository cloned from an origin that
// grew a branch afterwards, so the branch is on the remote and in none of
// the clone's refs.
func fixtureWithRemoteBranch(t *testing.T) (repo, origin string) {
	t.Helper()
	muteStderr(t)
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin = filepath.Join(tmp, "origin")
	repo = filepath.Join(tmp, "repo")
	gitRun(t, tmp, "init", "-q", "-b", "main", origin)
	gitRun(t, origin, "commit", "-q", "--allow-empty", "-m", "init")
	gitRun(t, tmp, "clone", "-q", origin, repo)
	for _, b := range []string{"theirs", "theirs-2"} {
		gitRun(t, origin, "checkout", "-q", "-b", b, "main")
		gitRun(t, origin, "commit", "-q", "--allow-empty", "-m", "work on "+b)
	}
	gitRun(t, origin, "checkout", "-q", "main")
	t.Setenv("IWT_ROOT", filepath.Join(tmp, "wtroot"))
	return repo, origin
}

// TestCreateWorktreeDecidesFromLocalRefs pins what a bare name means: the
// refs this repository already has, and nothing on a network. A name with
// no ref of its own becomes a new branch — the trade this makes, since a
// remote may have that name already — while a tracking ref is followed.
func TestCreateWorktreeDecidesFromLocalRefs(t *testing.T) {
	repo, _ := fixtureWithRemoteBranch(t)

	// On the remote, in none of this repository's refs: a branch of its
	// own, off main, with no upstream.
	path, err := createWorktree(repo, "theirs", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if head := gitOut(t, path, "log", "-1", "--format=%s"); head != "init" {
		t.Errorf("worktree is at %q, want a new branch off main (%q)", head, "init")
	}
	if up, err := gitTry(repo, "rev-parse", "--abbrev-ref", "theirs@{upstream}"); err == nil {
		t.Errorf("a new branch was given the upstream %q", up)
	}

	// Once a tracking ref exists, the same kind of name follows it.
	gitRun(t, repo, "fetch", "-q", "origin")
	path, err = createWorktree(repo, "theirs-2", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if head := gitOut(t, path, "log", "-1", "--format=%s"); head != "work on theirs-2" {
		t.Errorf("worktree is at %q, want the tracked branch's commit", head)
	}
	if up := gitOut(t, path, "rev-parse", "--abbrev-ref", "theirs-2@{upstream}"); up != "origin/theirs-2" {
		t.Errorf("upstream = %q, want origin/theirs-2", up)
	}
}

// TestAddRemoteTracksThatRemotesBranch covers the explicit lookup: the
// remote is named, so its branch of that name is what gets checked out —
// from the refs already fetched, with the upstream to match.
func TestAddRemoteTracksThatRemotesBranch(t *testing.T) {
	repo, _ := fixtureWithRemoteBranch(t)
	gitRun(t, repo, "fetch", "-q", "origin")

	rb, err := remoteBranchNamed(repo, "origin", "theirs")
	if err != nil {
		t.Fatal(err)
	}
	if rb.Remote != "origin" || rb.Ref != "refs/remotes/origin/theirs" {
		t.Fatalf("remoteBranchNamed = %+v, want origin's tracking ref", rb)
	}
	path, err := createWorktree(repo, "theirs", "", &rb)
	if err != nil {
		t.Fatal(err)
	}
	if head := gitOut(t, path, "log", "-1", "--format=%s"); head != "work on theirs" {
		t.Errorf("worktree is at %q, want the remote branch's commit", head)
	}
	if up := gitOut(t, path, "rev-parse", "--abbrev-ref", "theirs@{upstream}"); up != "origin/theirs" {
		t.Errorf("upstream = %q, want origin/theirs", up)
	}
}

// TestAddRemoteSaysWhatToRunRatherThanFetch pins that the lookup reaches no
// network: a branch the remote has but this repository has not fetched is
// reported as missing here, with the command that would bring it.
func TestAddRemoteSaysWhatToRunRatherThanFetch(t *testing.T) {
	repo, _ := fixtureWithRemoteBranch(t)

	before := gitOut(t, repo, "for-each-ref", "--format=%(refname) %(objectname)")
	_, err := remoteBranchNamed(repo, "origin", "theirs")
	if err == nil {
		t.Fatal("remoteBranchNamed fetched or invented a branch, want it to report the branch missing")
	}
	if msg := err.Error(); !strings.Contains(msg, "git fetch origin") {
		t.Errorf("error %q does not say what to run", msg)
	}
	if after := gitOut(t, repo, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
		t.Errorf("refs changed, so something was fetched:\n%s", after)
	}

	// An existing local branch is not something --remote may redefine.
	gitRun(t, repo, "branch", "mine")
	if _, err := remoteBranchNamed(repo, "origin", "mine"); err == nil {
		t.Error("remoteBranchNamed over an existing branch succeeded, want an error")
	}
}

// TestAddRemoteTellsAmbiguousFromAbsent pins that the two ways --remote can
// come up empty read differently: a ref that is not here yet is worth
// fetching for, while one shared with another mapping is not — fetching
// again would leave it just as unclear whose commit it holds.
func TestAddRemoteTellsAmbiguousFromAbsent(t *testing.T) {
	repo, origin := fixtureWithRemoteBranch(t)
	tmp := filepath.Dir(repo)
	backup := filepath.Join(tmp, "backup")
	gitRun(t, tmp, "clone", "-q", "--bare", origin, backup)
	gitRun(t, repo, "remote", "add", "backup", backup)
	gitRun(t, repo, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/shared/*")
	gitRun(t, repo, "config", "remote.backup.fetch", "+refs/heads/*:refs/remotes/shared/*")
	gitRun(t, repo, "fetch", "-q", "--all")

	_, err := remoteBranchNamed(repo, "origin", "theirs")
	if err == nil {
		t.Fatal("remoteBranchNamed on a shared tracking ref succeeded, want a refusal")
	}
	if msg := err.Error(); strings.Contains(msg, "git fetch") {
		t.Errorf("error %q sends the user to fetch, which would not settle it", msg)
	}

	// While a name genuinely absent still points at fetching.
	_, err = remoteBranchNamed(repo, "origin", "never-pushed")
	if err == nil || !strings.Contains(err.Error(), "git fetch origin") {
		t.Errorf("error for an absent branch = %v, want it to say what to run", err)
	}
}
