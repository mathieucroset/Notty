package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// trashedOptions is testOptions with ideas.md already in the trash.
func trashedOptions(t *testing.T) Options {
	t.Helper()
	opts := testOptions(t)
	if _, err := opts.Vault.Trash("ideas.md"); err != nil {
		t.Fatal(err)
	}
	return opts
}

func TestTrashView(t *testing.T) {
	m := start(t, trashedOptions(t), 120, 30)
	if !strings.Contains(screen(m), "Trash (1)") {
		t.Errorf("sidebar trash count missing:\n%s", screen(m))
	}
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTrash})
	if m.MainView() != ViewTrash || m.Focus() != FocusMain {
		t.Fatalf("MainView = %v, focus = %v", m.MainView(), m.Focus())
	}
	s := screen(m)
	for _, want := range []string{"Trash · 1 item ─", "ideas", "A note app in the terminal"} {
		if !strings.Contains(s, want) {
			t.Errorf("trash view missing %q:\n%s", want, s)
		}
	}
	assertSize(t, m, 120, 30)
	run(t, m, keyMsg("esc"))
	if m.MainView() != ViewNote {
		t.Errorf("esc: MainView = %v", m.MainView())
	}
}

func TestTrashRestore(t *testing.T) {
	opts := trashedOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTrash})
	run(t, m, keyMsg("enter"))
	if !exists(opts.Vault, "ideas.md") {
		t.Fatal("note not restored")
	}
	if _, ok := m.ix.Get("ideas.md"); !ok {
		t.Error("restored note not indexed")
	}
	if !hasToast(m, msgs.ToastInfo, "Restored to ideas.md") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if s := screen(m); !strings.Contains(s, "Trash (0)") {
		t.Errorf("trash count not refreshed:\n%s", s)
	}
}

func TestTrashRestoreFolderIndexesNotes(t *testing.T) {
	opts := testOptions(t)
	if _, err := opts.Vault.Trash("Work"); err != nil {
		t.Fatal(err)
	}
	m := start(t, opts, 120, 30)
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTrash})
	if !strings.Contains(screen(m), "Standup notes.md") {
		t.Errorf("folder preview missing its listing:\n%s", screen(m))
	}
	run(t, m, keyMsg("enter"))
	if _, ok := m.ix.Get("Work/Standup notes.md"); !ok {
		t.Error("notes of the restored folder not indexed")
	}
}

func TestTrashDeleteForever(t *testing.T) {
	opts := trashedOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTrash})
	run(t, m, keyMsg("D"))
	if s := screen(m); !strings.Contains(s, "Delete 'ideas.md' forever?") {
		t.Fatalf("confirmation missing:\n%s", s)
	}
	run(t, m, keyMsg("y"))
	if items, _ := opts.Vault.TrashItems(); len(items) != 0 {
		t.Errorf("trash = %v, want empty", items)
	}
	if !hasToast(m, msgs.ToastInfo, "Deleted 'ideas.md' forever") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}

func TestTrashEmpty(t *testing.T) {
	opts := trashedOptions(t)
	if _, err := opts.Vault.Trash("Work"); err != nil {
		t.Fatal(err)
	}
	m := start(t, opts, 120, 30)
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTrash})
	run(t, m, keyMsg("E"))
	if s := screen(m); !strings.Contains(s, "Delete all 2 items in the trash forever?") {
		t.Fatalf("confirmation missing:\n%s", s)
	}
	run(t, m, keyMsg("n"))
	if items, _ := opts.Vault.TrashItems(); len(items) != 2 {
		t.Fatalf("n emptied the trash: %v", items)
	}
	run(t, m, keyMsg("E"))
	run(t, m, keyMsg("y"))
	if items, _ := opts.Vault.TrashItems(); len(items) != 0 {
		t.Errorf("trash = %v, want empty", items)
	}
	if !strings.Contains(screen(m), "Trash (0)") {
		t.Errorf("count not refreshed:\n%s", screen(m))
	}
}

func TestTrashPurgedAtStartup(t *testing.T) {
	opts := trashedOptions(t)
	opts.Config.Trash.RetentionDays = 30
	opts.Now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	start(t, opts, 120, 30)
	if items, _ := opts.Vault.TrashItems(); len(items) != 0 {
		t.Errorf("old trash item not purged: %v", items)
	}

	opts = trashedOptions(t)
	opts.Config.Trash.RetentionDays = 30
	start(t, opts, 120, 30)
	if items, _ := opts.Vault.TrashItems(); len(items) != 1 {
		t.Errorf("recent trash item purged: %v", items)
	}
}

// TestTrashRestoreFlow restores a note from the Trash view in a real
// program.
func TestTrashRestoreFlow(t *testing.T) {
	opts := trashedOptions(t)
	tm := newProgram(t, opts)
	waitScreen(t, tm, "N O T E S")
	tm.Send(msgs.ActivateEntryMsg{Entry: msgs.EntryTrash})
	waitScreen(t, tm, "Trash · 1 item")
	tm.Send(keyMsg("enter"))
	waitScreen(t, tm, "Restored to ideas.md")
	tm.Send(keyMsg("ctrl+q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	if !exists(opts.Vault, "ideas.md") {
		t.Error("note not restored")
	}
}
