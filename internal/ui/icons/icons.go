// Package icons holds the glyph sets Notty's UI draws with (the icons
// config key): Nerd Font icons, plain Unicode symbols (the default), and
// ASCII. Every component takes its glyphs from the Set in theme.Styles
// rather than hard-coding them, so switching sets restyles the whole UI.
//
// Only icons change between sets. Pane borders, separators and the
// ellipsis stay box-drawing and punctuation characters, which every
// terminal font has.
package icons

// Set is one glyph set. Nerd and Unicode glyphs are one column wide
// (except the Unicode image emoji), so rows stay aligned; ASCII glyphs
// are at most a short bracketed word.
type Set struct {
	Name string

	// Logo precedes the app name in the sidebar title.
	Logo string

	// Sidebar tree and entries.
	FolderOpen, FolderClosed string
	Note, File, Pin          string
	Tasks, Conflicts, Trash  string
	// Dirty marks unsaved changes and unresolved conflicts.
	Dirty string

	// Task checkboxes, in the editor, the preview, and the Tasks view.
	TaskOpen, TaskDone string
	// Heading task progress bar cells.
	ProgressFull, ProgressEmpty string

	// Sync states in the status bar (spec §4.3).
	Synced, Syncing, Offline, SyncConflict, SyncError, LocalOnly string

	// Toast and error log levels.
	Info, Warn, Error string

	// Check marks something done or current (a resolved file, the
	// current theme).
	Check string

	// Image precedes an image's name in its chip.
	Image string

	// Conflict kinds in the resolver's file list.
	KindText, KindBinary, KindModifyDelete, KindPath string
}

var nerd = Set{
	Name:             "nerd",
	Logo:             "\U000F082E", // nf-md-notebook
	FolderOpen:       "",          // nf-fa-folder_open
	FolderClosed:     "",          // nf-fa-folder
	Note:             "",          // nf-fa-file_text_o
	File:             "",          // nf-fa-file_o
	Pin:              "",          // nf-oct-pin
	Tasks:            "",          // nf-fa-tasks
	Conflicts:        "",          // nf-fa-exclamation_triangle
	Trash:            "",          // nf-fa-trash_o
	Dirty:            "",          // nf-fa-circle
	TaskOpen:         "",          // nf-fa-square_o
	TaskDone:         "",          // nf-fa-check_square_o
	ProgressFull:     "▰",
	ProgressEmpty:    "▱",
	Synced:           "",          // nf-fa-check
	Syncing:          "",          // nf-fa-refresh
	Offline:          "\U000F0164", // nf-md-cloud_off_outline
	SyncConflict:     "",          // nf-fa-exclamation_triangle
	SyncError:        "",          // nf-fa-times_circle
	LocalOnly:        "",          // nf-fa-hdd_o
	Info:             "",          // nf-fa-info_circle
	Warn:             "",          // nf-fa-exclamation_triangle
	Error:            "",          // nf-fa-times_circle
	Check:            "",          // nf-fa-check
	Image:            "",          // nf-fa-picture_o
	KindText:         "",          // nf-fa-file_text_o
	KindBinary:       "",          // nf-oct-file_binary
	KindModifyDelete: "",          // nf-oct-diff
	KindPath:         "",          // nf-oct-arrow_switch
}

var unicode = Set{
	Name:             "unicode",
	Logo:             "◆",
	FolderOpen:       "▾",
	FolderClosed:     "▸",
	Note:             "•",
	File:             "·",
	Pin:              "★",
	Tasks:            "☐",
	Conflicts:        "⚠",
	Trash:            "⌫",
	Dirty:            "●",
	TaskOpen:         "☐",
	TaskDone:         "☑",
	ProgressFull:     "▰",
	ProgressEmpty:    "▱",
	Synced:           "✓",
	Syncing:          "↻",
	Offline:          "⊘",
	SyncConflict:     "⚠",
	SyncError:        "✗",
	LocalOnly:        "○",
	Info:             "ℹ",
	Warn:             "⚠",
	Error:            "✗",
	Check:            "✓",
	Image:            "🖼️", // emoji presentation: two columns everywhere
	KindText:         "≡",
	KindBinary:       "◆",
	KindModifyDelete: "±",
	KindPath:         "⇄",
}

var ascii = Set{
	Name:             "ascii",
	Logo:             "*",
	FolderOpen:       "v",
	FolderClosed:     ">",
	Note:             "-",
	File:             ".",
	Pin:              "^",
	Tasks:            "[ ]",
	Conflicts:        "!",
	Trash:            "x",
	Dirty:            "*",
	TaskOpen:         "[ ]",
	TaskDone:         "[x]",
	ProgressFull:     "#",
	ProgressEmpty:    "-",
	Synced:           "+",
	Syncing:          "~",
	Offline:          "-",
	SyncConflict:     "!",
	SyncError:        "x",
	LocalOnly:        "o",
	Info:             "i",
	Warn:             "!",
	Error:            "x",
	Check:            "+",
	Image:            "[img]",
	KindText:         "=",
	KindBinary:       "b",
	KindModifyDelete: "-",
	KindPath:         "<>",
}

// sets lists every set in the order Names returns them.
var sets = []Set{nerd, unicode, ascii}

// Names returns the set names: nerd, unicode, ascii.
func Names() []string {
	out := make([]string, len(sets))
	for i, s := range sets {
		out[i] = s.Name
	}
	return out
}

// Default is the set used when none is configured: unicode, which needs
// no special font.
func Default() Set { return unicode }

// Get returns the named set. An unknown name returns Default and false.
func Get(name string) (Set, bool) {
	for _, s := range sets {
		if s.Name == name {
			return s, true
		}
	}
	return Default(), false
}
