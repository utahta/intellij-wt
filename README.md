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
                         fuzzy-selected when no argument is given.
iwt list                 List worktrees with dirty state and last commit time.
iwt prune                Select worktrees to remove (Tab to multi-select).
                         --merged removes all worktrees whose branch is merged
                         into origin's default branch, without prompting.
iwt path                 Select a worktree and print its path (for cd wrappers).
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

### cd into a worktree

A binary cannot change its parent shell's directory, so pair `iwt path` with a
shell function:

```zsh
function iwtcd() {
  local p
  p=$(iwt path) && cd "$p"
}
```
