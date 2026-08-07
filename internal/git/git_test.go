package git

import (
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
