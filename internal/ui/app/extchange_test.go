package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

const external = "# Ideas\n\nedited elsewhere\nsecond line\n"

func TestExternalChangeReloadsCleanBuffer(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	pressKeys(t, m, "j", "j")
	writeFile(t, opts.Vault, "ideas.md", external)
	run(t, m, watchEventMsg{paths: []string{"ideas.md"}})
	if m.editor.Content() != external || m.editor.Dirty() {
		t.Fatalf("buffer = %q dirty %v", m.editor.Content(), m.editor.Dirty())
	}
	if m.editor.CursorLine() != 2 {
		t.Errorf("cursor line = %d, want kept on 2", m.editor.CursorLine())
	}
	if m.overlayOpen() {
		t.Error("a clean buffer asked before reloading")
	}
}

// dirtyExternal opens ideas.md, edits it, changes the file on disk and
// reports the change; the dialog should then be open.
func dirtyExternal(t *testing.T) (*Model, Options) {
	t.Helper()
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "mine ")
	writeFile(t, opts.Vault, "ideas.md", external)
	run(t, m, watchEventMsg{paths: []string{"ideas.md"}})
	o := m.topOverlay()
	if o == nil || o.kind != overlayDialog || o.dialog.ID() != dlgExternalChange {
		t.Fatalf("no external change dialog; overlays = %v", m.overlays)
	}
	s := screen(m)
	for _, want := range []string{"'ideas' changed on disk", "Reload from disk", "Keep mine"} {
		if !strings.Contains(s, want) {
			t.Errorf("dialog missing %q:\n%s", want, s)
		}
	}
	return m, opts
}

func TestExternalChangeDirtyReloadFromDisk(t *testing.T) {
	m, opts := dirtyExternal(t)
	// Autosave is held while the dialog waits.
	run(t, m, editor.AutosaveTickMsg{Path: "ideas.md", Version: m.editor.Version()})
	if got := readFile(t, opts.Vault, "ideas.md"); got != external {
		t.Fatalf("autosave overwrote the external change while asking: %q", got)
	}
	run(t, m, keyMsg("j"))
	run(t, m, keyMsg("enter")) // Reload from disk
	if m.overlayOpen() {
		t.Error("dialog still open")
	}
	if m.editor.Content() != external || m.editor.Dirty() {
		t.Errorf("buffer = %q dirty %v", m.editor.Content(), m.editor.Dirty())
	}
	if m.extConflict != "" {
		t.Error("saves still held after the answer")
	}
}

func TestExternalChangeDirtyKeepMine(t *testing.T) {
	m, opts := dirtyExternal(t)
	run(t, m, keyMsg("enter")) // Keep mine
	if !strings.HasPrefix(m.editor.Content(), "mine # Ideas") {
		t.Fatalf("buffer = %q", m.editor.Content())
	}
	if got := readFile(t, opts.Vault, "ideas.md"); !strings.HasPrefix(got, "mine # Ideas") {
		t.Errorf("keeping mine did not overwrite the file: %q", got)
	}
	if m.editor.Dirty() || m.extConflict != "" {
		t.Errorf("dirty %v, held %q", m.editor.Dirty(), m.extConflict)
	}
}

func TestExternalChangeEscKeepsMine(t *testing.T) {
	m, opts := dirtyExternal(t)
	run(t, m, keyMsg("esc"))
	if m.overlayOpen() || !strings.HasPrefix(m.editor.Content(), "mine ") {
		t.Fatalf("esc: overlay %v buffer %q", m.overlayOpen(), m.editor.Content())
	}
	if got := readFile(t, opts.Vault, "ideas.md"); !strings.HasPrefix(got, "mine ") {
		t.Errorf("file = %q", got)
	}
}

func TestExternalChangeSameContentIsQuiet(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "same ")
	writeFile(t, opts.Vault, "ideas.md", m.editor.Content())
	run(t, m, watchEventMsg{paths: []string{"ideas.md"}})
	if m.overlayOpen() || m.editor.Dirty() {
		t.Errorf("identical content: overlay %v dirty %v", m.overlayOpen(), m.editor.Dirty())
	}
}

func TestExternalDeleteKeepsDirtyBuffer(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "unsaved ")
	if err := os.Remove(filepath.Join(opts.Vault.Root, "ideas.md")); err != nil {
		t.Fatal(err)
	}
	run(t, m, watchEventMsg{paths: []string{"ideas.md"}})
	if m.NotePath() != "ideas.md" || !strings.HasPrefix(m.editor.Content(), "unsaved ") {
		t.Fatalf("dirty note closed: open %q", m.NotePath())
	}
	if !hasToast(m, msgs.ToastWarn, "deleted on disk") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	run(t, m, keyMsg("ctrl+s"))
	if got := readFile(t, opts.Vault, "ideas.md"); !strings.HasPrefix(got, "unsaved ") {
		t.Errorf("saving did not recreate the note: %q", got)
	}
}

func TestExternalDeleteClosesCleanNote(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	if err := os.Remove(filepath.Join(opts.Vault.Root, "ideas.md")); err != nil {
		t.Fatal(err)
	}
	run(t, m, watchEventMsg{paths: []string{"ideas.md"}})
	if m.NotePath() != "" {
		t.Errorf("clean deleted note still open: %q", m.NotePath())
	}
}
