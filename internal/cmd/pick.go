package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/utahta/intellij-wt/internal/git"
	"github.com/utahta/intellij-wt/internal/idea"
	"github.com/utahta/intellij-wt/internal/picker"
)

var (
	pickOpen bool
	pickCd   bool
)

// pickCmd is the staged picker built on the inline picker: repositories,
// then Tab into one repository's worktrees, with worktree creation and
// removal in open mode. It will replace the fzf widgets; hidden while
// experimental.
var pickCmd = &cobra.Command{
	Use:    "pick",
	Short:  "Staged repository/worktree picker (built-in inline UI)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runPick,
}

func init() {
	pickCmd.Flags().BoolVar(&pickOpen, "open", false, "open the selection in IDEA (with create/remove actions)")
	pickCmd.Flags().BoolVar(&pickCd, "cd", false, "print the selection's path (for cd wrappers)")
	rootCmd.AddCommand(pickCmd)
}

func runPick(cmd *cobra.Command, args []string) error {
	if pickOpen == pickCd {
		return fmt.Errorf("specify exactly one of --open or --cd")
	}

	verb := "cd"
	expect := []string{"ctrl+h"}
	keys2 := []picker.KeyHint{
		{Key: "enter", Desc: verb, Tone: picker.TonePrimary},
		{Key: "ctrl-h/esc", Desc: "back"},
	}
	if pickOpen {
		verb = "open in IDEA"
		expect = []string{"ctrl+h", "ctrl+n", "ctrl+d", "ctrl+x"}
		keys2 = []picker.KeyHint{
			{Key: "enter", Desc: verb, Tone: picker.TonePrimary},
			{Key: "ctrl-n", Desc: "new", Tone: picker.ToneCreate},
			{Key: "ctrl-d", Desc: "remove", Tone: picker.ToneDanger},
			{Key: "ctrl-x", Desc: "prune merged", Tone: picker.ToneDanger},
			{Key: "ctrl-h/esc", Desc: "back"},
		}
	}

	finish := func(path string) error {
		if pickOpen {
			return idea.Open(path)
		}
		fmt.Println(path)
		return nil
	}

stageOne:
	for {
		items, err := repoItems()
		if err != nil {
			return err
		}
		res, err := picker.Run(items, picker.Options{
			Prompt: "repo> ",
			Keys: []picker.KeyHint{
				{Key: "enter", Desc: verb, Tone: picker.TonePrimary},
				{Key: "tab", Desc: "worktrees"},
			},
			Expect: []string{"tab"},
		})
		if err != nil {
			return err
		}
		if res.Item == nil {
			return nil
		}
		if res.Key == "" {
			return finish(res.Item.Detail)
		}
		repo, label := res.Item.Detail, res.Item.Label

		for {
			entries, err := worktreesAt(repo)
			if err != nil {
				return err
			}
			items2 := make([]picker.Item, len(entries))
			for i, e := range entries {
				items2[i] = picker.Item{Label: worktreeLabel(e.Worktree), Detail: e.Path}
			}
			res2, err := picker.Run(items2, picker.Options{
				Prompt: label + " worktree> ",
				Keys:   keys2,
				Expect: expect,
			})
			if errors.Is(err, picker.ErrAbort) {
				continue stageOne // Esc: one level up, back to repositories
			}
			if err != nil {
				return err
			}
			switch res2.Key {
			case "ctrl+h":
				continue stageOne
			case "":
				if res2.Item == nil {
					return nil
				}
				return finish(res2.Item.Detail)
			case "ctrl+n":
				branch, err := pickBranch(repo)
				if errors.Is(err, picker.ErrAbort) || branch == "" {
					continue
				}
				if err != nil {
					return err
				}
				path, err := createWorktree(repo, branch, "")
				if err != nil {
					fmt.Fprintln(os.Stderr, paint("1;31", "iwt: "+err.Error()))
					continue
				}
				return finish(path)
			case "ctrl+d":
				if res2.Item != nil {
					if err := pruneTarget(res2.Item.Detail); err != nil {
						fmt.Fprintln(os.Stderr, paint("1;31", "iwt: "+err.Error()))
					}
				}
			case "ctrl+x":
				if err := pruneMergedWorktrees(repo); err != nil {
					fmt.Fprintln(os.Stderr, paint("1;31", "iwt: "+err.Error()))
				}
			}
		}
	}
}

// repoItems lists every discovered repository, the current one first so
// the picker starts with it highlighted.
func repoItems() ([]picker.Item, error) {
	roots := discoverRoots()
	if len(roots) == 0 {
		return nil, fmt.Errorf("no repositories found under the worktree root or $IWT_SEARCH_PATH")
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
	return items, nil
}

// pickBranch shows the new-worktree input box: pick an existing local or
// remote branch, or type a new name.
func pickBranch(repo string) (string, error) {
	branches := git.Branches(repo)
	items := make([]picker.Item, len(branches))
	for i, b := range branches {
		items[i] = picker.Item{Label: b}
	}
	res, err := picker.Run(items, picker.Options{
		Prompt: "new branch> ",
		Tone:   picker.ToneCreate,
		Keys: []picker.KeyHint{
			{Key: "enter", Desc: "create worktree (type a new name or pick)", Tone: picker.ToneCreate},
			{Key: "esc", Desc: "back"},
		},
	})
	if err != nil {
		return "", err
	}
	if res.Item != nil {
		return res.Item.Label, nil
	}
	return res.Query, nil
}
