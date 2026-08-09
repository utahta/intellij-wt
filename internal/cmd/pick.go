package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/picker"
)

// pickCmd is the embedded-picker prototype: stage-1 repository selection
// rendered inline, for validating terminal compatibility (JediTerm, tmux,
// iTerm) before it replaces the fzf widgets. Hidden while experimental.
var pickCmd = &cobra.Command{
	Use:    "pick",
	Short:  "Prototype: select a repository with the built-in inline picker",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runPick,
}

func init() {
	rootCmd.AddCommand(pickCmd)
}

func runPick(cmd *cobra.Command, args []string) error {
	roots := discoverRoots()
	if len(roots) == 0 {
		return fmt.Errorf("no repositories found under the worktree root or $IWT_SEARCH_PATH")
	}
	if cur := resolveRoot("."); cur != "" {
		for i, r := range roots {
			if r == cur {
				copy(roots[1:i+1], roots[:i])
				roots[0] = cur
				break
			}
		}
	}
	labels := make([]string, len(roots))
	runParallel(len(roots), func(i int) {
		labels[i] = repoLabel(roots[i])
	})

	items := make([]picker.Item, len(roots))
	for i := range roots {
		items[i] = picker.Item{Label: labels[i], Detail: roots[i]}
	}
	it, err := picker.Pick("repo> ", "enter: print path | esc: abort", items)
	if err != nil {
		return err
	}
	fmt.Println(it.Detail)
	return nil
}
