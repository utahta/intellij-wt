# intellij-wt

Manage git worktrees and open them in IntelliJ IDEA, from the command line.

- `iwt add` creates a worktree and opens it in IDEA
- `iwt open` fuzzy-selects a worktree and opens it in IDEA — if the project
  window is already open, it is raised to the front instead
- `iwt prune` fuzzy-selects worktrees to remove, offering to delete merged branches

macOS only (window focusing relies on System Events).

## Install

```bash
go install github.com/utahta/intellij-wt/cmd/iwt@latest
```

To raise already-open project windows, grant the Accessibility permission
(System Settings → Privacy & Security → Accessibility) to the app you run
`iwt` from (e.g. iTerm, IntelliJ IDEA). Without it, `iwt open` falls back to
`open -a "IntelliJ IDEA"`.

## Usage

```
iwt add <branch> [base]  Create a worktree and open it in IDEA.
                         Existing branches are checked out as is; new branches are
                         created off [base] (default: origin's default branch).
                         .envrc files in the worktree are direnv-allowed automatically.
iwt open                 Select a worktree and open/raise it in IDEA.
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
