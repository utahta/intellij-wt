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

// worktreePath places worktrees under a sibling "<repo>-wt" directory:
// /path/to/repo -> /path/to/repo-wt/<branch> ("/" in branch becomes "-").
func worktreePath(root, branch string) string {
	name := filepath.Base(root)
	return filepath.Join(filepath.Dir(root), name+"-wt", strings.ReplaceAll(branch, "/", "-"))
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
