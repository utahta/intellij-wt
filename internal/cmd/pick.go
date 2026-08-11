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

var pickCmd = &cobra.Command{
	Use:   "pick (--open | --cd)",
	Short: "Staged repository/worktree picker (backs the shell widgets)",
	Long: `Pick through the staged inline picker: repositories first, Tab drills
into the highlighted repository's worktrees, Esc backs out one level.

With --open, Enter opens the selection in IDEA, Ctrl+N creates a
worktree for a picked or typed branch, Ctrl+D removes the highlighted
worktree, and Ctrl+X prunes merged ones. With --cd, Enter prints the
selection's path for cd wrappers; the shell widgets from "iwt init zsh"
are thin bindings over these two modes.`,
	Args: cobra.NoArgs,
	RunE: runPick,
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
	var expect []string
	keys2 := []picker.KeyHint{
		{Key: "enter", Desc: verb, Tone: picker.TonePrimary},
		{Key: "esc", Desc: "back"},
	}
	if pickOpen {
		verb = "open in IDEA"
		expect = []string{"ctrl+n", "ctrl+d", "ctrl+x"}
		keys2 = []picker.KeyHint{
			{Key: "enter", Desc: verb, Tone: picker.TonePrimary},
			{Key: "ctrl-n", Desc: "new", Tone: picker.ToneCreate},
			{Key: "ctrl-d", Desc: "remove", Tone: picker.ToneDanger},
			{Key: "ctrl-x", Desc: "prune merged", Tone: picker.ToneDanger},
			{Key: "esc", Desc: "back"},
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
		if res.Index < 0 {
			return nil
		}
		if res.Key == "" {
			return finish(items[res.Index].Detail)
		}
		repo, label := items[res.Index].Detail, items[res.Index].Label

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
			case "":
				if res2.Index < 0 {
					return nil
				}
				return finish(items2[res2.Index].Detail)
			case "ctrl+n":
				branch, track, err := pickBranch(repo)
				// An empty branch means the input box was left empty —
				// but only once it is known that no error came back: on
				// one the branch is empty too, and being told to stop
				// would read as nothing having been typed.
				if err != nil {
					if errors.Is(err, picker.ErrAbort) {
						continue
					}
					return err
				}
				if branch == "" {
					continue
				}
				path, err := createWorktree(repo, branch, "", track)
				if err != nil {
					fmt.Fprintln(os.Stderr, paint("1;31", "iwt: "+err.Error()))
					continue
				}
				return finish(path)
			case "ctrl+d":
				if res2.Index >= 0 {
					if err := pruneTarget(items2[res2.Index].Detail); err != nil {
						if err = report(err); err != nil {
							return err
						}
					}
				}
			case "ctrl+x":
				if err := pruneMergedWorktrees(repo); err != nil {
					if err = report(err); err != nil {
						return err
					}
				}
			}
		}
	}
}

// report prints an error from an action taken inside the picker loop and
// returns nil, so the loop offers the picker again. A cancelled step is
// not worth a message; being told to stop is not the loop's to swallow,
// so that error comes back to end the run.
func report(err error) error {
	switch {
	case errors.Is(err, picker.ErrTerminated):
		return err
	case errors.Is(err, picker.ErrAbort):
		return nil
	}
	fmt.Fprintln(os.Stderr, paint("1;31", "iwt: "+err.Error()))
	return nil
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

// pickBranch shows the new-worktree input box: pick an unattached local
// or remote-only branch, or type a new name. The detail column tells the
// two apart; a name on several remotes appears once per remote, and the
// selection index recovers which remote branch to track (track is nil
// for local branches and typed names).
func pickBranch(repo string) (branch string, track *git.RemoteBranch, err error) {
	branches := git.CandidateBranches(repo)
	items := make([]picker.Item, len(branches))
	for i, b := range branches {
		detail := "local"
		if b.Ref != "" {
			// The remote name matters on its own: two remotes may fetch
			// the same branch into the same destination, and the choice
			// decides which one the new worktree tracks.
			detail = fmt.Sprintf("%s (%s)", git.ShortRef(b.Ref), b.Remote)
		}
		items[i] = picker.Item{Label: b.Name, Detail: detail}
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
		return "", nil, err
	}
	if res.Index >= 0 {
		b := branches[res.Index]
		if b.Ref != "" {
			return b.Name, &b, nil
		}
		return b.Name, nil, nil
	}
	return res.Query, nil, nil
}
