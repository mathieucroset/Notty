package palette

import (
	"fmt"
	"testing"

	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// wantCommands is the spec §8 command list. Names must exactly match
// DefaultCommands (case-insensitively; DefaultCommands owns the exact
// casing shown in the UI).
var wantCommands = []string{
	"new note",
	"new folder",
	"move line to note…",
	"move note to folder…",
	"switch theme",
	"toggle vim",
	"toggle line numbers",
	"sync now",
	"set up sync",
	"resolve conflicts",
	"open tasks",
	"open trash",
	"clean unused attachments",
	"error log",
	"open config",
	"help",
	"quit",
}

func TestDefaultCommandsCoversSpecList(t *testing.T) {
	cmds := DefaultCommands()
	if len(cmds) != len(wantCommands) {
		t.Fatalf("got %d commands, want %d", len(cmds), len(wantCommands))
	}
	got := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		if c.Name == "" {
			t.Errorf("command %q has an empty Name", c.ID)
		}
		if c.ID == "" {
			t.Errorf("command %q has an empty ID", c.Name)
		}
		got[toLower(c.Name)] = true
	}
	for _, want := range wantCommands {
		if !got[want] {
			t.Errorf("DefaultCommands missing spec command %q", want)
		}
	}
}

func TestDefaultCommandsSwitchThemeHasNoMsg(t *testing.T) {
	for _, c := range DefaultCommands() {
		if c.ID == idSwitchTheme {
			if c.Msg != nil {
				t.Error("switch theme command should have a nil Msg (the palette handles it internally)")
			}
			return
		}
	}
	t.Fatal("no switch-theme command found")
}

func TestDefaultCommandsEmitExpectedMessages(t *testing.T) {
	cmds := DefaultCommands()
	byID := make(map[string]Command, len(cmds))
	for _, c := range cmds {
		byID[c.ID] = c
	}

	check := func(id string, want any) {
		t.Helper()
		c, ok := byID[id]
		if !ok {
			t.Fatalf("missing command %q", id)
		}
		if c.Msg == nil {
			t.Fatalf("command %q has a nil Msg", id)
		}
		got := c.Msg()
		if gotType, wantType := fmt.Sprintf("%T", got), fmt.Sprintf("%T", want); gotType != wantType {
			t.Errorf("command %q emits %s, want %s", id, gotType, wantType)
		}
	}

	check("new-note", msgs.RequestNewNote{})
	check("new-folder", msgs.RequestNewFolder{})
	check("move-lines", msgs.MoveLinesToNoteMsg{})
	check("move-note", msgs.MoveOpenNoteMsg{})
	check("toggle-vim", ToggleVimMsg{})
	check("toggle-line-numbers", ToggleLineNumbersMsg{})
	check("sync-now", SyncNowMsg{})
	check("setup-sync", SetupSyncMsg{})
	check("resolve-conflicts", msgs.OpenResolverMsg{})
	check("clean-attachments", CleanAttachmentsMsg{})
	check("error-log", msgs.OpenErrorLogMsg{})
	check("open-config", OpenConfigMsg{})
	check("help", msgs.OpenHelpMsg{})
	check("quit", msgs.QuitMsg{})

	tasks, ok := byID["tasks"]
	if !ok {
		t.Fatal("missing tasks command")
	}
	if got, ok := tasks.Msg().(msgs.ActivateEntryMsg); !ok || got.Entry != msgs.EntryTasks {
		t.Errorf("tasks command emits %#v, want ActivateEntryMsg{Entry: EntryTasks}", tasks.Msg())
	}

	trash, ok := byID["trash"]
	if !ok {
		t.Fatal("missing trash command")
	}
	if got, ok := trash.Msg().(msgs.ActivateEntryMsg); !ok || got.Entry != msgs.EntryTrash {
		t.Errorf("trash command emits %#v, want ActivateEntryMsg{Entry: EntryTrash}", trash.Msg())
	}
}

func TestDefaultCommandsHelpAndQuitKeysMatchGlobalKeys(t *testing.T) {
	cmds := DefaultCommands()
	byID := make(map[string]Command, len(cmds))
	for _, c := range cmds {
		byID[c.ID] = c
	}

	if got, want := byID["help"].Key, keys.GlobalKeys[keys.Help].Help().Key; got != want {
		t.Errorf("help command Key = %q, want %q", got, want)
	}
	if got, want := byID["quit"].Key, keys.GlobalKeys[keys.Quit].Help().Key; got != want {
		t.Errorf("quit command Key = %q, want %q", got, want)
	}
}

// TestDefaultCommandsDeriveKeysFromKeyTables asserts that the Key shown for
// each spec §8 command that mirrors a sidebar action is derived from
// keys.Bindings(keys.Sidebar) by matching its help description, not
// hardcoded, and is rendered with a dim "key · context" label. It
// independently re-derives the expected key from the same key table (rather
// than calling the production keyFor helper) so a stale hardcoded string in
// DefaultCommands, or a description that no longer matches, both fail this
// test.
func TestDefaultCommandsDeriveKeysFromKeyTables(t *testing.T) {
	findKey := func(t *testing.T, ctx keys.Context, desc string) string {
		t.Helper()
		for _, b := range keys.Bindings(ctx) {
			if b.Help().Desc == desc {
				return b.Help().Key
			}
		}
		t.Fatalf("no binding in %s with description %q", ctx, desc)
		return ""
	}

	cmds := DefaultCommands()
	byID := make(map[string]Command, len(cmds))
	for _, c := range cmds {
		byID[c.ID] = c
	}

	cases := []struct {
		id   string
		desc string
	}{
		{"new-note", "new note"},
		{"new-folder", "new folder"},
		{"resolve-conflicts", "open resolver"},
		{"error-log", "error log"},
	}
	for _, tc := range cases {
		want := findKey(t, keys.Sidebar, tc.desc) + " · sidebar"
		if got := byID[tc.id].Key; got != want {
			t.Errorf("%s: Key = %q, want %q", tc.id, got, want)
		}
	}

	if got, want := byID["move-lines"].Key, findKey(t, keys.EditorNormal, "move lines to note")+" · editor · normal"; got != want {
		t.Errorf("move-lines: Key = %q, want %q", got, want)
	}
	if got, want := byID["move-note"].Key, findKey(t, keys.EditorNormal, "move note to folder")+" · editor · normal"; got != want {
		t.Errorf("move-note: Key = %q, want %q", got, want)
	}

	if got := byID["tasks"].Key; got != "" {
		t.Errorf("tasks: Key = %q, want empty (no dedicated keybinding)", got)
	}
	if got := byID["trash"].Key; got != "" {
		t.Errorf("trash: Key = %q, want empty (no dedicated keybinding)", got)
	}
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
