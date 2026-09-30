# Keybinding reference

This is the full keybinding table, grouped by context, generated from the
binding tables in `internal/ui/keys` (the same tables that drive Notty's own
in-app help, `F1`). See the [design spec, §4.4](superpowers/specs/2026-09-29-notty-design.md#44-keybindings-by-context)
for the routing rules between contexts.

A few notes on how to read it:

- **Global** keys work in every context, including while typing in the
  editor — they're all control or function keys, so they never collide with
  normal typing.
- When an overlay (finder, search, palette, help, a dialog) or a full-screen
  view (resolver, history, image viewer, wizard) is open, only `ctrl+q` and
  `F1` pass through as global actions; everything else goes to that view.
- "Editor · normal" / "Editor · insert" / "Editor · visual" apply when vim
  mode is on (the default). "Editor · vim off" is the full non-vim keymap.
- "Editor · read-only" applies to a conflicted note, whether vim is on or
  off.
- `alt+m` (move the cursor's line, or every line the selection touches, to
  the end of another note) and `alt+M` (`alt+shift+m`: move the open note to
  a folder) work between commands: not in insert mode, nor after an
  operator or a count. In a conflicted note they say why they can't. The
  command palette has both too ("Move line to note…", "Move note to
  folder…").
- The move dialog (sidebar `m`, or `alt+M`) also takes a folder that does
  not exist yet: Notty asks before creating it.

## Global

| Key | Action |
|---|---|
| `ctrl+p` | fuzzy finder |
| `ctrl+f` | full-text search |
| `ctrl+k` | command palette |
| `ctrl+b` | toggle sidebar (zen layout) |
| `ctrl+g` | cycle editor / split / preview |
| `ctrl+s` | save |
| `ctrl+e` | open note in $EDITOR |
| `ctrl+q` | quit |
| `F1` | help |

## Sidebar

| Key | Action |
|---|---|
| `j/↓` | move down |
| `k/↑` | move up |
| `h/←` | collapse folder / go to parent |
| `l/→` | expand folder |
| `enter` | open note, toggle folder, open entry |
| `tab` | focus main pane |
| `n` | new note |
| `N` | new folder |
| `r` | rename |
| `m` | move |
| `d` | delete (move to trash) |
| `p` | pin / unpin |
| `H` | note history |
| `#` | filter by tag |
| `esc` | clear tag filter |
| `c` | open resolver |
| `!` | error log |
| `?` | help |
| `q` | quit |

## Editor · normal

| Key | Action |
|---|---|
| `h j k l` | move |
| `w b e W B E` | word motions |
| `0 ^ $` | line start / first char / end |
| `gg G` | top / bottom |
| `f F t T ; ,` | find character |
| `{ } %` | paragraph / matching bracket |
| `gj gk` | move by screen row |
| `d c y` | delete / change / yank (with motion) |
| `dd cc yy` | line operators |
| `x X s S r J` | edit characters / join |
| `>> <<` | indent / outdent |
| `i a I A o O` | enter insert mode |
| `v V` | visual / visual-line mode |
| `:` | command mode |
| `.` | repeat |
| `p P` | paste after / before |
| `u ctrl+r` | undo / redo |
| `/ ? n N` | search forward / backward / next / previous |
| `"+` | system clipboard register |
| `space` | toggle task |
| `alt+m` | move lines to note |
| `alt+M` | move note to folder |
| `tab` | focus sidebar |
| `ctrl+w h/l` | focus sidebar / main pane |

## Editor · insert

| Key | Action |
|---|---|
| `tab` | indent |
| `shift+tab` | outdent |
| `ctrl+t` | toggle task |
| `ctrl+v` | paste image or text |
| `enter` | new line (continues lists) |
| `esc` | normal mode |

## Editor · visual

| Key | Action |
|---|---|
| `motions` | extend selection |
| `iw aw i" a" i( a( ip ap` | select text object |
| `d x` | delete selection |
| `c` | change selection |
| `y` | yank selection |
| `> <` | indent / outdent |
| `alt+m` | move lines to note |
| `alt+M` | move note to folder |
| `esc` | normal mode |

## Editor · command

| Key | Action |
|---|---|
| `:w` | save |
| `:q` | quit Notty |
| `:wq` | save and quit |
| `:e <note>` | open a note |
| `:img <path>` | insert an image |
| `:help` | help |
| `enter` | run command |
| `esc` | cancel |

## Editor · vim off

| Key | Action |
|---|---|
| `tab` | indent |
| `shift+tab` | outdent |
| `ctrl+t` | toggle task |
| `ctrl+v` | paste image or text |
| `ctrl+c` | copy |
| `ctrl+x` | cut |
| `ctrl+z` | undo |
| `ctrl+y` | redo |
| `shift+arrows` | select text |
| `alt+m` | move lines to note |
| `alt+M` | move note to folder |
| `esc` | focus sidebar |

## Editor · read-only

| Key | Action |
|---|---|
| `motions` | move and scroll |
| `/` | search |
| `y` | yank / copy |
| `tab` | focus sidebar |
| `c` | open resolver on this file |

## Preview

| Key | Action |
|---|---|
| `j/k` | scroll |
| `ctrl+d/u` | half-page down / up |
| `gg G` | top / bottom |
| `]t [t` | next / previous task |
| `space` | toggle highlighted task |
| `]i [i` | next / previous image |
| `enter` | open image viewer |
| `tab` | focus sidebar |
| `?` | help |

## Tasks view

| Key | Action |
|---|---|
| `j/k` | move |
| `space` | toggle task |
| `enter` | open note at task |
| `tab` | focus sidebar |
| `esc` | back to note |

## Trash view

| Key | Action |
|---|---|
| `j/k` | move |
| `enter` | restore |
| `D` | delete permanently |
| `E` | empty trash |
| `tab` | focus sidebar |
| `esc` | back to note |

## Resolver

| Key | Action |
|---|---|
| `j/k` | move through files |
| `]c [c` | next / previous conflict |
| `o` | keep yours |
| `t` | keep theirs |
| `b` | keep both |
| `e` | edit result |
| `1 2 3` | choose option |
| `enter` | mark resolved / confirm |
| `esc/q` | close (conflicts stay unresolved) |

## Resolver · edit

| Key | Action |
|---|---|
| `ctrl+s` | accept edit |
| `esc` | back to navigation (keeps edits) |

## History

| Key | Action |
|---|---|
| `j/k` | move |
| `tab` | rendered / diff |
| `enter` | restore this version |
| `esc/q` | close |

## Image viewer

| Key | Action |
|---|---|
| `n/p` | next / previous image |
| `o` | open in system viewer |
| `esc/q` | close |

## Wizard

| Key | Action |
|---|---|
| `enter` | confirm step |
| `esc` | back one step |
| `ctrl+q` | quit |

## Wizard · text input

| Key | Action |
|---|---|
| `enter` | confirm |
| `esc` | back one step |
| `ctrl+q` | quit |

## Overlays

Applies to the fuzzy finder, full-text search, and command palette.

| Key | Action |
|---|---|
| `ctrl+j/↓` | move down |
| `ctrl+k/↑` | move up |
| `enter` | choose |
| `esc` | close |
