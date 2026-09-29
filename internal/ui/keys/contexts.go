// Package keys defines Notty's key contexts, the binding tables for each
// context (spec §4.4), and the layered routing that decides whether a key
// press is a global action or belongs to the focused context.
//
// Routing layers, highest first:
//  1. An open overlay or a full-screen view gets every key, except ctrl+q
//     and F1, which pass through as global actions. In the resolver's edit
//     sub-context ctrl+s stays in the context (it accepts the edit), while
//     ctrl+q and F1 still pass through: the spec names only ctrl+q there,
//     and F1 follows the general full-screen rule. The wizard lets only
//     ctrl+q through.
//  2. Otherwise the global keys (all control or function keys) win in every
//     pane context, including insert mode.
//  3. Everything else goes to the focused context.
package keys

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Context identifies who receives keys that are not global actions.
type Context int

// Key contexts.
const (
	Global Context = iota
	Sidebar
	EditorNormal
	EditorInsert
	EditorVisual
	EditorCommand
	EditorPlain
	EditorReadOnly
	Preview
	TasksView
	TrashView
	Resolver
	ResolverEdit
	History
	ImageViewer
	Wizard
	WizardInput
	Overlay
)

var contextNames = map[Context]string{
	Global:         "Global",
	Sidebar:        "Sidebar",
	EditorNormal:   "Editor · normal",
	EditorInsert:   "Editor · insert",
	EditorVisual:   "Editor · visual",
	EditorCommand:  "Editor · command",
	EditorPlain:    "Editor · vim off",
	EditorReadOnly: "Editor · read-only",
	Preview:        "Preview",
	TasksView:      "Tasks view",
	TrashView:      "Trash view",
	Resolver:       "Resolver",
	ResolverEdit:   "Resolver · edit",
	History:        "History",
	ImageViewer:    "Image viewer",
	Wizard:         "Wizard",
	WizardInput:    "Wizard · text input",
	Overlay:        "Overlays",
}

// String returns the context's display name, used as a help-overlay heading.
func (c Context) String() string {
	if n, ok := contextNames[c]; ok {
		return n
	}
	return "Unknown"
}

// Contexts lists every context in help-overlay order.
func Contexts() []Context {
	return []Context{
		Global, Sidebar, EditorNormal, EditorInsert, EditorVisual, EditorCommand,
		EditorPlain, EditorReadOnly, Preview, TasksView, TrashView, Resolver,
		ResolverEdit, History, ImageViewer, Wizard, WizardInput, Overlay,
	}
}

// Action is a global action. None means the key belongs to the context.
type Action int

// Global actions.
const (
	None Action = iota
	Finder
	Search
	Palette
	ToggleSidebar
	CycleView
	Save
	ExternalEditor
	Quit
	Help
)

var actionNames = map[Action]string{
	None:           "None",
	Finder:         "Finder",
	Search:         "Search",
	Palette:        "Palette",
	ToggleSidebar:  "ToggleSidebar",
	CycleView:      "CycleView",
	Save:           "Save",
	ExternalEditor: "ExternalEditor",
	Quit:           "Quit",
	Help:           "Help",
}

// String returns the action's name.
func (a Action) String() string {
	if n, ok := actionNames[a]; ok {
		return n
	}
	return "Unknown"
}

func bind(keys []string, help, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
}

// GlobalKeys holds the global bindings (spec §4.4, "Global").
var GlobalKeys = map[Action]key.Binding{
	Finder:         bind([]string{"ctrl+p"}, "ctrl+p", "fuzzy finder"),
	Search:         bind([]string{"ctrl+f"}, "ctrl+f", "full-text search"),
	Palette:        bind([]string{"ctrl+k"}, "ctrl+k", "command palette"),
	ToggleSidebar:  bind([]string{"ctrl+b"}, "ctrl+b", "toggle sidebar (zen layout)"),
	CycleView:      bind([]string{"ctrl+g"}, "ctrl+g", "cycle editor / split / preview"),
	Save:           bind([]string{"ctrl+s"}, "ctrl+s", "save"),
	ExternalEditor: bind([]string{"ctrl+e"}, "ctrl+e", "open note in $EDITOR"),
	Quit:           bind([]string{"ctrl+q"}, "ctrl+q", "quit"),
	Help:           bind([]string{"f1"}, "F1", "help"),
}

