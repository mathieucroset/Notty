package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// settle lets a real watcher report the app's own file operations, and
// processes what it reports.
func settle(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		if msgs := collect(m, false); len(msgs) > 0 {
			drive(t, m, msgs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// confirm answers the dialog on top with value.
func confirm(t *testing.T, m *Model, value string) {
	t.Helper()
	o := m.topOverlay()
	if o == nil || o.kind != overlayDialog {
		t.Fatalf("no dialog open; toasts %v", toastTexts(m))
	}
	run(t, m, dialog.ResultMsg{ID: o.dialog.ID(), OK: true, Value: value})
}

func TestUnchangedDiskDoesNotAsk(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "mine ")
	// Rewritten with the content Notty loaded (a touch, a sync that
	// changed nothing): not an external change.
	writeFile(t, opts.Vault, "ideas.md", "# Ideas\n\nA note app in the terminal.\n")
	run(t, m, watchEventMsg{paths: []string{"ideas.md"}})
	if m.overlayOpen() {
		t.Fatal("asked although the file matches what Notty last read")
	}
	if !m.editor.Dirty() || !strings.HasPrefix(m.editor.Content(), "mine ") {
		t.Error("buffer lost its edits")
	}
	// After a save the baseline moves: the saved content is not external.
	run(t, m, keyMsg("ctrl+s"))
	insertText(t, m, "more ")
	writeFile(t, opts.Vault, "ideas.md", readFile(t, opts.Vault, "ideas.md"))
	run(t, m, watchEventMsg{paths: []string{"ideas.md"}})
	if m.overlayOpen() {
		t.Error("asked about Notty's own save")
	}
}

func TestExternalChangeDialogDefaultsToKeepMine(t *testing.T) {
	m, opts := dirtyExternal(t)
	run(t, m, keyMsg("enter"))
	if got := readFile(t, opts.Vault, "ideas.md"); !strings.HasPrefix(got, "mine ") {
		t.Errorf("enter on the dialog did not keep the edits: %q", got)
	}
}

func TestRenameOpenDirtyNoteWithWatcher(t *testing.T) {
	opts := testOptions(t)
	realWatcher(t, &opts)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "unsaved ")
	run(t, m, msgs.RequestRename{Path: "ideas.md"})
	confirm(t, m, "thoughts")
	settle(t, m)
	if exists(opts.Vault, "ideas.md") || !exists(opts.Vault, "thoughts.md") {
		t.Fatal("note not renamed")
	}
	if got := readFile(t, opts.Vault, "thoughts.md"); !strings.HasPrefix(got, "unsaved ") {
		t.Errorf("buffer not saved before the rename: %q", got)
	}
	if m.NotePath() != "thoughts.md" || m.editor.Path() != "thoughts.md" || m.editor.Dirty() {
		t.Errorf("open %q / editor %q dirty %v", m.NotePath(), m.editor.Path(), m.editor.Dirty())
	}
	if m.overlayOpen() || !strings.HasPrefix(m.editor.Content(), "unsaved ") {
		t.Errorf("overlay %v buffer %q", m.overlayOpen(), m.editor.Content())
	}
}

func TestRenameFolderOfOpenDirtyNote(t *testing.T) {
	opts := testOptions(t)
	realWatcher(t, &opts)
	m := openNote(t, opts, "Work/Standup notes.md")
	insertText(t, m, "unsaved ")
	run(t, m, msgs.RequestRename{Path: "Work"})
	confirm(t, m, "Office")
	settle(t, m)
	if got := readFile(t, opts.Vault, "Office/Standup notes.md"); !strings.HasPrefix(got, "unsaved ") {
		t.Errorf("buffer not saved before the folder rename: %q", got)
	}
	if m.NotePath() != "Office/Standup notes.md" || m.editor.Dirty() || m.overlayOpen() {
		t.Errorf("open %q dirty %v overlay %v", m.NotePath(), m.editor.Dirty(), m.overlayOpen())
	}
}

func TestMoveOpenDirtyNoteTakesRewrittenLinks(t *testing.T) {
	opts := testOptions(t)
	realWatcher(t, &opts)
	writeFile(t, opts.Vault, "Work/pic.md", "# Pic\n\n![](img.png)\n")
	writeAbs(t, filepath.Join(opts.Vault.Root, "Work", "img.png"), []byte("png"))
	m := openNote(t, opts, "Work/pic.md")
	insertText(t, m, "unsaved ")
	run(t, m, msgs.RequestMove{Path: "Work/pic.md"})
	confirm(t, m, "")
	settle(t, m)
	got := readFile(t, opts.Vault, "pic.md")
	if !strings.HasPrefix(got, "unsaved ") || !strings.Contains(got, "![](Work/img.png)") {
		t.Fatalf("moved file = %q", got)
	}
	if m.NotePath() != "pic.md" || m.editor.Content() != got || m.editor.Dirty() {
		t.Errorf("open %q, buffer %q (dirty %v), want the rewritten file", m.NotePath(), m.editor.Content(), m.editor.Dirty())
	}
	if m.overlayOpen() {
		t.Error("the move asked about an external change")
	}
}

func TestTrashOpenDirtyNoteSavesFirst(t *testing.T) {
	opts := testOptions(t)
	realWatcher(t, &opts)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "last words ")
	run(t, m, msgs.RequestTrash{Path: "ideas.md"})
	confirm(t, m, "")
	settle(t, m)
	if exists(opts.Vault, "ideas.md") || m.NotePath() != "" {
		t.Fatalf("note not trashed and closed (open %q)", m.NotePath())
	}
	items, err := opts.Vault.TrashItems()
	if err != nil || len(items) != 1 {
		t.Fatalf("trash = %v, %v", items, err)
	}
	b, err := os.ReadFile(opts.Vault.Abs(opts.Vault.TrashContentPath(items[0])))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "last words ") {
		t.Errorf("trashed note lost the unsaved edits: %q", b)
	}
}

func TestOpOnOpenNoteAbortsWhenSaveFails(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "unsaved ")
	root := opts.Vault.Root
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	run(t, m, msgs.RequestRename{Path: "ideas.md"})
	confirm(t, m, "thoughts")
	if !hasToast(m, msgs.ToastError, "was not renamed") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if !exists(opts.Vault, "ideas.md") || m.NotePath() != "ideas.md" || !m.editor.Dirty() {
		t.Error("the rename went ahead after the save failed")
	}
}
