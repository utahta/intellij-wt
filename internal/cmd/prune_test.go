package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/utahta/intellij-wt/internal/git"
	"github.com/utahta/intellij-wt/internal/picker"
)

// gitTry runs git in dir and hands back its output and error, for the
// cases where failing is the answer being checked.
func gitTry(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// gitOut runs git in dir and returns its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestRemoveWorktreesStopsWhenAborted pins the difference between the two
// ways a question can go unanswered: declining a worktree skips it and
// the removals behind it go ahead, but cancelling — or the process being
// told to stop — must leave them alone, however clean they are.
func TestRemoveWorktreesStopsWhenAborted(t *testing.T) {
	muteStderr(t)
	// git reports worktree paths with symlinks resolved (/var is one on
	// macOS), so the fixture has to speak the same paths.
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "repo")
	dirty := filepath.Join(tmp, "wt-dirty")
	clean := filepath.Join(tmp, "wt-clean")

	gitRun(t, tmp, "init", "-q", "-b", "main", root)
	gitRun(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	gitRun(t, root, "worktree", "add", "-q", dirty, "-b", "dirty")
	gitRun(t, root, "worktree", "add", "-q", clean, "-b", "clean")
	if err := os.WriteFile(filepath.Join(dirty, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !git.IsDirty(dirty) {
		t.Fatal("fixture worktree is not dirty")
	}

	all, err := git.Worktrees(root)
	if err != nil {
		t.Fatal(err)
	}
	var wts []git.Worktree
	for _, want := range []string{dirty, clean} {
		for _, w := range all {
			if w.Path == want {
				wts = append(wts, w)
			}
		}
	}
	if len(wts) != 2 {
		t.Fatalf("found %d of the 2 fixture worktrees: %+v", len(wts), all)
	}

	for _, tt := range []struct {
		name    string
		answer  func(string) (bool, error)
		wantErr error
		asks    int
		gone    []string
	}{
		{
			name:    "declined",
			answer:  func(string) (bool, error) { return false, nil },
			asks:    2, // the dirty one, then the clean one's branch
			gone:    []string{clean},
			wantErr: nil,
		},
		{
			name:    "cancelled",
			answer:  func(string) (bool, error) { return false, picker.ErrAbort },
			asks:    1,
			wantErr: picker.ErrAbort,
		},
		{
			name:    "terminated",
			answer:  func(string) (bool, error) { return false, picker.ErrTerminated },
			asks:    1,
			wantErr: picker.ErrTerminated,
		},
	} {
		// Each case starts from the fixture as built: only the declined
		// case removes anything, and it runs last-effect-free by being
		// checked against its own expectations.
		asks := 0
		old := confirmFn
		confirmFn = func(msg string) (bool, error) {
			asks++
			return tt.answer(msg)
		}
		err := removeWorktrees(root, wts, false)
		confirmFn = old

		if !errors.Is(err, tt.wantErr) {
			t.Errorf("%s: removeWorktrees = %v, want %v", tt.name, err, tt.wantErr)
		}
		if asks != tt.asks {
			t.Errorf("%s: asked %d questions, want %d", tt.name, asks, tt.asks)
		}
		for _, w := range []string{dirty, clean} {
			_, statErr := os.Stat(w)
			shouldBeGone := false
			for _, g := range tt.gone {
				if g == w {
					shouldBeGone = true
				}
			}
			if shouldBeGone && statErr == nil {
				t.Errorf("%s: %s still present", tt.name, w)
			}
			if !shouldBeGone && statErr != nil {
				t.Errorf("%s: %s removed unasked", tt.name, w)
			}
		}
		if tt.name == "declined" {
			// Put the clean worktree back for the cases that follow.
			gitRun(t, root, "worktree", "add", "-q", clean, "clean")
		}
	}
}