// globalOrder is the display and matching order of GlobalKeys.
var globalOrder = []Action{Finder, Search, Palette, ToggleSidebar, CycleView, Save, ExternalEditor, Quit, Help}

// fullScreen reports whether ctx is an overlay or a full-screen view, which
// get keys before the global layer.
func fullScreen(ctx Context) bool {
	switch ctx {
	case Resolver, ResolverEdit, History, ImageViewer, Wizard, WizardInput, Overlay:
		return true
	}
	return false
}

// matchGlobal returns the global action bound to k, if any.
func matchGlobal(k tea.KeyPressMsg) (Action, bool) {
	for _, a := range globalOrder {
		if key.Matches(k, GlobalKeys[a]) {
			return a, true
		}
	}
	return None, false
}

// Route decides where a key press goes. It returns (action, true) when the
// key is a global action, and (None, false) when it belongs to ctx.
// overlayOpen reports whether an overlay is open on top of ctx.
func Route(ctx Context, overlayOpen bool, k tea.KeyPressMsg) (Action, bool) {
	a, ok := matchGlobal(k)
	if !ok {
		return None, false
	}
	switch {
	case ctx == Wizard || ctx == WizardInput:
		// The wizard lets only ctrl+q through, even with an overlay open.
		if a == Quit {
			return Quit, true
		}
		return None, false
	case overlayOpen || fullScreen(ctx):
		// Only ctrl+q and F1 pass through; ctrl+s in resolver edit, like
		// every other key, stays in the context.
		if a == Quit || a == Help {
			return a, true
		}
		return None, false
	default:
		return a, true
	}
}

// Bindings returns the binding table for ctx (spec §4.4). The tables feed
// the help overlay, so every entry carries a key label and a description.
// Multi-key sequences (such as "gg" or "]t") appear as a single key label.
func Bindings(ctx Context) []key.Binding {
	if ctx == Global {
		out := make([]key.Binding, 0, len(globalOrder))
		for _, a := range globalOrder {
			out = append(out, GlobalKeys[a])
		}
		return out
	}
	return contextBindings[ctx]
}

