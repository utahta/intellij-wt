// Package cmd implements the iwt subcommands.
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/utahta/intellij-wt/internal/git"
	"github.com/utahta/intellij-wt/internal/picker"
)

var rootCmd = &cobra.Command{
	Use:           "iwt",
	Short:         "Manage git worktrees and open them in IntelliJ IDEA",
	Version:       "0.1.0",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	err := rootCmd.Execute()
	// Cancelling a picker is not an error worth reporting; the nonzero
	// exit still stops && chains in scripts.
	if err != nil && !errors.Is(err, picker.ErrAbort) {
		fmt.Fprintln(os.Stderr, paint("1;31", "iwt: "+err.Error()))
	}
	return err
}

// paint wraps s in an ANSI color (SGR code) when stderr is a terminal, so
// notices stand out between fzf redraws. NO_COLOR disables it.
func paint(code, s string) string {
	if os.Getenv("NO_COLOR") != "" {
		return s
	}
	if fi, err := os.Stderr.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
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

// confirm asks a yes/no question. On a terminal it reads a single key
// from /dev/tty in raw mode and echoes it itself, so the answer is
// visible regardless of the surrounding terminal state (e.g. inside a
// zle widget, where echo is off). With piped stdin it reads a line, for
// scripts and tests.
func confirm(msg string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", paint("1;33", msg))

	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(line)
		return line == "y" || line == "Y"
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	old, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		return false
	}
	var buf [1]byte
	_, _ = tty.Read(buf[:])
	_ = term.Restore(int(tty.Fd()), old)
	fmt.Fprintf(os.Stderr, "%c\n", buf[0])
	return buf[0] == 'y' || buf[0] == 'Y'
}
