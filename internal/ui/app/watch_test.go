package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/watcher"
)

// TestWatcherExternalEditRefreshes runs a real program with a real
// watcher: a note edited and a note created outside the app show up in the
// index and the tree.
func TestWatcherExternalEditRefreshes(t *testing.T) {
	opts := testOptions(t)
	w, err := watcher.New(opts.Vault.Root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	opts.Watcher = w
	tm := teatest.NewTestModel(t, New(opts), teatest.WithInitialTermSize(120, 30))
	waitScreen(t, tm, "NOTES", "ideas")

	writeFile(t, opts.Vault, "ideas.md", "# Ideas\n\nnow tagged #fresh\n")
	writeFile(t, opts.Vault, "Later.md", "# Later\n")
	waitScreen(t, tm, "#fresh", "Later")

	tm.Send(keyMsg("ctrl+q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	final := tm.FinalModel(t).(*Model)
	if n, ok := final.ix.Get("ideas.md"); !ok || !index.HasTag(n, "fresh") {
		t.Errorf("index not refreshed: %+v", n)
	}
	if final.opts.Watcher != nil {
		t.Error("watcher not closed on quit")
	}
	if _, open := <-w.Events(); open {
		t.Error("watcher still running after quit")
	}
}

func TestWatchEventReloadsOpenNote(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.OpenNoteMsg{Path: "ideas.md", Line: -1})
	writeFile(t, opts.Vault, "ideas.md", "# Ideas\n\nedited elsewhere #ext\n")
	run(t, m, watchEventMsg{paths: []string{"ideas.md"}})
	if !strings.Contains(screen(m), "edited elsewhere") {
		t.Errorf("open note not reloaded:\n%s", screen(m))
	}
	if n, _ := m.ix.Get("ideas.md"); !index.HasTag(n, "ext") {
		t.Error("index not updated")
	}
	if !strings.Contains(screen(m), "#ext") {
		t.Errorf("tag chips not refreshed:\n%s", screen(m))
	}
}

func TestWatchEventReindexesNewFolder(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	writeFile(t, opts.Vault, "Archive/2024/old.md", "# Old\n")
	run(t, m, watchEventMsg{paths: []string{"Archive"}})
	if _, ok := m.ix.Get("Archive/2024/old.md"); !ok {
		t.Error("note inside a new folder not indexed")
	}
}

func TestExternalDeletePrunesPinsAndLocalState(t *testing.T) {
	opts := testOptions(t)
	opts.Pins = &meta.State{Pins: []string{"Work/Standup notes.md", "ideas.md"}}
	opts.Local.Expanded = []string{"Work"}
	m := start(t, opts, 120, 30)
	if err := os.RemoveAll(filepath.Join(opts.Vault.Root, "Work")); err != nil {
		t.Fatal(err)
	}
	run(t, m, watchEventMsg{paths: []string{"Work", "ideas.md"}})
	if !reflect.DeepEqual(opts.Pins.Pins, []string{"ideas.md"}) {
		t.Errorf("pins = %v, want the deleted note's pin gone", opts.Pins.Pins)
	}
	if len(opts.Local.Expanded) != 0 {
		t.Errorf("expanded = %v, want the deleted folder gone", opts.Local.Expanded)
	}
	saved, _ := meta.Load(opts.Vault.Root)
	if !reflect.DeepEqual(saved.Pins, []string{"ideas.md"}) {
		t.Errorf("saved pins = %v", saved.Pins)
	}
	if _, ok := m.ix.Get("Work/Standup notes.md"); ok {
		t.Error("deleted note still indexed")
	}
}

func TestWatcherErrors(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, watchErrMsg{err: errors.New("cannot watch Private")})
	if !hasToast(m, msgs.ToastWarn, "cannot watch Private") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	run(t, m, watchErrMsg{err: watcher.ErrRootGone})
	if !hasToast(m, msgs.ToastError, "vault folder was removed") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}

func TestSaveNote(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.OpenNoteMsg{Path: "ideas.md", Line: -1})
	run(t, m, m.saveNoteCmd("ideas.md", "# Ideas\n\nsaved #kept\n", 7)())
	if s := readFile(t, opts.Vault, "ideas.md"); s != "# Ideas\n\nsaved #kept\n" {
		t.Errorf("file = %q", s)
	}
	if n, _ := m.ix.Get("ideas.md"); !index.HasTag(n, "kept") {
		t.Error("saved content not indexed")
	}
	if !strings.Contains(screen(m), "saved #kept") {
		t.Errorf("open note not updated:\n%s", screen(m))
	}
	// The command reports the version it saved.
	if msg, ok := m.saveNoteCmd("ideas.md", "x", 9)().(savedMsg); !ok || msg.version != 9 || msg.err != nil {
		t.Errorf("savedMsg = %+v", msg)
	}
}

func TestSaveNoteFailureToasts(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	// A folder cannot be overwritten by a note.
	run(t, m, m.saveNoteCmd("Work", "x", 1)())
	if !hasToast(m, msgs.ToastError, "Could not save Work") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}