var contextBindings = map[Context][]key.Binding{
	Sidebar: {
		bind([]string{"j", "down"}, "j/↓", "move down"),
		bind([]string{"k", "up"}, "k/↑", "move up"),
		bind([]string{"h", "left"}, "h/←", "collapse folder / go to parent"),
		bind([]string{"l", "right"}, "l/→", "expand folder"),
		bind([]string{"enter"}, "enter", "open note, toggle folder, open entry"),
		bind([]string{"tab"}, "tab", "focus main pane"),
		bind([]string{"n"}, "n", "new note"),
		bind([]string{"N"}, "N", "new folder"),
		bind([]string{"r"}, "r", "rename"),
		bind([]string{"m"}, "m", "move"),
		bind([]string{"d"}, "d", "delete (move to trash)"),
		bind([]string{"p"}, "p", "pin / unpin"),
		bind([]string{"H"}, "H", "note history"),
		bind([]string{"#"}, "#", "filter by tag"),
		bind([]string{"esc"}, "esc", "clear tag filter"),
		bind([]string{"c"}, "c", "open resolver"),
		bind([]string{"!"}, "!", "error log"),
		bind([]string{"?"}, "?", "help"),
		bind([]string{"q"}, "q", "quit"),
	},
	EditorNormal: {
		bind([]string{"h", "j", "k", "l"}, "h j k l", "move"),
		bind([]string{"w", "b", "e", "W", "B", "E"}, "w b e W B E", "word motions"),
		bind([]string{"0", "^", "$"}, "0 ^ $", "line start / first char / end"),
		bind([]string{"gg", "G"}, "gg G", "top / bottom"),
		bind([]string{"f", "F", "t", "T", ";", ","}, "f F t T ; ,", "find character"),
		bind([]string{"{", "}", "%"}, "{ } %", "paragraph / matching bracket"),
		bind([]string{"gj", "gk"}, "gj gk", "move by screen row"),
		bind([]string{"d", "c", "y"}, "d c y", "delete / change / yank (with motion)"),
		bind([]string{"dd", "cc", "yy"}, "dd cc yy", "line operators"),
		bind([]string{"x", "X", "s", "S", "r", "J"}, "x X s S r J", "edit characters / join"),
		bind([]string{">>", "<<"}, ">> <<", "indent / outdent"),
		bind([]string{"i", "a", "I", "A", "o", "O"}, "i a I A o O", "enter insert mode"),
		bind([]string{"v", "V"}, "v V", "visual / visual-line mode"),
		bind([]string{":"}, ":", "command mode"),
		bind([]string{"."}, ".", "repeat"),
		bind([]string{"p", "P"}, "p P", "paste after / before"),
		bind([]string{"u", "ctrl+r"}, "u ctrl+r", "undo / redo"),
		bind([]string{"/", "?", "n", "N"}, "/ ? n N", "search forward / backward / next / previous"),
		bind([]string{"\"+"}, "\"+", "system clipboard register"),
		bind([]string{"space"}, "space", "toggle task"),
		bind([]string{"tab"}, "tab", "focus sidebar"),
		bind([]string{"ctrl+w h", "ctrl+w l"}, "ctrl+w h/l", "focus sidebar / main pane"),
	},
	EditorInsert: {
		bind([]string{"tab"}, "tab", "indent"),
		bind([]string{"shift+tab"}, "shift+tab", "outdent"),
		bind([]string{"ctrl+t"}, "ctrl+t", "toggle task"),
		bind([]string{"ctrl+v"}, "ctrl+v", "paste image or text"),
		bind([]string{"enter"}, "enter", "new line (continues lists)"),
		bind([]string{"esc"}, "esc", "normal mode"),
	},
	EditorVisual: {
		bind([]string{"h", "j", "k", "l"}, "motions", "extend selection"),
		bind([]string{"iw", "aw", "ip", "ap"}, "iw aw i\" a\" i( a( ip ap", "select text object"),
		bind([]string{"d", "x"}, "d x", "delete selection"),
		bind([]string{"c"}, "c", "change selection"),
		bind([]string{"y"}, "y", "yank selection"),
		bind([]string{">", "<"}, "> <", "indent / outdent"),
		bind([]string{"esc"}, "esc", "normal mode"),
	},
	EditorCommand: {
		bind([]string{":w"}, ":w", "save"),
		bind([]string{":q"}, ":q", "quit Notty"),
		bind([]string{":wq"}, ":wq", "save and quit"),
		bind([]string{":e"}, ":e <note>", "open a note"),
		bind([]string{":img"}, ":img <path>", "insert an image"),
		bind([]string{":help"}, ":help", "help"),
		bind([]string{"enter"}, "enter", "run command"),
		bind([]string{"esc"}, "esc", "cancel"),
	},
	EditorPlain: {
		bind([]string{"tab"}, "tab", "indent"),
		bind([]string{"shift+tab"}, "shift+tab", "outdent"),
		bind([]string{"ctrl+t"}, "ctrl+t", "toggle task"),
		bind([]string{"ctrl+v"}, "ctrl+v", "paste image or text"),
		bind([]string{"ctrl+c"}, "ctrl+c", "copy"),
		bind([]string{"ctrl+x"}, "ctrl+x", "cut"),
		bind([]string{"ctrl+z"}, "ctrl+z", "undo"),
		bind([]string{"ctrl+y"}, "ctrl+y", "redo"),
		bind([]string{"shift+left", "shift+right", "shift+up", "shift+down"}, "shift+arrows", "select text"),
		bind([]string{"esc"}, "esc", "focus sidebar"),
	},
	EditorReadOnly: {
		bind([]string{"h", "j", "k", "l"}, "motions", "move and scroll"),
		bind([]string{"/"}, "/", "search"),
		bind([]string{"y"}, "y", "yank / copy"),
		bind([]string{"tab"}, "tab", "focus sidebar"),
		bind([]string{"c"}, "c", "open resolver on this file"),
	},
	Preview: {
		bind([]string{"j", "k"}, "j/k", "scroll"),
		bind([]string{"ctrl+d", "ctrl+u"}, "ctrl+d/u", "half-page down / up"),
		bind([]string{"gg", "G"}, "gg G", "top / bottom"),
		bind([]string{"]t", "[t"}, "]t [t", "next / previous task"),
		bind([]string{"space"}, "space", "toggle highlighted task"),
		bind([]string{"]i", "[i"}, "]i [i", "next / previous image"),
		bind([]string{"enter"}, "enter", "open image viewer"),
		bind([]string{"tab"}, "tab", "focus sidebar"),
		bind([]string{"?"}, "?", "help"),
	},
	TasksView: {
		bind([]string{"j", "k"}, "j/k", "move"),
		bind([]string{"space"}, "space", "toggle task"),
		bind([]string{"enter"}, "enter", "open note at task"),
		bind([]string{"tab"}, "tab", "focus sidebar"),
		bind([]string{"esc"}, "esc", "back to note"),
	},
	TrashView: {
		bind([]string{"j", "k"}, "j/k", "move"),
		bind([]string{"enter"}, "enter", "restore"),
		bind([]string{"D"}, "D", "delete permanently"),
		bind([]string{"E"}, "E", "empty trash"),
		bind([]string{"tab"}, "tab", "focus sidebar"),
		bind([]string{"esc"}, "esc", "back to note"),
	},
	Resolver: {
		bind([]string{"j", "k"}, "j/k", "move through files"),
		bind([]string{"]c", "[c"}, "]c [c", "next / previous conflict"),
		bind([]string{"o"}, "o", "keep yours"),
		bind([]string{"t"}, "t", "keep theirs"),
		bind([]string{"b"}, "b", "keep both"),
		bind([]string{"e"}, "e", "edit result"),
		bind([]string{"1", "2", "3"}, "1 2 3", "choose option"),
		bind([]string{"enter"}, "enter", "mark resolved / confirm"),
		bind([]string{"esc", "q"}, "esc/q", "close (conflicts stay unresolved)"),
	},
	ResolverEdit: {
		bind([]string{"ctrl+s"}, "ctrl+s", "accept edit"),
		bind([]string{"esc"}, "esc", "back to navigation (keeps edits)"),
	},
	History: {
		bind([]string{"j", "k"}, "j/k", "move"),
		bind([]string{"tab"}, "tab", "rendered / diff"),
		bind([]string{"enter"}, "enter", "restore this version"),
		bind([]string{"esc", "q"}, "esc/q", "close"),
	},
	ImageViewer: {
		bind([]string{"n", "p"}, "n/p", "next / previous image"),
		bind([]string{"o"}, "o", "open in system viewer"),
		bind([]string{"esc", "q"}, "esc/q", "close"),
	},
	Wizard: {
		bind([]string{"enter"}, "enter", "confirm step"),
		bind([]string{"esc"}, "esc", "back one step"),
		bind([]string{"ctrl+q"}, "ctrl+q", "quit"),
	},
	WizardInput: {
		bind([]string{"enter"}, "enter", "confirm"),
		bind([]string{"esc"}, "esc", "back one step"),
		bind([]string{"ctrl+q"}, "ctrl+q", "quit"),
	},
	Overlay: {
		bind([]string{"ctrl+j", "down"}, "ctrl+j/↓", "move down"),
		bind([]string{"ctrl+k", "up"}, "ctrl+k/↑", "move up"),
		bind([]string{"enter"}, "enter", "choose"),
		bind([]string{"esc"}, "esc", "close"),
	},
}
