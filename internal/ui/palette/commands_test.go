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

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
