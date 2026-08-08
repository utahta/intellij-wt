// Package cmd implements the iwt subcommands.
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
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
	if err != nil && !errors.Is(err, fuzzyfinder.ErrAbort) {
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

func selectEntry(entries []worktreeEntry, header string) (git.Worktree, error) {
	idx, err := fuzzyfinder.Find(entries, func(i int) string {
		return entryLabel(entries[i])
	}, fuzzyfinder.WithHeader(header), previewPath(func(i int) string { return entries[i].Path }))
	if err != nil {
		return git.Worktree{}, err
	}
	return entries[idx].Worktree, nil
}

// previewPath shows the highlighted worktree's path in a preview window,
// keeping it out of the label so fuzzy matching only sees org/repo/branch.
func previewPath(path func(i int) string) fuzzyfinder.Option {
	return fuzzyfinder.WithPreviewWindow(func(i, _, _ int) string {
		if i < 0 {
			return ""
		}
		return path(i)
	})
}

func selectWorktrees(wts []git.Worktree, header string) ([]git.Worktree, error) {
	idxs, err := fuzzyfinder.FindMulti(wts, func(i int) string {
		return worktreeLabel(wts[i])
	}, fuzzyfinder.WithHeader(header), previewPath(func(i int) string { return wts[i].Path }))
	if err != nil {
		return nil, err
	}
	selected := make([]git.Worktree, 0, len(idxs))
	for _, i := range idxs {
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

func confirm(msg string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", paint("1;33", msg))
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	return line == "y" || line == "Y"
}
