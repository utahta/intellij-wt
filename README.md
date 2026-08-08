# intellij-wt

Manage git worktrees and open them in IntelliJ IDEA, from the command line.

- `iwt add` creates a worktree and opens it in IDEA
- `iwt open` fuzzy-selects a worktree and opens it in IDEA — if the project
  window is already open, it is raised to the front instead
- `iwt prune` fuzzy-selects worktrees to remove, offering to delete merged branches

macOS only.

## Install

```bash
go install github.com/utahta/intellij-wt/cmd/iwt@latest
```

Opening and raising project windows uses the `idea` command-line launcher,
looked up in this order:

1. the `IWT_IDEA_BIN` environment variable
2. `idea` on PATH
3. `~/Library/Application Support/JetBrains/Toolbox/scripts/idea`
4. `/Applications/IntelliJ IDEA.app/Contents/MacOS/idea`

Without it, `iwt` falls back to `open -a "IntelliJ IDEA"`, which activates
the app but may not raise the right project window.

## Usage

```
iwt add <branch> [base]  Create a worktree and open it in IDEA.
                         Existing branches are checked out as is; new branches are
                         created off [base] (default: origin's default branch).
                         .envrc files in the worktree are direnv-allowed automatically.
iwt open [branch|path]   Open/raise a worktree in IDEA: by branch name of the
                         current repo, by a directory inside any worktree, or
                         fuzzy-selected when no argument is given. --all selects
                         across every discovered repository.
iwt list                 List worktrees with dirty state and last commit time.
                         --all lists every discovered repository; --porcelain
                         prints org/repo<TAB>branch<TAB>path for scripts.
iwt prune                Select worktrees to remove (Tab to multi-select).
                         --merged removes all worktrees whose branch is merged
                         into origin's default branch, without prompting.
iwt path                 Select a worktree and print its path (for cd wrappers).
                         --all selects across every discovered repository.
iwt init zsh             Print zsh integration (iwtcd, Ctrl+O picker, optional
                         tmux glue — see Shell integration below).
```

Worktrees are placed under a shared root, organized by the origin remote's
owner: `~/.intellij-wt/worktrees/<org>/<repo>/<repo>--<branch>` (slashes in
branch names become dashes; repos without an origin go under `_local`).
Set `IWT_ROOT` to use a different root directory. The leaf directory name
doubles as the IDEA project name, so project windows are identifiable by
repo and branch.

Worktrees created by older versions (the sibling `<repo>-wt/` layout) keep
working — `list`, `open`, and `prune` operate on whatever `git worktree`
reports, regardless of location.

### Selecting across repositories

`iwt open --all` and `iwt path --all` select from every discovered
repository, always including the current one — this is also the default
when run outside a git repository.
Repositories are discovered from the worktrees under the shared root, plus
any git repositories found by scanning the colon-separated directories in
`IWT_SEARCH_PATH`:

```bash
export IWT_SEARCH_PATH="$HOME/go/src/github.com:$HOME/src"
```

The scan skips hidden directories and stops descending once it finds a
repository, so pointing it at a large source tree is cheap.

## Shell integration

Add to `.zshrc`:

```zsh
eval "$(iwt init zsh)"
```

This provides:

- A **Ctrl+O widget** that fuzzy-picks a worktree across all repositories
  and opens it in IDEA.
- A **Ctrl+G widget** that picks the same way and cd's into the
  selection. It runs `iwtcd --all`, which can also be called directly (a
  binary cannot change its parent shell's directory, hence a shell
  function); plain `iwtcd` picks within the current repository.

All pickers pipe `iwt list --porcelain` into fzf, so they follow your
usual fzf look and keybindings. When fzf is absent, `iwtcd` falls back
to the built-in finder and the widgets are skipped. Override the keys by
setting `IWT_OPEN_KEY` / `IWT_CD_KEY` before the eval line — but not
Ctrl+I, which is indistinguishable from Tab in terminals; Ctrl+S also
requires terminal flow control to be disabled first (`stty -ixon`).

With `--idea-tmux`, IDEA's built-in terminal (detected via
`TERMINAL_EMULATOR=JetBrains-JediTerm`) attaches to a tmux session named
after the current worktree, creating it when needed — each worktree
keeps its own session, and switching projects with `iwt open` reattaches
to where you left off.

With `--idea-tmux-autostart <cmd>` (implies `--idea-tmux`), each freshly
created session runs `<cmd>` once, right before the first prompt:

```zsh
eval "$(iwt init zsh --idea-tmux-autostart claude)"
```

The result: `iwt add <branch>` opens IDEA on a new worktree with a
terminal attached to its own tmux session and the agent already running.

## Tips

### Open the Terminal tool window by default

Save a window layout that has the Terminal tool window open and make it
the default (Window → Layouts, IDEA 2023.1+). Worktree projects created
by `iwt add` then show a terminal on their first open; reopened projects
restore whatever layout they were closed with.

### Fix misplaced IME preedit text in the IDE terminal

With the "Reworked" terminal engine, composing Japanese (or other IME)
input in a TUI app draws the uncommitted preedit string away from the
actual cursor position — committed text still lands correctly, but
composing is hard to follow. Switching Settings → Tools → Terminal →
Terminal engine to **Classic** fixes it.

Classic drops Reworked-only features such as command blocks and prompt
completion, but those are inactive inside full-screen TUI apps anyway,
so a tmux/agent-centric workflow loses nothing. (Observed on IntelliJ
IDEA 2026.2 / macOS / Apple Japanese IME as of 2026-08; newer versions
may fix the engine, so re-check before applying.)
