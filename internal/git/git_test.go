package git

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseWorktrees(t *testing.T) {
	out := `worktree /Users/x/repo
HEAD 1111111111111111111111111111111111111111
branch refs/heads/develop

worktree /Users/x/repo-wt/feature-foo
HEAD 2222222222222222222222222222222222222222
branch refs/heads/feature/foo

worktree /Users/x/repo-wt/detached
HEAD 3333333333333333333333333333333333333333
detached`

	got := parseWorktrees(out)
	want := []Worktree{
		{Path: "/Users/x/repo", Head: "1111111111111111111111111111111111111111", Branch: "develop", Main: true},
		{Path: "/Users/x/repo-wt/feature-foo", Head: "2222222222222222222222222222222222222222", Branch: "feature/foo"},
		{Path: "/Users/x/repo-wt/detached", Head: "3333333333333333333333333333333333333333"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseWorktrees() = %+v, want %+v", got, want)
	}
}

func TestParseWorktreesEmpty(t *testing.T) {
	if got := parseWorktrees(""); len(got) != 0 {
		t.Errorf("parseWorktrees(\"\") = %+v, want empty", got)
	}
}

func TestConfigOriginURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `[core]
	repositoryformatversion = 0
[remote "upstream"]
	url = git@github.com:other/repo.git
[remote "origin"]
	fetch = +refs/heads/*:refs/remotes/origin/*
	url = git@github.com:utahta/intellij-wt.git
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := configOriginURL(path); got != "git@github.com:utahta/intellij-wt.git" {
		t.Errorf("configOriginURL() = %q", got)
	}
	if got := configOriginURL(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("configOriginURL(missing) = %q, want \"\"", got)
	}
}

func TestConfigOriginURLNoOrigin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("[core]\n\tbare = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := configOriginURL(path); got != "" {
		t.Errorf("configOriginURL(no origin) = %q, want \"\"", got)
	}
}

func TestCandidateBranches(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "remote-only")
	mustGit(dir, "clone", "-q", origin, repo)
	mustGit(repo, "branch", "free")
	mustGit(repo, "worktree", "add", "-q", "-b", "attached", filepath.Join(dir, "wt"))

	checkCandidates := func(desc string, want []RemoteBranch) {
		t.Helper()
		if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: CandidateBranches() = %+v, want %+v", desc, got, want)
		}
	}
	checkRefs := func(desc, branch string, want []RemoteBranch) {
		t.Helper()
		if got := RemoteBranchRefs(repo, branch); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: RemoteBranchRefs(%s) = %v, want %v", desc, branch, got, want)
		}
	}

	// main is checked out in the main worktree and "attached" in a linked
	// one: both excluded. "free" (unattached local) comes before the
	// remote-only branch.
	checkCandidates("initial", []RemoteBranch{
		{Name: "free"},
		{Remote: "origin", Name: "remote-only", Ref: "refs/remotes/origin/remote-only"},
	})
	checkRefs("initial", "remote-only", []RemoteBranch{
		{Remote: "origin", Name: "remote-only", Ref: "refs/remotes/origin/remote-only"},
	})
	checkRefs("initial", "free", nil)

	// A name on several remotes yields one candidate per remote, but
	// origin is preferred outright when resolving a bare name.
	upstream := filepath.Join(dir, "upstream")
	mustGit(dir, "clone", "-q", "--bare", origin, upstream)
	mustGit(repo, "remote", "add", "upstream", upstream)
	mustGit(repo, "fetch", "-q", "upstream")
	checkCandidates("two remotes", []RemoteBranch{
		{Name: "free"},
		{Remote: "origin", Name: "remote-only", Ref: "refs/remotes/origin/remote-only"},
		{Remote: "upstream", Name: "remote-only", Ref: "refs/remotes/upstream/remote-only"},
	})
	checkRefs("two remotes", "remote-only", []RemoteBranch{
		{Remote: "origin", Name: "remote-only", Ref: "refs/remotes/origin/remote-only"},
	})

	// A remote whose name merely starts with "origin/" is not origin:
	// with no real origin left, nothing may claim its preference, and
	// candidate names must not degrade into fake "team/..." entries.
	mustGit(repo, "remote", "rename", "origin", "origin/team")
	checkCandidates("slashed remote name", []RemoteBranch{
		{Name: "free"},
		{Remote: "origin/team", Name: "remote-only", Ref: "refs/remotes/origin/team/remote-only"},
		{Remote: "upstream", Name: "remote-only", Ref: "refs/remotes/upstream/remote-only"},
	})
	checkRefs("slashed remote name", "remote-only", []RemoteBranch{
		{Remote: "origin/team", Name: "remote-only", Ref: "refs/remotes/origin/team/remote-only"},
		{Remote: "upstream", Name: "remote-only", Ref: "refs/remotes/upstream/remote-only"},
	})

	// A custom fetch refspec moves the tracking namespace: candidates
	// and refs must follow it, not the remote's name.
	mustGit(repo, "config", "remote.upstream.fetch", "+refs/heads/*:refs/remotes/vendor/*")
	mustGit(repo, "fetch", "-q", "upstream")
	wantCustom := []RemoteBranch{
		{Name: "free"},
		{Remote: "origin/team", Name: "remote-only", Ref: "refs/remotes/origin/team/remote-only"},
		{Remote: "upstream", Name: "remote-only", Ref: "refs/remotes/vendor/remote-only"},
	}
	wantCustomRefs := []RemoteBranch{
		{Remote: "origin/team", Name: "remote-only", Ref: "refs/remotes/origin/team/remote-only"},
		{Remote: "upstream", Name: "remote-only", Ref: "refs/remotes/vendor/remote-only"},
	}
	checkCandidates("custom refspec", wantCustom)
	checkRefs("custom refspec", "remote-only", wantCustomRefs)

	// A duplicated fetch refspec must not double the refs (which would
	// misreport a single remote as "multiple remotes"), and two distinct
	// destinations of one remote are equivalent mirrors: one ref per
	// remote, one candidate per name.
	mustGit(repo, "config", "--add", "remote.upstream.fetch", "+refs/heads/*:refs/remotes/vendor/*")
	checkRefs("duplicated refspec", "remote-only", wantCustomRefs)
	mustGit(repo, "config", "--add", "remote.upstream.fetch", "+refs/heads/*:refs/remotes/vendor2/*")
	mustGit(repo, "fetch", "-q", "upstream")
	checkCandidates("two destinations", wantCustom)
	checkRefs("two destinations", "remote-only", wantCustomRefs)
}

func TestNestedBranchStandardLayout(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "feature/foo")
	mustGit(dir, "clone", "-q", origin, repo)

	// for-each-ref globs stop at "/": the scan must still find nested
	// branches under the standard refspec.
	want := []RemoteBranch{{Remote: "origin", Name: "feature/foo", Ref: "refs/remotes/origin/feature/foo"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a nested branch = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "feature/foo"); !reflect.DeepEqual(refs, want) {
		t.Errorf("RemoteBranchRefs(feature/foo) = %v, want %v", refs, want)
	}
}

func TestNarrowedFetchRefspecs(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "release/one")
	mustGit(dir, "clone", "-q", origin, repo)

	// A narrowed glob shifts the branch-name prefix: release/one is
	// tracked at refs/remotes/vendor/one.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/release/*:refs/remotes/vendor/*")
	mustGit(repo, "fetch", "-q", "origin")
	want := []RemoteBranch{{Remote: "origin", Name: "release/one", Ref: "refs/remotes/vendor/one"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a narrowed refspec = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "release/one"); !reflect.DeepEqual(refs, []RemoteBranch{{Remote: "origin", Name: "release/one", Ref: "refs/remotes/vendor/one"}}) {
		t.Errorf("RemoteBranchRefs(release/one) = %v", refs)
	}
	// main is no longer covered by the refspec: its stale clone-time ref
	// must not resurface through the conventional-layout fallback.
	if refs := RemoteBranchRefs(repo, "main"); len(refs) != 0 {
		t.Errorf("RemoteBranchRefs(main) with a narrowed refspec = %v, want empty", refs)
	}

	// An exact refspec maps a single branch, name and ref verbatim.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/release/one:refs/remotes/pin/first")
	mustGit(repo, "fetch", "-q", "origin")
	want = []RemoteBranch{{Remote: "origin", Name: "release/one", Ref: "refs/remotes/pin/first"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with an exact refspec = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "release/one"); !reflect.DeepEqual(refs, []RemoteBranch{{Remote: "origin", Name: "release/one", Ref: "refs/remotes/pin/first"}}) {
		t.Errorf("RemoteBranchRefs with an exact refspec = %v", refs)
	}

	// A partial-component glob (qa*) needs the wildcard kept in the ref
	// scan, and the bare-prefix branch "qa" is an empty-suffix match.
	mustGit(origin, "branch", "qa")
	mustGit(origin, "branch", "qa1")
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/qa*:refs/remotes/origin/qa*")
	mustGit(repo, "fetch", "-q", "origin")
	want = []RemoteBranch{
		{Remote: "origin", Name: "qa", Ref: "refs/remotes/origin/qa"},
		{Remote: "origin", Name: "qa1", Ref: "refs/remotes/origin/qa1"},
	}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a partial-component glob = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "qa1"); !reflect.DeepEqual(refs, []RemoteBranch{{Remote: "origin", Name: "qa1", Ref: "refs/remotes/origin/qa1"}}) {
		t.Errorf("RemoteBranchRefs(qa1) = %v", refs)
	}

	// A branch merely named .../HEAD is a real branch; only the remote's
	// root HEAD symref is excluded.
	mustGit(origin, "branch", "release/HEAD")
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/release/*:refs/remotes/origin/release/*")
	mustGit(repo, "fetch", "-q", "origin")
	want = []RemoteBranch{
		{Remote: "origin", Name: "release/HEAD", Ref: "refs/remotes/origin/release/HEAD"},
		{Remote: "origin", Name: "release/one", Ref: "refs/remotes/origin/release/one"},
	}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a nested HEAD branch = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "release/HEAD"); !reflect.DeepEqual(refs, []RemoteBranch{{Remote: "origin", Name: "release/HEAD", Ref: "refs/remotes/origin/release/HEAD"}}) {
		t.Errorf("RemoteBranchRefs(release/HEAD) = %v", refs)
	}

	// The wildcard may sit mid-pattern on both sides.
	mustGit(origin, "branch", "release/a/head")
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/release/*/head:refs/remotes/vendor/release/*/head")
	mustGit(repo, "fetch", "-q", "origin")
	want = []RemoteBranch{{Remote: "origin", Name: "release/a/head", Ref: "refs/remotes/vendor/release/a/head"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a mid-pattern wildcard = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "release/a/head"); !reflect.DeepEqual(refs, []RemoteBranch{{Remote: "origin", Name: "release/a/head", Ref: "refs/remotes/vendor/release/a/head"}}) {
		t.Errorf("RemoteBranchRefs(release/a/head) = %v", refs)
	}

	// An unqualified exact source cannot be verified offline as a
	// branch — git's DWIM may resolve it to a tag of the same name —
	// so it yields no candidates, and no conventional fallback either.
	mustGit(repo, "config", "remote.origin.fetch", "release/one:refs/remotes/vendor/un")
	mustGit(repo, "fetch", "-q", "origin")
	if got := CandidateBranches(repo); len(got) != 0 {
		t.Errorf("CandidateBranches with an unqualified source = %+v, want empty", got)
	}
	if refs := RemoteBranchRefs(repo, "release/one"); len(refs) != 0 {
		t.Errorf("RemoteBranchRefs with an unqualified source = %+v, want empty", refs)
	}
}

func TestSameSourceMultipleDestinations(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "topic")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)

	// git resolves the upstream through the earliest refspec (z/*), so
	// the candidate must use that ref even though the ref scan meets
	// the lexicographically smaller a/* destination first.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/z/*")
	mustGit(repo, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/a/*")
	mustGit(repo, "fetch", "-q", "origin")
	want := []RemoteBranch{
		{Remote: "origin", Name: "main", Ref: "refs/remotes/origin/z/main"},
		{Remote: "origin", Name: "topic", Ref: "refs/remotes/origin/z/topic"},
	}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with two destinations = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "topic"); !reflect.DeepEqual(refs, want[1:]) {
		t.Errorf("RemoteBranchRefs(topic) = %+v, want %+v", refs, want[1:])
	}

	// When the first destination is gone and only a later mirror
	// remains, git still resolves the upstream to the missing ref: the
	// name has no usable upstream and must not be offered.
	mustGit(repo, "update-ref", "-d", "refs/remotes/origin/z/topic")
	want = want[:1]
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with the first destination missing = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "topic"); len(refs) != 0 {
		t.Errorf("RemoteBranchRefs(topic) with the first destination missing = %+v, want empty", refs)
	}
}

func TestBroadGlobWithSuffix(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "foo")
	mustGit(origin, "tag", "foo")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)

	// The source suffix consumes part of "refs/heads/": branch foo's
	// wildcard match is "heads" (no trailing slash), so it is stored at
	// .../origin/heads and must be found there. The tag foo also
	// matches the refspec but reconstructs to refs/tags/foo and is not
	// a branch.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/*/foo:refs/remotes/origin/*")
	mustGit(repo, "fetch", "-q", "origin")
	want := []RemoteBranch{
		{Remote: "origin", Name: "foo", Ref: "refs/remotes/origin/heads"},
	}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a suffixed broad glob = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "foo"); !reflect.DeepEqual(refs, want) {
		t.Errorf("RemoteBranchRefs(foo) = %+v, want %+v", refs, want)
	}
}

func TestBroadFetchRefspec(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "topic")
	mustGit(origin, "tag", "v1")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)

	// A broad refspec covers branches as its refs/heads/ subset: names
	// derive from that subset (not "heads/topic"), and tags are not
	// offered as branches.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/*:refs/remotes/origin/*")
	mustGit(repo, "fetch", "-q", "origin")
	want := []RemoteBranch{
		{Remote: "origin", Name: "main", Ref: "refs/remotes/origin/heads/main"},
		{Remote: "origin", Name: "topic", Ref: "refs/remotes/origin/heads/topic"},
	}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a broad refspec = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "topic"); !reflect.DeepEqual(refs, want[1:]) {
		t.Errorf("RemoteBranchRefs(topic) = %+v, want %+v", refs, want[1:])
	}

	// Explicit but unsupported refspecs must not fall back to the
	// conventional location: the stale refs under it belong to no
	// configured mapping. A tags-only pattern is kept as a blocker but
	// never scanned or offered.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/tags/*:refs/tags/*")
	posMaps, _ := trackingMapsByRemote(repo, []string{"origin"})
	for _, m := range posMaps["origin"] {
		if m.branchCapable() {
			t.Errorf("tags-only refspec parsed as branch-capable: %+v", m)
		}
	}
	if got := CandidateBranches(repo); len(got) != 0 {
		t.Errorf("CandidateBranches with an unsupported refspec = %+v, want empty", got)
	}
}

func TestTagsBlockerRefspec(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "tag", "v1")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)

	// The tags refspec comes first: refs under its destination are
	// claimed by it and must not be reinterpreted as branches by the
	// later heads refspec — the tag v1 is not offered as branch v1.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/tags/*:refs/remotes/origin/tags/*")
	mustGit(repo, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	mustGit(repo, "fetch", "-q", "--no-tags", "origin")
	want := []RemoteBranch{{Remote: "origin", Name: "main", Ref: "refs/remotes/origin/main"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a tags blocker = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "tags/v1"); len(refs) != 0 {
		t.Errorf("RemoteBranchRefs(tags/v1) = %+v, want empty", refs)
	}
}

func TestNegativeFetchRefspecs(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "topic")
	mustGit(origin, "branch", "private/secret")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)
	mustGit(repo, "fetch", "-q", "--no-tags", "origin")

	// A negative refspec added after a fetch leaves the excluded
	// tracking ref behind (fetch --prune does not remove it either),
	// but git will never update it again: it must not be offered.
	mustGit(repo, "config", "--add", "remote.origin.fetch", "^refs/heads/private/*")
	want := []RemoteBranch{
		{Remote: "origin", Name: "main", Ref: "refs/remotes/origin/main"},
		{Remote: "origin", Name: "topic", Ref: "refs/remotes/origin/topic"},
	}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a negative pattern = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "private/secret"); len(refs) != 0 {
		t.Errorf("RemoteBranchRefs(private/secret) = %+v, want empty", refs)
	}

	// Exact negatives (no wildcard) exclude a single source ref.
	mustGit(repo, "config", "--add", "remote.origin.fetch", "^refs/heads/topic")
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want[:1]) {
		t.Errorf("CandidateBranches with an exact negative = %+v, want %+v", got, want[:1])
	}
}

func TestNegativeRefspecKeepsAmbiguity(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "topic")
	mustGit(origin, "branch", "private/secret")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)
	mustGit(repo, "remote", "add", "backup", origin)
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/shared/*")
	mustGit(repo, "config", "remote.backup.fetch", "+refs/heads/*:refs/remotes/shared/*")
	mustGit(repo, "config", "--add", "remote.backup.fetch", "^refs/heads/private/*")
	mustGit(repo, "fetch", "-q", "--no-tags", "origin")
	mustGit(repo, "fetch", "-q", "--no-tags", "backup")

	// Both remotes fetch into refs/remotes/shared/*. backup's negative
	// stops it from writing private/* from now on, but does not erase
	// what a fetch before the negative existed may have written: the
	// ref stays ambiguous, only origin's reading survives as flagged.
	if refs := RemoteBranchRefs(repo, "topic"); len(refs) != 2 || !refs[0].Ambiguous || !refs[1].Ambiguous {
		t.Errorf("RemoteBranchRefs(topic) = %+v, want two ambiguous entries", refs)
	}
	want := []RemoteBranch{{Remote: "origin", Name: "private/secret", Ref: "refs/remotes/shared/private/secret", Ambiguous: true}}
	if refs := RemoteBranchRefs(repo, "private/secret"); !reflect.DeepEqual(refs, want) {
		t.Errorf("RemoteBranchRefs(private/secret) = %+v, want %+v", refs, want)
	}
	if got := CandidateBranches(repo); len(got) != 0 {
		t.Errorf("CandidateBranches = %+v, want empty", got)
	}
}

func TestNegatedBlockerStillBlocks(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "frozen/x")
	mustGit(origin, "tag", "v1")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)

	// The tag v1 is fetched into frozen/ while no negative exists yet.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/tags/*:refs/remotes/origin/frozen/*")
	mustGit(repo, "fetch", "-q", "--no-tags", "origin")

	// Excluding the tags now does not delete that write: frozen/v1
	// still holds the tag's commit and must not be reinterpreted as
	// branch frozen/v1 by the later heads refspec — the negated
	// mapping keeps its claim on everything under frozen/, including
	// frozen/x, which cannot be told apart from a stale tag locally.
	mustGit(repo, "config", "--add", "remote.origin.fetch", "^refs/tags/*")
	mustGit(repo, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	mustGit(repo, "fetch", "-q", "--no-tags", "origin")
	want := []RemoteBranch{{Remote: "origin", Name: "main", Ref: "refs/remotes/origin/main"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a negated blocker = %+v, want %+v", got, want)
	}
	for _, name := range []string{"frozen/v1", "frozen/x"} {
		if refs := RemoteBranchRefs(repo, name); len(refs) != 0 {
			t.Errorf("RemoteBranchRefs(%s) = %+v, want empty", name, refs)
		}
	}
}

func TestNegativeWithUnqualifiedSource(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "foo")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)

	// An unqualified exact source ("foo") is DWIM-resolved on the
	// remote, so git matches negatives against the resolved ref
	// (^refs/heads/foo excludes it; a raw ^foo matches nothing). iwt
	// cannot resolve it locally and already treats such mappings as
	// non-candidate blockers, which covers both readings; the ref they
	// claim must stay blocked, not fall through to later refspecs.
	mustGit(repo, "config", "remote.origin.fetch", "foo:refs/remotes/origin/bar")
	mustGit(repo, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	mustGit(repo, "fetch", "-q", "--no-tags", "origin")
	mustGit(repo, "config", "--add", "remote.origin.fetch", "^refs/heads/foo")
	want := []RemoteBranch{{Remote: "origin", Name: "main", Ref: "refs/remotes/origin/main"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with an unqualified source = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "bar"); len(refs) != 0 {
		t.Errorf("RemoteBranchRefs(bar) = %+v, want empty", refs)
	}
}

func TestOverlappingRefspecOrder(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "b/x")
	mustGit(dir, "clone", "-q", origin, repo)

	// The conventional refspec comes first; a narrowed one maps a/* into
	// the overlapping origin/b/* namespace. refs/remotes/origin/b/x was
	// created by the first refspec, and like git, name resolution must
	// follow refspec order: the ref reads back as b/x, not a/x.
	mustGit(repo, "config", "--add", "remote.origin.fetch", "+refs/heads/a/*:refs/remotes/origin/b/*")
	mustGit(repo, "fetch", "-q", "origin")
	want := []RemoteBranch{{Remote: "origin", Name: "b/x", Ref: "refs/remotes/origin/b/x"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with overlapping destinations = %+v, want %+v", got, want)
	}
	if refs := RemoteBranchRefs(repo, "b/x"); !reflect.DeepEqual(refs, []RemoteBranch{{Remote: "origin", Name: "b/x", Ref: "refs/remotes/origin/b/x"}}) {
		t.Errorf("RemoteBranchRefs(b/x) = %v", refs)
	}
	if refs := RemoteBranchRefs(repo, "a/x"); len(refs) != 0 {
		t.Errorf("RemoteBranchRefs(a/x) = %v, want empty", refs)
	}

	// Destinations overlapping across remotes: a ref that two remotes
	// fetch into holds whichever remote was fetched last, so no
	// interpretation can vouch for its commit — such refs are dropped
	// entirely rather than offered under either reading.
	up := filepath.Join(dir, "up")
	mustGit(dir, "init", "-q", "-b", "main", up)
	mustGit(up, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(up, "branch", "topic")
	mustGit(repo, "remote", "add", "upstream", up)
	mustGit(repo, "config", "remote.upstream.fetch", "+refs/heads/*:refs/remotes/origin/vendor/*")
	mustGit(repo, "fetch", "-q", "upstream")
	want = []RemoteBranch{
		{Remote: "origin", Name: "b/x", Ref: "refs/remotes/origin/b/x"},
	}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with cross-remote overlap = %+v, want %+v", got, want)
	}
	wantTopic := []RemoteBranch{{Remote: "upstream", Name: "topic", Ref: "refs/remotes/origin/vendor/topic", Ambiguous: true}}
	if refs := RemoteBranchRefs(repo, "topic"); !reflect.DeepEqual(refs, wantTopic) {
		t.Errorf("RemoteBranchRefs(topic) = %+v, want %+v", refs, wantTopic)
	}
	wantVendorTopic := []RemoteBranch{{Remote: "origin", Name: "vendor/topic", Ref: "refs/remotes/origin/vendor/topic", Ambiguous: true}}
	if refs := RemoteBranchRefs(repo, "vendor/topic"); !reflect.DeepEqual(refs, wantVendorTopic) {
		t.Errorf("RemoteBranchRefs(vendor/topic) = %+v, want %+v", refs, wantVendorTopic)
	}
}

func TestTagAndBranchWritersCollide(t *testing.T) {
	dir := t.TempDir()
	tagsrc := filepath.Join(dir, "tagsrc")
	headsrc := filepath.Join(dir, "headsrc")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", tagsrc)
	mustGit(tagsrc, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(tagsrc, "tag", "v1")
	mustGit(dir, "init", "-q", "-b", "main", headsrc)
	mustGit(headsrc, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(headsrc, "branch", "v1")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "tags", tagsrc)
	mustGit(repo, "config", "remote.tags.fetch", "+refs/tags/*:refs/remotes/shared/*")
	mustGit(repo, "remote", "add", "heads", headsrc)
	mustGit(repo, "config", "remote.heads.fetch", "+refs/heads/*:refs/remotes/shared/*")
	mustGit(repo, "fetch", "-q", "--no-tags", "heads")
	mustGit(repo, "fetch", "-q", "--no-tags", "tags")

	// refs/remotes/shared/v1 is written by both a tag and a branch:
	// the tag side counts as a writer even though it never yields a
	// branch interpretation, so the single branch reading is not safe.
	if got := CandidateBranches(repo); len(got) != 0 {
		t.Errorf("CandidateBranches with tag/branch writers colliding = %+v, want empty", got)
	}
	want := []RemoteBranch{{Remote: "heads", Name: "v1", Ref: "refs/remotes/shared/v1", Ambiguous: true}}
	if refs := RemoteBranchRefs(repo, "v1"); !reflect.DeepEqual(refs, want) {
		t.Errorf("RemoteBranchRefs(v1) = %+v, want %+v", refs, want)
	}
}

func TestDirectRefAtHeadPath(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)

	// An exact refspec may store a plain commit ref at the HEAD path;
	// only a symbolic ref is the remote-HEAD alias to skip.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/HEAD")
	mustGit(repo, "fetch", "-q", "origin")
	want := []RemoteBranch{{Remote: "origin", Name: "main", Ref: "refs/remotes/origin/HEAD"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a direct ref at the HEAD path = %+v, want %+v", got, want)
	}
	if rbs := RemoteBranchRefs(repo, "main"); !reflect.DeepEqual(rbs, want) {
		t.Errorf("RemoteBranchRefs(main) = %+v, want %+v", rbs, want)
	}
}

func TestAddWorktreeTrackingOverlap(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	upstream := filepath.Join(dir, "upstream")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(dir, "clone", "-q", "--bare", origin, upstream)
	mustGit(upstream, "branch", "topic")
	mustGit(dir, "clone", "-q", origin, repo)
	// upstream fetches into origin's namespace: refs/remotes/origin/up/*
	// is covered by both remotes' refspecs.
	mustGit(repo, "remote", "add", "upstream", upstream)
	mustGit(repo, "config", "remote.upstream.fetch", "+refs/heads/*:refs/remotes/origin/up/*")
	mustGit(repo, "fetch", "-q", "upstream")

	// refs/remotes/origin/up/topic is covered by both remotes' refspecs:
	// its content depends on fetch order, so it is not offered as a
	// candidate — but resolution still sees it, flagged, so "ambiguous"
	// is not mistaken for "does not exist".
	wantAmbiguous := []RemoteBranch{{Remote: "upstream", Name: "topic", Ref: "refs/remotes/origin/up/topic", Ambiguous: true}}
	if rbs := RemoteBranchRefs(repo, "topic"); !reflect.DeepEqual(rbs, wantAmbiguous) {
		t.Fatalf("RemoteBranchRefs(topic) = %+v, want %+v", rbs, wantAmbiguous)
	}
	if got := CandidateBranches(repo); len(got) != 0 {
		t.Fatalf("CandidateBranches with a shared destination = %+v, want empty", got)
	}
	rb := RemoteBranch{Remote: "upstream", Name: "topic", Ref: "refs/remotes/origin/up/topic"}

	// The user's pull strategy must carry over, as --track would have
	// applied it.
	mustGit(repo, "config", "branch.autoSetupRebase", "always")
	// Tracking must be in place before the checkout so post-checkout
	// hooks observe the upstream.
	hookOut := filepath.Join(dir, "hook-upstream")
	hook := "#!/bin/sh\ngit rev-parse --abbrev-ref 'topic@{upstream}' > '" + hookOut + "' 2>&1\n"
	if err := os.WriteFile(filepath.Join(repo, ".git", "hooks", "post-checkout"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}

	// git's own auto-tracking would abort on the ambiguously covered
	// start ref (even without --track); the explicit configuration must
	// go through and record the caller's chosen remote.
	if err := AddWorktreeTracking(repo, filepath.Join(dir, "wt"), "topic", rb); err != nil {
		t.Fatalf("AddWorktreeTracking() = %v", err)
	}
	if remote, _ := run(repo, "config", "branch.topic.remote"); remote != "upstream" {
		t.Errorf("branch.topic.remote = %q, want upstream", remote)
	}
	if merge, _ := run(repo, "config", "branch.topic.merge"); merge != "refs/heads/topic" {
		t.Errorf("branch.topic.merge = %q, want refs/heads/topic", merge)
	}
	if rebase, _ := run(repo, "config", "branch.topic.rebase"); rebase != "true" {
		t.Errorf("branch.topic.rebase = %q, want true", rebase)
	}
	if b, err := os.ReadFile(hookOut); err != nil || strings.TrimSpace(string(b)) != "origin/up/topic" {
		t.Errorf("post-checkout hook saw upstream %q (err=%v), want origin/up/topic", strings.TrimSpace(string(b)), err)
	}
}

func TestNoFetchConfigYieldsNoCandidates(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")

	// A remote without any fetch refspec cannot track anything: git
	// resolves @{upstream} through the configured refspecs, so a
	// worktree created from an assumed layout would end up with an
	// unresolvable upstream. Leftover refs yield no candidates.
	mustGit(repo, "config", "remote.up.url", filepath.Join(dir, "nowhere"))
	sha, err := run(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	mustGit(repo, "update-ref", "refs/remotes/up/feature/foo", sha)

	if got := CandidateBranches(repo); len(got) != 0 {
		t.Errorf("CandidateBranches with a config-less remote = %+v, want empty", got)
	}
	if refs := RemoteBranchRefs(repo, "feature/foo"); len(refs) != 0 {
		t.Errorf("RemoteBranchRefs(feature/foo) = %+v, want empty", refs)
	}
}

func TestUncreatableBranchNames(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	sha, err := run(origin, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// A remote may expose refs/heads/HEAD (a valid ref name), but git
	// refuses "HEAD" as a local branch name: no candidate.
	mustGit(origin, "update-ref", "refs/heads/HEAD", sha)
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "remote", "add", "origin", origin)
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/HEAD:refs/remotes/vendor/special")
	mustGit(repo, "fetch", "-q", "origin")
	if got := CandidateBranches(repo); len(got) != 0 {
		t.Errorf("CandidateBranches with a remote refs/heads/HEAD = %+v, want empty", got)
	}

	// git also refuses branch names beginning with "-": such remote
	// refs must not become candidates either — they could only fail at
	// creation. The fetched refs/heads/HEAD lands at the conventional
	// HEAD path here (as a plain ref, not a symref) and is likewise
	// excluded by name.
	mustGit(origin, "update-ref", "refs/heads/-x", sha)
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	mustGit(repo, "fetch", "-q", "origin")
	want := []RemoteBranch{{Remote: "origin", Name: "main", Ref: "refs/remotes/origin/main"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with dash/HEAD remote refs = %+v, want %+v", got, want)
	}
}

func TestOwnerFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:utahta/intellij-wt.git", "utahta"},
		{"https://github.com/utahta/intellij-wt.git", "utahta"},
		{"https://github.com/utahta/intellij-wt", "utahta"},
		{"ssh://git@github.com/utahta/intellij-wt.git", "utahta"},
		{"git@gitlab.com:group/subgroup/repo.git", "subgroup"},
		{"https://github.com/repo.git", ""},
		{"git@github.com:repo.git", ""},
		{"/path/to/origin", ""},
		{"file:///path/to/origin", ""},
		{"../origin", ""},
	}
	for _, tt := range tests {
		if got := ownerFromURL(tt.url); got != tt.want {
			t.Errorf("ownerFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestAmbiguousBranchAndTagName(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(repo, "branch", "release")
	mustGit(repo, "tag", "release")

	// git shortens ref names only as far as stays unambiguous, so with a
	// tag of the same name the branch reads as "heads/release" — a name
	// no branch answers to. Checking that out would create a second
	// branch called "heads/release" off the default branch instead.
	want := []RemoteBranch{{Name: "release"}}
	if got := CandidateBranches(repo); !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches with a branch and tag of one name = %+v, want %+v", got, want)
	}
	// The same shortening would keep the branch from ever matching the
	// name asked about, leaving a merged branch looking unmerged.
	if !IsMerged(repo, "release", "main") {
		t.Error("IsMerged(release, main) = false, want true")
	}
}

func refValue(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := run(dir, "rev-parse", ref)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCheckBranchName(t *testing.T) {
	dir := t.TempDir()
	if _, err := run(dir, "init", "-q", "-b", "main", "repo"); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	for _, name := range []string{"topic", "feature/x"} {
		if err := CheckBranchName(repo, name); err != nil {
			t.Errorf("CheckBranchName(%q) = %v, want it accepted", name, err)
		}
	}
	// A pattern would match refs on the remote that nothing asked about;
	// the rest git refuses as branch names of its own accord.
	for _, name := range []string{"release/*", "a b", "-x", "HEAD", "", "x..y"} {
		if err := CheckBranchName(repo, name); err == nil {
			t.Errorf("CheckBranchName(%q) = nil, want it refused", name)
		}
	}
}

func TestRunLoudLetsHooksPrompt(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", repo)
	mustGit(repo, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")

	// A hook that asks something and waits for the answer prints no
	// newline. Held back until a line ended, the question would never
	// reach the terminal and the hook would wait on an answer nobody knew
	// to give, so commands that run hooks write to it directly.
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf 'Continue? '\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "stderr.txt")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = f
	err = AddWorktreeNewBranch(repo, filepath.Join(dir, "wt"), "topic", "main")
	os.Stderr = old
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	printed, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(printed), "Continue? ") {
		t.Errorf("the hook's prompt did not reach the terminal: %q", printed)
	}
}

func TestRemoteBranchRefForNamesItsRemote(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "shared")
	mustGit(dir, "clone", "-q", origin, repo)
	mustGit(dir, "clone", "-q", "--bare", origin, filepath.Join(dir, "backup"))
	mustGit(repo, "remote", "add", "backup", filepath.Join(dir, "backup"))
	mustGit(repo, "fetch", "-q", "backup")

	// Both remotes have it, and a bare name resolves to origin's by
	// preference. Naming a remote has to override that, or --remote backup
	// would hand back origin's commit.
	if rb, found, ambiguous := RemoteBranchRefFor(repo, "backup", "shared"); !found || ambiguous || rb.Ref != "refs/remotes/backup/shared" {
		t.Errorf("RemoteBranchRefFor(backup, shared) = %+v, %v, %v, want backup's ref", rb, found, ambiguous)
	}
	if rb := RemoteBranchRefs(repo, "shared"); len(rb) != 1 || rb[0].Remote != "origin" {
		t.Errorf("RemoteBranchRefs(shared) = %+v, want origin's by preference", rb)
	}
	if _, found, ambiguous := RemoteBranchRefFor(repo, "backup", "nope"); found || ambiguous {
		t.Errorf("RemoteBranchRefFor(backup, nope) = %v, %v, want neither", found, ambiguous)
	}
}

func branchList(t *testing.T, dir string) string {
	t.Helper()
	out, err := run(dir, "branch", "--format=%(refname:lstrip=2)")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFetchFollowsTheRepositorysConfiguration(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "theirs")
	mustGit(dir, "clone", "-q", origin, repo)
	mustGit(origin, "branch", "-D", "theirs")

	// Nothing is asked for beyond the fetch itself, so a repository that
	// has not been told to prune keeps the tracking ref of a branch the
	// remote no longer has.
	if err := Fetch(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := run(repo, "rev-parse", "--verify", "refs/remotes/origin/theirs"); err != nil {
		t.Errorf("a tracking ref went away without being asked to: %v", err)
	}

	// And a repository that has been told to prune does prune: the
	// configuration decides, the same way it would from the command line.
	mustGit(repo, "config", "fetch.prune", "true")
	if err := Fetch(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := run(repo, "rev-parse", "--verify", "refs/remotes/origin/theirs"); err == nil {
		t.Error("fetch.prune was set and the stale tracking ref stayed")
	}

	// The branch on the remote arrives either way.
	mustGit(origin, "branch", "fresh")
	if err := Fetch(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := run(repo, "rev-parse", "--verify", "refs/remotes/origin/fresh"); err != nil {
		t.Errorf("the fetch did not bring the remote's branch: %v", err)
	}
}

func TestRemoteBranchRefForTellsAmbiguousFromAbsent(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	backup := filepath.Join(dir, "backup")
	repo := filepath.Join(dir, "repo")
	mustGit := func(d string, args ...string) {
		t.Helper()
		if _, err := run(d, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustGit(dir, "init", "-q", "-b", "main", origin)
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "foo")
	mustGit(dir, "clone", "-q", origin, repo)
	mustGit(dir, "clone", "-q", "--bare", origin, backup)
	mustGit(repo, "remote", "add", "backup", backup)

	// origin keeps a destination of its own and mirrors into one it shares
	// with backup. The shared ref is ambiguous — whose commit it holds
	// depends on fetch order — while the private one is the ref git
	// resolves the branch through, and it sorts after the shared one, so
	// stopping at the first match would miss it.
	mustGit(repo, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/z-own/*")
	mustGit(repo, "config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/a-shared/*")
	mustGit(repo, "config", "remote.backup.fetch", "+refs/heads/*:refs/remotes/a-shared/*")
	mustGit(repo, "fetch", "-q", "--all")

	rb, found, ambiguous := RemoteBranchRefFor(repo, "origin", "foo")
	if !found || ambiguous || rb.Ref != "refs/remotes/z-own/foo" {
		t.Errorf("RemoteBranchRefFor(origin, foo) = %+v, %v, %v, want origin's own ref", rb, found, ambiguous)
	}

	// backup has nothing but the shared ref: refs exist, and none of them
	// says what it holds. That is not the same as having none, and no
	// further fetch would change it.
	_, found, ambiguous = RemoteBranchRefFor(repo, "backup", "foo")
	if found || !ambiguous {
		t.Errorf("RemoteBranchRefFor(backup, foo) = %v, %v, want ambiguous", found, ambiguous)
	}

	// A name no ref carries at all: absent, plainly.
	_, found, ambiguous = RemoteBranchRefFor(repo, "origin", "never-pushed")
	if found || ambiguous {
		t.Errorf("RemoteBranchRefFor(origin, never-pushed) = %v, %v, want neither", found, ambiguous)
	}
}
