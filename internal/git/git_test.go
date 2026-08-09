package git

import (
	"os"
	"path/filepath"
	"reflect"
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
	mustGit(origin, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")
	mustGit(origin, "branch", "remote-only")
	mustGit(dir, "clone", "-q", origin, repo)
	mustGit(repo, "branch", "free")
	mustGit(repo, "worktree", "add", "-q", "-b", "attached", filepath.Join(dir, "wt"))

	got := CandidateBranches(repo)
	// main is checked out in the main worktree and "attached" in a linked
	// one: both excluded. "free" (unattached local) comes before the
	// remote-only branch.
	want := []Branch{
		{Name: "free"},
		{Name: "remote-only", Ref: "origin/remote-only"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CandidateBranches() = %+v, want %+v", got, want)
	}

	if ref := RemoteBranchRef(repo, "remote-only"); ref != "origin/remote-only" {
		t.Errorf("RemoteBranchRef(remote-only) = %q", ref)
	}
	if ref := RemoteBranchRef(repo, "free"); ref != "" {
		t.Errorf("RemoteBranchRef(free) = %q, want \"\"", ref)
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
