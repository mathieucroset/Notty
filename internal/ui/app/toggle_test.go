package app

import (
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestToggleInOpenNoteChangesBufferNotDisk(t *testing.T) {
	opts := tasksOptions(t)
	m := openNote(t, opts, "Work/Todo.md")
	run(t, m, msgs.ToggleTaskMsg{Path: "Work/Todo.md", Line: 3, Text: "- [ ] buy milk"})
	if !strings.Contains(m.editor.Content(), "- [x] buy milk") {
		t.Fatalf("buffer not toggled: %q", m.editor.Content())
	}
	if got := readFile(t, opts.Vault, "Work/Todo.md"); got != todoNote {
		t.Errorf("toggle wrote the file directly: %q", got)
	}
	if !m.editor.Dirty() {
		t.Error("toggled buffer is not dirty, so autosave would skip it")
	}
	// One undo step.
	run(t, m, keyMsg("u"))
	if m.editor.Content() != todoNote {
		t.Errorf("undo gives %q", m.editor.Content())
	}
	// Autosave then writes it.
	run(t, m, msgs.ToggleTaskMsg{Path: "Work/Todo.md", Line: 3, Text: "- [ ] buy milk"})
	run(t, m, editor.AutosaveTickMsg{Path: "Work/Todo.md", Version: m.editor.Version()})
	if got := readFile(t, opts.Vault, "Work/Todo.md"); !strings.Contains(got, "- [x] buy milk") {
		t.Errorf("autosave did not write the toggle: %q", got)
	}
}

func TestToggleMissingTaskInOpenNoteWarns(t *testing.T) {
	opts := tasksOptions(t)
	m := openNote(t, opts, "Work/Todo.md")
	run(t, m, msgs.ToggleTaskMsg{Path: "Work/Todo.md", Line: 3, Text: "- [ ] gone"})
	if !hasToast(m, msgs.ToastWarn, "not toggled") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if m.editor.Content() != todoNote || m.editor.Dirty() {
		t.Error("a missing task changed the buffer")
	}
}

func TestPreviewToggleGoesToBuffer(t *testing.T) {
	opts := tasksOptions(t)
	m := openNote(t, opts, "Work/Todo.md")
	run(t, m, keyMsg("ctrl+g"))
	run(t, m, keyMsg("ctrl+g"))
	pressKeys(t, m, "]", "t")
	run(t, m, spaceKey)
	if !strings.Contains(m.editor.Content(), "- [x] write report") {
		t.Fatalf("preview toggle did not reach the buffer: %q", m.editor.Content())
	}
	if got := readFile(t, opts.Vault, "Work/Todo.md"); got != todoNote {
		t.Errorf("preview toggle wrote the file: %q", got)
	}
	// The preview re-renders the toggled buffer.
	if !strings.Contains(screen(m), "●") {
		t.Errorf("no dirty marker after the preview toggle:\n%s", screen(m))
	}
}

func TestTasksViewToggleOnOpenNote(t *testing.T) {
	opts := tasksOptions(t)
	m := openNote(t, opts, "Work/Todo.md")
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTasks})
	run(t, m, spaceKey) // the overdue task
	if !strings.Contains(m.editor.Content(), "- [x] write report") {
		t.Fatalf("buffer not toggled: %q", m.editor.Content())
	}
	if got := readFile(t, opts.Vault, "Work/Todo.md"); got != todoNote {
		t.Errorf("file changed: %q", got)
	}
	if s := screen(m); !strings.Contains(s, "Tasks · 1 open") {
		t.Errorf("tasks view not refreshed from the buffer:\n%s", s)
	}
}
