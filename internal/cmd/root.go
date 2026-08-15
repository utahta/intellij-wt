// Package cmd implements the iwt subcommands.
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/utahta/intellij-wt/internal/git"
	"github.com/utahta/intellij-wt/internal/picker"
)

var rootCmd = &cobra.Command{
	Use:           "iwt",
	Short:         "Manage git worktrees and open them in IntelliJ IDEA",
	Version:       "0.3.0",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	err := rootCmd.Execute()
	// Neither cancelling a picker nor being told to stop is an error
	// worth reporting; the nonzero exit still stops && chains in scripts.
	if err != nil && !errors.Is(err, picker.ErrAbort) && !errors.Is(err, picker.ErrTerminated) {
		fmt.Fprintln(os.Stderr, paint("1;31", "iwt: "+err.Error()))
	}
	return err
}

// paint prepares a message for stderr, wrapped in an ANSI color (SGR
// code) when stderr is a terminal so notices stand out between picker
// redraws; NO_COLOR disables the color. Whatever the message is made of,
// nothing in it may steer the terminal, so escapes are neutralized here
// too — some messages carry text iwt never sees, like git's own output
// inside a wrapped error. Results on stdout stay untouched: shell
// wrappers cd into those.
func paint(code, s string) string {
	s = safeMessage(s)
	if os.Getenv("NO_COLOR") != "" {
		return s
	}
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

// safeMessage neutralizes what a terminal would act on while leaving a
// message's own shape intact: newlines and tabs lay out diagnostics —
// cobra's command suggestions arrive as several lines — so they stay,
// while an ESC or any other control character becomes U+FFFD. Values
// quoted inside a message get the stricter treatment of picker.Sanitize
// where they are put in: a path has no business adding a line of its own
// to iwt's output.
func safeMessage(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, s)
}

// worktreePath places worktrees under a shared root (default
// ~/.intellij-wt/worktrees, overridable with IWT_ROOT):
// <iwt-root>/<org>/<repo>/<repo>--<branch> ("/" in branch becomes "-").
// org comes from the origin remote URL, falling back to "_local". The leaf
// directory doubles as the IDEA project name, hence the repo prefix.
func worktreePath(root, branch string) (string, error) {
	base, err := iwtRoot()
	if err != nil {
		return "", err
	}
	org := git.OriginOwner(root)
	if org == "" {
		org = "_local"
	}
	name := filepath.Base(root)
	return filepath.Join(base, org, name, name+"--"+strings.ReplaceAll(branch, "/", "-")), nil
}

func selectEntry(entries []worktreeEntry, verb string) (git.Worktree, error) {
	items := make([]picker.Item, len(entries))
	for i, e := range entries {
		items[i] = picker.Item{Label: entryLabel(e), Detail: e.Path}
	}
	res, err := picker.Run(items, picker.Options{
		Prompt: "worktree> ",
		Keys:   []picker.KeyHint{{Key: "enter", Desc: verb, Tone: picker.TonePrimary}},
	})
	if err != nil {
		return git.Worktree{}, err
	}
	if res.Index < 0 {
		return git.Worktree{}, picker.ErrAbort
	}
	return entries[res.Index].Worktree, nil
}

func selectWorktrees(wts []git.Worktree, verb string) ([]git.Worktree, error) {
	items := make([]picker.Item, len(wts))
	for i, w := range wts {
		items[i] = picker.Item{Label: worktreeLabel(w), Detail: w.Path}
	}
	res, err := picker.Run(items, picker.Options{
		Prompt: "prune> ",
		Multi:  true,
		Keys: []picker.KeyHint{
			{Key: "enter", Desc: verb, Tone: picker.ToneDanger},
			{Key: "tab", Desc: "toggle"},
		},
	})
	if err != nil {
		return nil, err
	}
	selected := make([]git.Worktree, 0, len(res.Indices))
	for _, i := range res.Indices {
		selected = append(selected, wts[i])
	}
	return selected, nil
}

func worktreeLabel(w git.Worktree) string {
	label := w.Branch
	if label == "" {
		label = w.Head[:min(8, len(w.Head))] + " (detached)"
	}
	if w.Main {
		label += " (main)"
	}
	return label
}

// confirmFn is the question asked by destructive flows, replaced in
// tests.
var confirmFn = confirm

// confirm asks a yes/no question, answering No or failing — never both
// at once. A No calls off one step, so its caller may carry on with the
// rest; an error means no answer was given at all (the user cancelled,
// the process was told to stop, the terminal is unusable), and callers
// must abandon the work instead of reading it as a No and moving to the
// next item.
//
// On a terminal the answer comes from the picker, so terminal input is
// decoded by the library iwt already depends on rather than by a second
// reader of its own: escape sequences, 8-bit controls, replies a
// previous program's queries left behind and pastes are all its
// business, and none of them can spell an answer. "no" starts
// highlighted, so Enter keeps the default, while typing y or n narrows
// to one option as before; an answer matching neither option is a No.
//
// Non-terminal stdin (a pipe, a file, /dev/null) reads a line instead,
// for scripts and tests — EOF answers No, since automation that offers
// no answer means to decline, not to stop. A char-device check would not
// do here: /dev/null is one, and waiting for an answer would hang
// automation that redirected stdin precisely to avoid prompts.
func confirm(msg string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintf(os.Stderr, "%s [y/N]: ", paint("1;33", msg))
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		return isYes(line), nil
	}

	fmt.Fprintln(os.Stderr, paint("1;33", msg))
	items := []picker.Item{{Label: "no"}, {Label: "yes"}}
	res, err := picker.Run(items, picker.Options{
		Prompt: "confirm> ",
		Tone:   picker.ToneDanger,
		Keys:   []picker.KeyHint{{Key: "enter", Desc: "answer", Tone: picker.ToneDanger}},
	})
	if err != nil {
		return false, err
	}
	if res.Index < 0 {
		return false, nil // an answer that named neither option
	}
	return isYes(items[res.Index].Label), nil
}

// isYes reports whether an answer to a yes/no prompt is affirmative;
// everything else, an empty line included, keeps the default No.
func isYes(answer string) bool {
	s := strings.TrimSpace(answer)
	return strings.EqualFold(s, "y") || strings.EqualFold(s, "yes")
}
