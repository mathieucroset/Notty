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
	// current theme, a finished setup step); Pending marks a step still
	// to run.
	Check, Pending string

	// Image precedes an image's name in its chip.
	Image string

	// Conflict kinds in the resolver's file list.
	KindText, KindBinary, KindModifyDelete, KindPath string
}

var nerd = Set{
	Name:             "nerd",
	Logo:             "\U000F082E", // nf-md-notebook
	FolderOpen:       "\uf07c",          // nf-fa-folder_open
	FolderClosed:     "\uf07b",          // nf-fa-folder
	Note:             "\uf0f6",          // nf-fa-file_text_o
	File:             "\uf016",          // nf-fa-file_o
	Pin:              "\uf435",          // nf-oct-pin
	Tasks:            "\uf0ae",          // nf-fa-tasks
	Conflicts:        "\uf071",          // nf-fa-exclamation_triangle
	Trash:            "\uf014",          // nf-fa-trash_o
	Dirty:            "\uf111",          // nf-fa-circle
	TaskOpen:         "\uf096",          // nf-fa-square_o
	TaskDone:         "\uf046",          // nf-fa-check_square_o
	ProgressFull:     "▰",
	ProgressEmpty:    "▱",
	Synced:           "\uf00c",          // nf-fa-check
	Syncing:          "\uf021",          // nf-fa-refresh
	Offline:          "\U000F0164", // nf-md-cloud_off_outline
	SyncConflict:     "\uf071",          // nf-fa-exclamation_triangle
	SyncError:        "\uf057",          // nf-fa-times_circle
	LocalOnly:        "\uf0a0",          // nf-fa-hdd_o
	Info:             "\uf05a",          // nf-fa-info_circle
	Warn:             "\uf071",          // nf-fa-exclamation_triangle
	Error:            "\uf057",          // nf-fa-times_circle
	Check:            "\uf00c",          // nf-fa-check
	Pending:          "\uf10c",          // nf-fa-circle_o
	Image:            "\uf03e",          // nf-fa-picture_o
	KindText:         "\uf0f6",          // nf-fa-file_text_o
	KindBinary:       "\uf471",          // nf-oct-file_binary
	KindModifyDelete: "\uf440",          // nf-oct-diff
	KindPath:         "\uf443",          // nf-oct-arrow_switch
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
	Pending:          "○",
	Image:            "🖼\ufe0f", // emoji presentation: two columns everywhere
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
	ProgressFull:     "=",
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
	Pending:          "o",
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

// Spinner returns the frames of the set's busy spinner: braille dots, or
// a turning bar for ASCII.
func (s Set) Spinner() []string {
	if s.Name == ascii.Name {
		return []string{"|", "/", "-", "\\"}
	}
	return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
}

// Get returns the named set. An unknown name returns Default and false.
func Get(name string) (Set, bool) {
	for _, s := range sets {
		if s.Name == name {
			return s, true
		}
	}
	return Default(), false
}
