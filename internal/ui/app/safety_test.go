package app

import (
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/ui/history"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestDroppedExternalChangeDialogKeepsMine(t *testing.T) {
	m, opts := dirtyExternal(t)
	run(t, m, msgs.OpenPaletteMsg{}) // replaces the overlay stack
	if m.extConflict != "" {
		t.Error("saves still held after the dialog was dropped")
	}
	if got := readFile(t, opts.Vault, "ideas.md"); !strings.HasPrefix(got, "mine ") {
		t.Errorf("dropping the dialog did not keep the edits: %q", got)
	}
}

func TestExternalChangeDialogStacksOnTop(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "mine ")
	run(t, m, msgs.RequestRename{Path: "Work/Standup notes.md"})
	writeFile(t, opts.Vault, "ideas.md", external)
	run(t, m, watchEventMsg{paths: []string{"ideas.md"}})
	if len(m.overlays) != 2 || m.topOverlay().dialog.ID() != dlgExternalChange {
		t.Fatalf("external change dialog not stacked: %d overlays", len(m.overlays))
	}
	run(t, m, keyMsg("enter")) // keep mine
	if o := m.topOverlay(); o == nil || o.dialog.ID() != dlgRename {
		t.Error("the rename dialog did not come back")
	}
}

func TestEditorFocusRequestAppliesBeforeNextKey(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	cur := m.editor.Cursor()
	// Two keys delivered before any command runs, as when typing fast.
	m.Update(keyMsg("tab"))
	m.Update(keyMsg("j"))
	if m.Focus() != FocusSidebar {
		t.Fatal("tab did not focus the sidebar right away")
	}
	if m.editor.Cursor() != cur {
		t.Error("the key after tab still went to the editor")
	}
}

func TestOlderSnapshotNeverLandsAfterNewer(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	older := m.saveNoteCmd("ideas.md", "older\n", 1)
	newer := m.saveNoteCmd("ideas.md", "newer\n", 2)
	if res := newer().(savedMsg); res.err != nil || res.stale {
		t.Fatalf("newer save = %+v", res)
	}
	res := older().(savedMsg)
	if !res.stale {
		t.Errorf("older save after the newer one = %+v, want stale", res)
	}
	if got := readFile(t, opts.Vault, "ideas.md"); got != "newer\n" {
		t.Errorf("file = %q", got)
	}
	run(t, m, res) // a stale result changes nothing
	if m.baseline == "" {
		t.Error("a stale save reset the baseline")
	}
}

func TestQuitWaitsForInflightSaves(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	save := m.saveNoteCmd("ideas.md", "late save\n", 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		save()
	}()
	if !hasQuit(run(t, m, keyMsg("ctrl+q"))) {
		t.Fatal("did not quit")
	}
	if got := readFile(t, opts.Vault, "ideas.md"); got != "late save\n" {
		t.Errorf("quit did not wait for the running save: %q", got)
	}
}

func TestRestoreIntoLockedBufferWarns(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	m.editor = m.editor.Lock()
	run(t, m, history.RestoreVersionMsg{Path: "ideas.md", Rev: "abcdef1234", Content: "old\n"})
	if !hasToast(m, msgs.ToastWarn, "Could not restore") || hasToast(m, msgs.ToastInfo, "Restored") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if got := readFile(t, opts.Vault, "ideas.md"); got == "old\n" {
		t.Error("restore saved although the buffer did not take it")
	}
}

func TestPreviewViewModeLabel(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	run(t, m, keyMsg("ctrl+g"))
	run(t, m, keyMsg("ctrl+g"))
	if last := lastLine(screen(m)); !strings.Contains(last, "PREVIEW") {
		t.Errorf("status row = %q", last)
	}
}
