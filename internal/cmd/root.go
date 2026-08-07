// Package cmd implements the wt subcommands.
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ktr0731/go-fuzzyfinder"
	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
)

var rootCmd = &cobra.Command{
	Use:           "wt",
	Short:         "Manage git worktrees and open them in IntelliJ IDEA",
	Version:       "0.1.0",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

// worktreePath places worktrees under a shared root (default
// ~/.intellij-wt/worktrees, overridable with WT_ROOT):
// <wt-root>/<org>/<repo>/<repo>--<branch> ("/" in branch becomes "-").
// org comes from the origin remote URL, falling back to "_local". The leaf
// directory doubles as the IDEA project name, hence the repo prefix.
func worktreePath(root, branch string) (string, error) {
	base := os.Getenv("WT_ROOT")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve worktree root: %w (set WT_ROOT)", err)
		}
		base = filepath.Join(home, ".intellij-wt", "worktrees")
	}
	org := git.OriginOwner(root)
	if org == "" {
		org = "_local"
	}
	name := filepath.Base(root)
	return filepath.Join(base, org, name, name+"--"+strings.ReplaceAll(branch, "/", "-")), nil
}

func selectWorktree(wts []git.Worktree, header string) (git.Worktree, error) {
	idx, err := fuzzyfinder.Find(wts, func(i int) string {
		return worktreeLabel(wts[i])
	}, fuzzyfinder.WithHeader(header))
	if err != nil {
		return git.Worktree{}, err
	}
	return wts[idx], nil
}

func selectWorktrees(wts []git.Worktree, header string) ([]git.Worktree, error) {
	idxs, err := fuzzyfinder.FindMulti(wts, func(i int) string {
		return worktreeLabel(wts[i])
	}, fuzzyfinder.WithHeader(header))
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
	return label + "  " + w.Path
}

func confirm(msg string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", msg)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	return line == "y" || line == "Y"
}
