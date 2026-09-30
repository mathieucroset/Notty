// Package palette implements Notty's command palette (spec §8): a
// fuzzy-searchable list of every action, opened with ctrl+k, plus an inline
// theme picker with live preview (spec §4.5).
package palette

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// idSwitchTheme is the ID of the "Switch theme" command. The palette
// recognizes it and opens its internal theme picker instead of running Msg.
const idSwitchTheme = "switch-theme"

// Command is one entry in the command palette.
type Command struct {
	// ID identifies the command. The palette itself only ever inspects it to
	// recognize the "switch theme" command.
	ID string
	// Name is the fuzzy-matched, displayed label.
	Name string
	// Key is the keybinding shown at the right of the row (e.g. "ctrl+p"),
	// or "" when the command has no dedicated key.
	Key string
	// Msg produces the message the palette emits when this command runs. It
	// is nil for "Switch theme", which the palette handles internally.
	Msg func() tea.Msg
}

// ToggleVimMsg asks the app to toggle vim mode. Persisting the change to
// config.toml is the app's job.
type ToggleVimMsg struct{}

// ToggleLineNumbersMsg asks the app to toggle line numbers. Persisting the
// change to config.toml is the app's job.
type ToggleLineNumbersMsg struct{}

// SyncNowMsg asks the app to sync immediately.
type SyncNowMsg struct{}

// SetupSyncMsg asks the app to open the sync setup wizard.
type SetupSyncMsg struct{}

// CleanAttachmentsMsg asks the app to delete attachments no note references.
type CleanAttachmentsMsg struct{}

// OpenConfigMsg asks the app to open config.toml in $EDITOR.
type OpenConfigMsg struct{}

// globalKey returns the display key for a global action, e.g. "F1".
func globalKey(a keys.Action) string {
	return keys.GlobalKeys[a].Help().Key
}

// keyFor looks up the binding in ctx whose help description is desc, and
// formats it for display as "key · context" (e.g. "n · sidebar"). It
// derives the key from the real key tables instead of hardcoding it, so the
// palette can never show a keybinding that has drifted from ctx's actual
// bindings. It returns "" if no such binding exists.
func keyFor(ctx keys.Context, desc string) string {
	for _, b := range keys.Bindings(ctx) {
		if h := b.Help(); h.Desc == desc {
			return h.Key + " · " + strings.ToLower(ctx.String())
		}
	}
	return ""
}

// DefaultCommands returns the command palette's command list (spec §8), in
// the order they are shown when the query is empty.
func DefaultCommands() []Command {
	return []Command{
		{
			ID:   "new-note",
			Name: "New note",
			Key:  keyFor(keys.Sidebar, "new note"),
			Msg:  func() tea.Msg { return msgs.RequestNewNote{} },
		},
		{
			ID:   "new-folder",
			Name: "New folder",
			Key:  keyFor(keys.Sidebar, "new folder"),
			Msg:  func() tea.Msg { return msgs.RequestNewFolder{} },
		},
		{
			ID:   "move-lines",
			Name: "Move line to note…",
			Key:  keyFor(keys.EditorNormal, "move lines to note"),
			Msg:  func() tea.Msg { return msgs.MoveLinesToNoteMsg{} },
		},
		{
			ID:   "move-note",
			Name: "Move note to folder…",
			Key:  keyFor(keys.EditorNormal, "move note to folder"),
			Msg:  func() tea.Msg { return msgs.MoveOpenNoteMsg{} },
		},
		{
			ID:   idSwitchTheme,
			Name: "Switch theme",
			// Msg is nil: the palette opens its internal theme picker
			// instead of emitting a message for this command.
		},
		{
			ID:   "toggle-vim",
			Name: "Toggle vim",
			Msg:  func() tea.Msg { return ToggleVimMsg{} },
		},
		{
			ID:   "toggle-line-numbers",
			Name: "Toggle line numbers",
			Msg:  func() tea.Msg { return ToggleLineNumbersMsg{} },
		},
		{
			ID:   "sync-now",
			Name: "Sync now",
			Msg:  func() tea.Msg { return SyncNowMsg{} },
		},
		{
			ID:   "setup-sync",
			Name: "Set up sync",
			Msg:  func() tea.Msg { return SetupSyncMsg{} },
		},
		{
			ID:   "resolve-conflicts",
			Name: "Resolve conflicts",
			Key:  keyFor(keys.Sidebar, "open resolver"),
			Msg:  func() tea.Msg { return msgs.OpenResolverMsg{} },
		},
		{
			ID:   "tasks",
			Name: "Open Tasks",
			Msg:  func() tea.Msg { return msgs.ActivateEntryMsg{Entry: msgs.EntryTasks} },
		},
		{
			ID:   "trash",
			Name: "Open Trash",
			Msg:  func() tea.Msg { return msgs.ActivateEntryMsg{Entry: msgs.EntryTrash} },
		},
		{
			ID:   "clean-attachments",
			Name: "Clean unused attachments",
			Msg:  func() tea.Msg { return CleanAttachmentsMsg{} },
		},
		{
			ID:   "error-log",
			Name: "Error log",
			Key:  keyFor(keys.Sidebar, "error log"),
			Msg:  func() tea.Msg { return msgs.OpenErrorLogMsg{} },
		},
		{
			ID:   "open-config",
			Name: "Open config",
			Msg:  func() tea.Msg { return OpenConfigMsg{} },
		},
		{
			ID:   "help",
			Name: "Help",
			Key:  globalKey(keys.Help),
			Msg:  func() tea.Msg { return msgs.OpenHelpMsg{} },
		},
		{
			ID:   "quit",
			Name: "Quit",
			Key:  globalKey(keys.Quit),
			Msg:  func() tea.Msg { return msgs.QuitMsg{} },
		},
	}
}
