package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func mkdirAll(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanRepos(t *testing.T) {
	tmp := t.TempDir()
	mkdirAll(t, tmp, "a", "repo1", ".git")
	mkdirAll(t, tmp, "a", "repo1", "vendor", "nested", ".git") // inside repo1: not descended into
	mkdirAll(t, tmp, "b", "c", "repo2")
	// a .git file (linked worktree / submodule style) also counts
	if err := os.WriteFile(filepath.Join(tmp, "b", "c", "repo2", ".git"), []byte("gitdir: /x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkdirAll(t, tmp, ".hidden", "repo3", ".git")                   // hidden: skipped
	mkdirAll(t, tmp, "d1", "d2", "d3", "d4", "d5", "deep", ".git") // beyond maxScanDepth: skipped

	got := scanRepos(tmp, 0)
	want := []string{
		filepath.Join(tmp, "a", "repo1"),
		filepath.Join(tmp, "b", "c", "repo2"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanRepos() = %v, want %v", got, want)
	}
}

func TestScanIwtRoot(t *testing.T) {
	tmp := t.TempDir()
	mkdirAll(t, tmp, "org1", "repoA", "repoA--x")
	mkdirAll(t, tmp, "org1", "repoA", "repoA--y")
	mkdirAll(t, tmp, "org2", "repoB", "repoB--z")
	// stray file at repo level: ignored
	if err := os.WriteFile(filepath.Join(tmp, "org1", "stray"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got := scanIwtRoot(tmp)
	want := []string{
		filepath.Join(tmp, "org1", "repoA", "repoA--x"),
		filepath.Join(tmp, "org1", "repoA", "repoA--y"),
		filepath.Join(tmp, "org2", "repoB", "repoB--z"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanIwtRoot() = %v, want %v", got, want)
	}
}

func TestScanIwtRootMissing(t *testing.T) {
	if got := scanIwtRoot(filepath.Join(t.TempDir(), "nope")); len(got) != 0 {
		t.Errorf("scanIwtRoot(missing) = %v, want empty", got)
	}
}

func TestResolveRootGitDir(t *testing.T) {
	tmp := t.TempDir()
	repo := mkdirAll(t, tmp, "repo")
	mkdirAll(t, repo, ".git")

	got := resolveRoot(repo)
	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("resolveRoot(%q) = %q, want %q", repo, got, want)
	}
}

func TestResolveRootNonRepo(t *testing.T) {
	if got := resolveRoot(t.TempDir()); got != "" {
		t.Errorf("resolveRoot(non-repo) = %q, want \"\"", got)
	}
}
