# Notty

A terminal markdown notes app with automatic GitHub sync, built in Go with
Bubble Tea.

![Notty](docs/screenshot.png)
<!-- TODO: replace with a real screenshot of the main screen once the UI has shipped. -->

## Features

- Folders and plain markdown notes, organized in a tree sidebar
- A built-in modal (vim) editor with live markdown styling, or a simple
  non-vim mode — plus one-key handoff to `$EDITOR`
- Real images in the terminal: full-resolution inline images in Kitty and
  Ghostty, half-block images everywhere else, and a full-screen viewer with
  Sixel and iTerm2 support
- Todo lists inside notes, with a global **Tasks** view grouped by due date
- Fuzzy finder (`ctrl+p`) and full-text search (`ctrl+f`)
- Tags (`#like-this`), pins, trash, and per-note history (powered by git)
- Automatic, invisible sync to a private GitHub repository, with an in-app
  conflict resolver for the rare cases that need one
- 7 themes: Catppuccin (Mocha / Latte), Tokyo Night (Night / Day), Rosé Pine
  (Main / Dawn), and Nord

## Install

**Go install:**

```sh
go install github.com/mathieucroset/notty/cmd/notty@latest
```

**GitHub Releases:** download a prebuilt binary for Linux, macOS, or
Windows (amd64/arm64) from the
[releases page](https://github.com/mathieucroset/notty/releases).

**Homebrew tap:**

```sh
brew install mathieucroset/tap/notty
```

**Requirements:**
- `git` — required for sync and per-note history. Without it, notes, trash,
  and restore still work, but sync and history are disabled.
- `gh` (optional) — lets the first-run wizard create a private GitHub repo
  for you.
- `wl-paste` (Wayland) or `xclip` (X11) (optional, Linux only) — lets you
  paste images from the clipboard into a note.

## Quick start

Run:

```sh
notty
```

On first run (no config file yet, or the configured vault isn't a git
repository), a setup wizard walks you through three steps:

1. **Vault folder** — where your notes live (default `~/Notes`).
2. **Sync** — create a private GitHub repo (needs `gh`), point Notty at an
   existing repo URL, or stay local-only for now (you can set up sync later
   from the command palette).
3. **Theme** — pick one, with a live preview.

From the main screen:
- `n` in the sidebar creates a new note; `N` creates a new folder.
- Type to edit. Notty autosaves after a second of idle typing, or press
  `ctrl+s` to save immediately.
- `ctrl+p` opens the fuzzy finder, `ctrl+f` full-text search, `ctrl+g`
  cycles between editor / split / preview, and `ctrl+k` opens the command
  palette.
- `F1` opens the help overlay (the same reference as [docs/keys.md](docs/keys.md)).
- `ctrl+q` quits — Notty flushes and commits your work first.

## How sync works

Notty keeps a private GitHub repository in sync with your notes, without
you thinking about git:

- A few seconds after you save (or after an external change is detected),
  Notty commits automatically.
- It fetches from the remote every few minutes and at startup, and pushes
  in the background whenever your local history is ahead.
- If the remote has changes you don't, it merges them in behind the scenes.
  Genuine conflicts (the same note edited differently on two machines) open
  an in-app **resolver** — a three-way diff view where you pick yours,
  theirs, both, or edit the result by hand.
- No network, or no remote configured yet? Notty just works locally
  (`○ local only` / `⊘ offline` in the status bar) and catches up next time
  it can reach GitHub.

For scripts or cron jobs, `notty sync` runs one sync cycle headlessly and
exits non-zero on conflict or error, without opening the TUI.

## Keybindings

The essentials:

| Key | Action |
|---|---|
| `ctrl+p` | Fuzzy finder |
| `ctrl+f` | Full-text search |
| `ctrl+k` | Command palette |
| `ctrl+g` | Cycle editor / split / preview |
| `ctrl+s` | Save |
| `ctrl+e` | Open note in `$EDITOR` |
| `ctrl+b` | Toggle sidebar |
| `n` / `N` | New note / new folder (sidebar) |
| `space` | Toggle a task |
| `F1` | Help |
| `ctrl+q` | Quit |

The vim editor supports a full set of motions, operators, text objects, and
registers. For the complete, per-context reference (sidebar, editor in
every mode, preview, Tasks, Trash, resolver, history, image viewer, wizard,
overlays), see **[docs/keys.md](docs/keys.md)**.

## Configuration

Notty's local config file is optional — every setting has a default:

| OS | Path |
|---|---|
| Linux / macOS | `~/.config/notty/config.toml` (or `$XDG_CONFIG_HOME/notty/config.toml`) |
| Windows | `%APPDATA%\notty\config.toml` |

Settings in the vault's own `.notty/settings.toml` are applied first, so
they follow you between machines; the local config file overrides them.
Here's the full set of keys, with their defaults:

```toml
vault = "~/Notes"            # where your notes live
theme = "catppuccin-mocha"   # see the theme list above; also settable live from the command palette
vim = true                   # vim-style modal editing; false for a plain typing mode
line_numbers = false         # show line numbers in the editor (relative, when on)
editor = ""                  # command for ctrl+e; falls back to $VISUAL, then $EDITOR, then nano (notepad on Windows)
autosave_ms = 1000           # idle delay before autosave, in milliseconds
icons = "unicode"            # nerd | unicode | ascii — see below

[sync]
enabled = true                # false behaves like local-only: commits still happen, but no fetch or push
commit_delay_s = 5            # idle delay after a save before Notty commits
fetch_interval_m = 5          # how often Notty fetches from the remote in the background

[trash]
retention_days = 30           # trashed items older than this are purged at startup

[tasks]
show_done = false             # show completed tasks in the Tasks view
due_soon_days = 7             # a task is "due soon" within this many days

[images]
protocol = "auto"             # kitty | sixel | iterm | halfblocks | off — "auto" detects the terminal
max_import_mb = 5             # images larger than this ask for confirmation before importing
```

`icons` picks the glyphs Notty draws for folders, notes, pins, task
checkboxes, sync states, toasts and the like:

| Value | Looks like | Use it when |
|---|---|---|
| `unicode` (default) | `▸ ▾ • ★ ☐ ☑ ✓ ↻ ⚠` | any modern terminal font |
| `nerd` | Nerd Font icons (folders, files, a pin, …) | your terminal font is a [Nerd Font](https://www.nerdfonts.com); without one they show as empty boxes |
| `ascii` | `> v - ^ [ ] [x] + ~ !` | a font or console with little Unicode |

Only icons change: pane borders and separators are box-drawing characters
in every set. Any other value is an error at startup.

## CLI

```
notty                          Open the TUI on the configured vault
notty new "<title>" [--folder Work]
                                Create a note (optionally inside a folder) and open it
notty sync                      Run one headless sync cycle and exit; nonzero on conflict or error
notty --vault <path>            Override the configured vault path for this run
notty --version                 Print the version and exit
```

## Vault layout and data safety

Your notes are plain `.md` files in an ordinary folder (the *vault*,
`~/Notes` by default), which Notty keeps as a git repository:

```
~/Notes/
├── Work/
│   └── Standup notes.md
├── attachments/        images referenced by your notes
├── .trash/             trashed notes and folders, restorable
└── .notty/
    ├── state.json      pins (synced)
    └── settings.toml   synced settings (optional)
```

Nothing here is proprietary — you can read, edit, `grep`, or back up your
vault with any other tool at any time. A few things Notty does to keep your
data safe:

- **Atomic saves:** every write goes to a temp file, gets fsynced, then is
  renamed into place, so a crash never leaves a half-written note.
- **Trash, not delete:** deleting a note or folder moves it to `.trash/`
  rather than removing it; nothing is purged until `trash.retention_days`
  has passed (or you empty it yourself).
- **History:** every save is a git commit, so `H` in the sidebar gives you
  the full history of a note, with the option to restore any past version.
- **Crash recovery:** if Notty ever crashes with unsaved edits, it writes
  them to `.notty/recovery/` before exiting and offers to restore them next
  time you start it.

## Development

```sh
go build ./cmd/notty     # build the binary
go test ./...            # run the test suite
golangci-lint run        # lint
```

See the [design spec](docs/superpowers/specs/2026-09-29-notty-design.md)
for the full architecture, and the
[implementation plan](docs/superpowers/plans/2026-09-29-notty.md) for how
it was built.

## License

TBD
