# Notty

A terminal markdown notes app with automatic GitHub sync, built in Go with
Bubble Tea.

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
  (Main / Dawn), and Nord — plus your own theme files, live-reloaded, and
  [matugen](https://github.com/InioX/matugen) support so Notty follows your
  wallpaper

## Install

**Go install:**

```sh
go install github.com/mathieucroset/notty/cmd/notty@latest
```

**From source:** clone the repo, then `make install` builds a static
binary and installs it to `~/.local/bin/notty` (set `PREFIX` to install
elsewhere, e.g. `make install PREFIX=/usr/local`).

**GitHub Releases:** download a prebuilt binary for Linux, macOS, or
Windows (amd64/arm64) from the
[releases page](https://github.com/mathieucroset/notty/releases).

**Homebrew tap:**

```sh
brew install mathieucroset/tap/notty
```

**Updating:** Notty tells you when a newer release is out (see
`update_check` under [Configuration](#configuration)). To upgrade:

- Go install: run the same `go install …@latest` command again.
- From source: pull the latest changes, then `make install` again. (A
  build made exactly at a release tag checks for updates, but its toast
  shows the generic "download" hint; pulling and running `make install`
  again is still the way to update.)
- GitHub Releases: download the new binary and replace the old one.
- Homebrew: `brew upgrade notty`.

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
The one exception is `editor`: it names a command Notty runs, and the
vault's settings travel through git, so anyone who can push to a shared
vault could pick it. Notty only reads `editor` from the local config file
and ignores it (with a line in the log) in `.notty/settings.toml`.
Here's the full set of keys, with their defaults:

```toml
vault = "~/Notes"            # where your notes live
theme = "catppuccin-mocha"   # a built-in or your own theme (see Themes below); also settable live from the command palette
vim = true                   # vim-style modal editing; false for a plain typing mode
line_numbers = false         # show line numbers in the editor (relative, when on)
editor = ""                  # command for ctrl+e (local config only); falls back to $VISUAL, then $EDITOR, then nano (notepad on Windows)
                             # it is split into words at blanks; "double quotes" (and 'single quotes', except on Windows) group a word
                             # holding blanks, backslashes are kept as they are, and an unclosed quote is an error:
                             # editor = '"C:\Program Files\Notepad++\notepad++.exe" -multiInst'
autosave_ms = 1000           # idle delay before autosave, in milliseconds
icons = "unicode"            # nerd | unicode | ascii — see below
update_check = true          # tell you when a newer release is out — see below

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

On Windows, an editor that is a batch file, such as VS Code's `code.cmd`,
cannot open notes whose path contains `%`, `!`, `&`, `|`, `<`, `>`, `^`
or `"`. Windows runs batch files through cmd.exe, which reads those
characters as commands, variables or redirections even in a file name, and
there is no safe way to escape all of them. Notty refuses with a message
instead of running such a command line. Rename the note, or set `editor`
to the program the batch file starts (for VS Code,
`'"C:\Users\you\AppData\Local\Programs\Microsoft VS Code\Code.exe" -w'`).

`icons` picks the glyphs Notty draws for folders, notes, pins, task
checkboxes, sync states, toasts and the like:

| Value | Looks like | Use it when |
|---|---|---|
| `unicode` (default) | `▸ ▾ • ★ ☐ ☑ ✓ ↻ ⚠` | any modern terminal font |
| `nerd` | Nerd Font icons (folders, files, a pin, …) | your terminal font is a [Nerd Font](https://www.nerdfonts.com); without one they show as empty boxes |
| `ascii` | `> v - ^ [ ] [x] + ~ !` | a font or console with little Unicode |

Only icons change: pane borders and separators are box-drawing characters
in every set. Any other value is an error at startup.

`update_check` lets Notty tell you when a newer release is out: a one-time
toast saying how to upgrade (for the way you installed it), then a
`↑ v0.2.0` marker in the status bar until you do. To find out, Notty sends
one anonymous HTTPS request to `api.github.com` at most once a day, with a
`notty/<version>` User-Agent and nothing about you or your notes. It never
slows down startup, and a failed check stays silent. Turn it off with
`update_check = false`, or with the environment variable
`NOTTY_NO_UPDATE_CHECK=1`. Development builds and `notty sync` never check.

### Themes

`theme` is one of the built-in themes — `catppuccin-mocha` (the default),
`catppuccin-latte`, `tokyo-night`, `tokyo-night-day`, `rose-pine`,
`rose-pine-dawn`, `nord` — or the name of a theme file of your own. You can
also switch live with **Switch theme** in the command palette (`ctrl+k`),
which previews each theme and saves your choice to the local config.

#### Your own theme

A theme file lives in the `themes` folder next to `config.toml`:
`~/.config/notty/themes/<name>.toml` on Linux and macOS
(`$XDG_CONFIG_HOME/notty/themes/` when that is set),
`%APPDATA%\notty\themes\<name>.toml` on Windows. Its name is the file name
without `.toml`, so `themes/dusk.toml` is `theme = "dusk"`; the theme picker
and the first-run wizard list it after the built-ins. A file named like a
built-in theme (`nord.toml`, say) is ignored: built-ins can't be overridden.
Symlinked theme files work too, which is handy with a dotfile manager.

Colors are `"#RRGGBB"` strings (any case). Unknown keys are an error, so a
typo doesn't go unnoticed.

| Key | Required | Colors | When left out |
|---|---|---|---|
| `base` | yes | the main background | |
| `surface` | yes | raised panel backgrounds | |
| `overlay` | yes | selection and highlight backgrounds | |
| `text` | yes | body text | |
| `subtext` | yes | secondary text | |
| `muted` | yes | dim markup, unfocused borders | |
| `accent` | yes | the main accent | |
| `accent2` | yes | the second accent | |
| `error` | yes | errors | |
| `success` | no | success states | `#a6d189` in a dark theme, `#40a02b` in a light one |
| `warning` | no | warnings | `#e5c890` in a dark theme, `#df8e1d` in a light one |
| `headings` | no | headings H1 to H6 (also code highlighting): a list of up to 6 colors | each missing level alternates `accent`, `accent2`: H1, H3 and H5 get `accent`, H2, H4 and H6 `accent2` (`[accent, accent2][i % 2]` for level `i`, H1 = 0) |
| `dark` | no | `true` for a dark theme, `false` for a light one | `true` when `base` is dark (relative luminance below 0.5) |

A complete example:

<!-- example theme file: loaded by internal/ui/theme/readme_test.go -->
```toml
# ~/.config/notty/themes/dusk.toml — use it with theme = "dusk"
base     = "#141318"   # main background
surface  = "#201f24"   # raised panel background
overlay  = "#36343a"   # selection / highlight background
text     = "#e6e1e9"
subtext  = "#cac4cf"
muted    = "#948f99"   # dim markup, unfocused borders
accent   = "#cfbcff"
accent2  = "#f2b7c2"
error    = "#ffb4ab"

# optional
success  = "#a6d189"
warning  = "#e5c890"
headings = ["#cfbcff", "#f2b7c2", "#cbc2db"]   # H1, H2, H3; H4–H6 fall back
dark     = true
```

**Live reload.** Notty watches the `themes` folder: save the theme file
you're using and the colors change at once, no restart. A file that doesn't
load (half-written, a typo) keeps the current colors and shows a warning
naming the problem; the next good save applies silently. Deleting the file
keeps the current colors, and it's loaded again when it comes back. Changes
made to a symlink's target outside the `themes` folder are picked up the
next time Notty starts.

If the configured theme can't be loaded when Notty starts — for example
`theme = "matugen"` synced through the vault's `.notty/settings.toml` to a
machine without that file — Notty starts in `catppuccin-mocha` with a
warning, and switches to your theme as soon as its file appears.

#### Following your wallpaper with matugen (Noctalia)

[matugen](https://github.com/InioX/matugen) generates Material You colors
from your wallpaper; desktop shells such as Noctalia run it for you. To give
Notty the same colors, run:

```sh
notty theme matugen
```

It writes Notty's matugen template to `~/.config/notty/matugen-template.toml`
(an existing file is never overwritten) and prints the entry to add to your
matugen config. With matugen itself, or Noctalia 4 (its
`user-templates.toml`):

```toml
[templates.notty]
input_path  = "~/.config/notty/matugen-template.toml"
output_path = "~/.config/notty/themes/matugen.toml"
```

Noctalia 5 renders templates with its own engine and reads user templates
from a different table, in the same `user-templates.toml`:

```toml
[theme.templates.user.notty]
input_path  = "~/.config/notty/matugen-template.toml"
output_path = "~/.config/notty/themes/matugen.toml"
```

`noctalia theme --list-templates` should then list `notty` under "User
templates". (The printed paths are the real ones on your machine.) Then set
`theme = "matugen"` in `config.toml`, or pick `matugen` with **Switch theme**
once the file exists. From then on, every time matugen renders the template
— a new wallpaper, a light/dark switch — Notty picks the new colors up by
itself: no `post_hook` or restart needed. The template leaves `dark`,
`success` and `warning` to the defaults above, so it works in both light and
dark schemes.

## CLI

```
notty                          Open the TUI on the configured vault
notty new "<title>" [--folder Work]
                                Create a note (optionally inside a folder) and open it
notty sync                      Run one headless sync cycle and exit; nonzero on conflict or error
notty theme matugen             Write the matugen template and print the matugen config for it
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
    ├── settings.toml   synced settings (optional)
    └── recovery/       unsaved edits saved after a crash (not synced)
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
- **Crash recovery:** if Notty ever crashes with unsaved edits, it restores
  the terminal, writes them to `.notty/recovery/<note>-<timestamp>.md`
  (kept out of git) and says where. The next time you start it, it offers
  each file back: restore it as a new note `<name> (recovered)` next to the
  original, or discard it (`esc` asks again next time).
- **Log:** warnings, errors and crash details go to `notty.log` in
  `$XDG_STATE_HOME/notty/` (`~/.local/state/notty/` by default,
  `%LOCALAPPDATA%\notty\` on Windows), rotated to `notty.log.1` past 5 MB.
  Set `NOTTY_DEBUG=1` for a more detailed log.

## Development

```sh
go build ./cmd/notty     # build the binary
go test ./...            # run the test suite
golangci-lint run        # lint
```

## License

[MIT](LICENSE)
