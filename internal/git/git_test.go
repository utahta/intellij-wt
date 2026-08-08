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
