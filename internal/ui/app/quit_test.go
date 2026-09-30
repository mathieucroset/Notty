package app

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestQuitSavesDirtyBufferAndCursor(t *testing.T) {
	for _, quit := range []func(t *testing.T, m *Model) []tea.Msg{
		func(t *testing.T, m *Model) []tea.Msg { return run(t, m, keyMsg("ctrl+q")) },
		func(t *testing.T, m *Model) []tea.Msg {
			pressKeys(t, m, ":")
			return run(t, m, tea.PasteMsg{Content: "q"})
		},
	} {
		opts := testOptions(t)
		m := openNote(t, opts, "ideas.md")
		pressKeys(t, m, "j", "j")
		insertText(t, m, "bye ")
		var out []tea.Msg
		if out = quit(t, m); len(out) == 0 {
			// :q runs on enter.
			out = run(t, m, keyMsg("enter"))
		}
		if !hasQuit(out) {
			t.Fatalf("did not quit: %v", out)
		}
		if got := readFile(t, opts.Vault, "ideas.md"); got != "# Ideas\n\nbye A note app in the terminal.\n" {
			t.Errorf("dirty buffer not saved on quit: %q", got)
		}
		saved, err := localstate.Load(opts.LocalPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := saved.Cursor["ideas.md"]; got[0] != 2 {
			t.Errorf("cursor not saved on quit: %v", got)
		}
	}
}

func TestQuitWithFailedSaveAsksAgain(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "x")
	abs := filepath.Join(opts.Vault.Root, "ideas.md")
	if err := os.Remove(abs); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(abs, 0o755); err != nil {
		t.Fatal(err)
	}
	if hasQuit(run(t, m, keyMsg("ctrl+q"))) {
		t.Fatal("quit although the buffer could not be saved")
	}
	if !hasToast(m, msgs.ToastError, "Quit again to discard") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if !hasQuit(run(t, m, keyMsg("ctrl+q"))) {
		t.Error("second quit did not discard and quit")
	}
}

func TestFailedQuitThenEditSavesAgain(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "first ")
	root := opts.Vault.Root
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if hasQuit(run(t, m, keyMsg("ctrl+q"))) {
		t.Fatal("quit although the save failed")
	}
	if !m.discardOnQuit {
		t.Fatal("failed quit did not arm the discard")
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	insertText(t, m, "second ")
	if m.discardOnQuit {
		t.Error("an edit did not disarm the discard")
	}
	if !hasQuit(run(t, m, keyMsg("ctrl+q"))) {
		t.Fatal("did not quit")
	}
	if got := readFile(t, opts.Vault, "ideas.md"); !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("quit after the fix did not save: %q", got)
	}
}

func TestSuccessfulSaveDisarmsDiscard(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "x")
	m.discardOnQuit = true
	run(t, m, keyMsg("ctrl+s"))
	if m.discardOnQuit {
		t.Error("a successful save did not disarm the discard")
	}
}

func TestQuitCleanBufferDoesNotWrite(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	abs := filepath.Join(opts.Vault.Root, "ideas.md")
	before, err := os.Stat(abs)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if !hasQuit(run(t, m, keyMsg("ctrl+q"))) {
		t.Fatal("did not quit")
	}
	after, err := os.Stat(abs)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("quitting rewrote a clean note")
	}
}

// TestQuitDeletesKittyImages runs a real program with Kitty inline images:
// the preview transmits the image once the terminal is ready, and quitting
// writes the delete sequence before the program ends.
func TestQuitDeletesKittyImages(t *testing.T) {
	opts := testOptions(t)
	opts.Caps = imgrender.Caps{Inline: imgrender.ProtoKitty, Viewer: imgrender.ProtoKitty, CellW: 8, CellH: 16}
	src := writePNG(t, 64, 32)
	if err := os.MkdirAll(filepath.Join(opts.Vault.Root, "attachments"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	writeAbs(t, filepath.Join(opts.Vault.Root, "attachments", "pic.png"), data)
	writeFile(t, opts.Vault, "pics.md", "# Pics\n\n![](/attachments/pic.png)\n")
	opts.Local.LastNote = "pics.md"

	tm := newProgram(t, opts)
	waitScreen(t, tm, "Pics")
	tm.Send(keyMsg("ctrl+g"))
	tm.Send(keyMsg("ctrl+g"))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "\x1b_G") // a Kitty transmission
	}, teatest.WithDuration(5*time.Second))
	tm.Send(keyMsg("ctrl+q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	final := finalModel(t, tm)
	if final.preview.KittyCleanup() == "" {
		t.Fatal("no Kitty image was recorded as transmitted")
	}
	b, err := io.ReadAll(tm.FinalOutput(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "a=d") {
		t.Errorf("no Kitty delete written on quit")
	}
}
