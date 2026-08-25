# plasticity-modules

Modules for [`plst`](https://github.com/esmarkowski/plasticity).

```sh
plst install esmarkowski/plasticity-modules
```

One repo, several module binaries. `plst.json` declares them all, so a single
install registers every subcommand.

| module | what it does |
|---|---|
| `harness` | interchangeable sets of agent configuration |

## harness

A harness is a directory — usually a git repository — holding the parts an agent
reads. Using one links those parts into place and registers its hooks; using
another swaps them.

```sh
plst harness new rails                       # scaffold one
plst harness install esmarkowski/my-harness  # or clone one
plst harness use rails                       # link it in
plst harness use rails --project             # for this repository only
plst harness off                             # put back what it displaced
plst harness list
```

### Layout

```
rails/
  harness.json     name, description, hook declarations
  CLAUDE.md
  rules/           project scope only
  agents/
  skills/
  commands/
  hooks/           scripts, registered from harness.json
```

Components are read from the directory, not declared — adding `agents/` to a
harness is enough to have it linked.

### Symlinks, not copies

Each component becomes a symlink into the harness. Swapping is instant, nothing
drifts, and an edit made while a harness is active is an edit to the harness, so
its own git history is the record of it.

This was checked rather than assumed. A scratch project with a symlinked
`CLAUDE.md`, a symlinked `agents/` directory, and an `agents/` directory holding a
symlinked entry were each probed for what the agent reported loading. All three
loaded — directory links and entry links alike.

Only the known component paths are ever touched. The agent's configuration
directory also holds live state — a daemon lock, job directories, a history file,
plan files, caches — and none of it is a harness's business.

Anything real already at a component path is **moved aside, never deleted**, into
`~/.plasticity/harnesses/.parked/`, and put back by `plst harness off`.

### Rules are project scope only

The agent never reads rules from user scope. Measured: thirteen rule loads on the
machine this was written on, every one from a project's own `.claude/rules`, none
from `~/.claude/rules`. So `use` skips `rules/` at user scope and says so, rather
than creating a link that would look right and never be read.

Rules also load on `path_glob_match` — the `paths:` frontmatter has to match a
file touched during the session — so they are not loaded at session start at all.

### Hooks

Declared in `harness.json`, in the same shape the agent's own settings file uses.
Use `${HARNESS_ROOT}` rather than writing down a path, the same idea as the
agent's `${CLAUDE_PLUGIN_ROOT}`: a harness may be cloned anywhere, so a hook
command that names a path works on one machine.

```json
{
  "name": "rails",
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "${HARNESS_ROOT}/hooks/guard.sh" }] }
    ]
  }
}
```

Registrations are **merged**, not replaced. The settings file also holds the
model, the theme, the status line, enabled plugins, and any hooks you registered
by hand — a harness owns what it declared and nothing else. What was written is
recorded, so `off` removes exactly that rather than guessing.

Fields this program has never heard of — a `timeout`, a `statusMessage` — are
passed through untouched.

### Scopes

`user` is the agent's own configuration directory and is the default: a harness is
normally the whole way you work. `--project` acts on a repository's `.claude`,
found by walking up to the repository root, so it lands there rather than in
whichever subdirectory you typed the command in. The two are tracked
independently, so one project can run a different harness from everything else.
