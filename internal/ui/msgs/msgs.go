// Package msgs holds the message types shared between Notty's UI
// components. It is a leaf package: it imports nothing from internal/ui, so
// every component can emit and receive these messages without import
// cycles. Components emit these types rather than defining their own.
package msgs

// OpenNoteMsg asks the app to open the note at Path (vault-relative). Line
// is the 0-based line to put the cursor on; -1 restores the saved cursor.
type OpenNoteMsg struct {
	Path string
	Line int
}

// ToggleTaskMsg asks the app to toggle the task at Line in the note at
// Path. Text is the line's content, used to find the task again if the line
// number has drifted.
type ToggleTaskMsg struct {
	Path string
	Line int
	Text string
}

// SaveRequestMsg asks the app to save the current buffer.
type SaveRequestMsg struct{}

// FocusSidebarMsg moves keyboard focus to the sidebar.
type FocusSidebarMsg struct{}

// FocusMainMsg moves keyboard focus to the main pane.
type FocusMainMsg struct{}

// QuitMsg asks the app to run its quit sequence.
type QuitMsg struct{}

// OpenImageViewerMsg opens the full-screen image viewer on Paths, starting
// at Index.
type OpenImageViewerMsg struct {
	Paths []string
	Index int
}

// ImportImageMsg asks the app to import an image into the vault, either from
// a file (Path) or from raw bytes (Data, with extension Ext such as "png").
type ImportImageMsg struct {
	Path string
	Data []byte
	Ext  string
}

// ToastLevel is the severity of a toast notification.
type ToastLevel int

// Toast levels. Info and Warn toasts disappear on their own; Error toasts
// stay until dismissed (spec §4.3).
const (
	ToastInfo ToastLevel = iota
	ToastWarn
	ToastError
)

// ToastMsg shows a toast notification.
type ToastMsg struct {
	Level ToastLevel
	Text  string
}

// OpenResolverMsg opens the conflict resolver. An empty Path starts at the
// first conflicted file.
type OpenResolverMsg struct {
	Path string
}

// TagCount is a tag and the number of notes that carry it.
type TagCount struct {
	Tag   string
	Count int
}

// RequestNewNote asks the app to show the new-note dialog for Folder
// (vault-relative, "" for the root).
type RequestNewNote struct {
	Folder string
}

// RequestNewFolder asks the app to show the new-folder dialog inside Parent.
type RequestNewFolder struct {
	Parent string
}

// RequestRename asks the app to show the rename dialog for Path.
type RequestRename struct {
	Path string
}

// RequestMove asks the app to show the move dialog for Path.
type RequestMove struct {
	Path string
}

// MoveOpenNoteMsg asks the app to show the move dialog for the open note.
type MoveOpenNoteMsg struct{}

// MoveLinesToNoteMsg asks the app to move the open note's selected lines
// (or its cursor line) to the end of another note, chosen in the finder.
type MoveLinesToNoteMsg struct{}

// RequestTrash asks the app to move Path to the trash (after confirmation).
type RequestTrash struct {
	Path string
}

// TogglePinMsg pins or unpins the note at Path.
type TogglePinMsg struct {
	Path string
}

// OpenHistoryMsg opens the history view for Path.
type OpenHistoryMsg struct {
	Path string
}

// FilterTagMsg filters the sidebar tree to notes carrying Tag.
type FilterTagMsg struct {
	Tag string
}

// ClearFilterMsg clears the sidebar tag filter.
type ClearFilterMsg struct{}

// Entry identifies one of the sidebar's bottom entries.
type Entry int

// Sidebar entries.
const (
	EntryTasks Entry = iota
	EntryConflicts
	EntryTrash
)

// ActivateEntryMsg opens the Tasks view, the resolver, or the Trash view.
type ActivateEntryMsg struct {
	Entry Entry
}

// OpenErrorLogMsg opens the error log overlay.
type OpenErrorLogMsg struct{}

// OpenFileExternalMsg opens a non-note file at Path in the external editor.
type OpenFileExternalMsg struct {
	Path string
}

// OpenHelpMsg opens the help overlay.
type OpenHelpMsg struct{}

// OpenPaletteMsg opens the command palette.
type OpenPaletteMsg struct{}

// OpenFinderMsg opens the fuzzy finder, or full-text search when FullText
// is true.
type OpenFinderMsg struct {
	FullText bool
}

// CycleNoteViewMsg cycles the note view: editor, split, preview.
type CycleNoteViewMsg struct{}

// ToggleSidebarMsg shows or hides the sidebar.
type ToggleSidebarMsg struct{}

// SyncState is the syncer's state as shown in the status bar (spec §4.3).
type SyncState string

// Sync states. SyncUnknown means no sync status has been reported yet.
const (
	SyncUnknown   SyncState = ""
	SyncSynced    SyncState = "synced"
	SyncSyncing   SyncState = "syncing"
	SyncOffline   SyncState = "offline"
	SyncConflict  SyncState = "conflict"
	SyncError     SyncState = "error"
	SyncLocalOnly SyncState = "local"
)

// SyncStatusMsg reports the syncer's state for the status bar. It is a
// placeholder until the syncer exists.
type SyncStatusMsg struct {
	State     SyncState
	Pending   int
	Conflicts int
}
