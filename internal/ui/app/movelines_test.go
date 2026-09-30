package app

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/finder"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

var (
	altM      = tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt}
	shiftDown = tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}
)

const (
	srcNote    = "# Src\n\none\ntwo\nthree\n"
	targetPath = "Work/Standup notes.md"
	targetNote = "# Standup notes\n\n- shipped auth flow\n- follow up with design\n"
)

// moveLinesSetup opens src.md with the cursor on line, in vim or plain
// mode.
func moveLinesSetup(t *testing.T, vimMode bool, line int) (*Model, Options) {
	t.Helper()
	opts := testOptions(t)
	opts.Config.Vim = vimMode
	writeFile(t, opts.Vault, "src.md", srcNote)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.OpenNoteMsg{Path: "src.md", Line: line})
	if m.NotePath() != "src.md" {
		t.Fatalf("src.md not open (open: %q)", m.NotePath())
	}
	return m, opts
}

// pickerOpen reports whether the finder is open in pick mode.
func pickerOpen(m *Model) bool {
	o := m.topOverlay()
	return o != nil && o.kind == overlayFinder && strings.Contains(screen(m), "Move to note")
}

func TestMoveLinesToNote(t *testing.T) {
	tests := []struct {
		name      string
		vim       bool
		line      int
		keys      []tea.Msg
		wantMoved string // appended to the target
		wantLeft  string // the source buffer
		wantToast string
	}{
		{"vim cursor line", true, 3, []tea.Msg{altM}, "two\n", "# Src\n\none\nthree\n", "Moved 1 line to Work/Standup notes.md"},
		{"vim visual line", true, 2, []tea.Msg{keyMsg("V"), keyMsg("j"), altM}, "one\ntwo\n", "# Src\n\nthree\n", "Moved 2 lines to Work/Standup notes.md"},
		{"vim charwise visual", true, 2, []tea.Msg{keyMsg("v"), keyMsg("j"), keyMsg("j"), altM}, "one\ntwo\nthree\n", "# Src\n\n", "Moved 3 lines to Work/Standup notes.md"},
		{"palette", true, 4, []tea.Msg{msgs.MoveLinesToNoteMsg{}}, "three\n", "# Src\n\none\ntwo\n", "Moved 1 line to Work/Standup notes.md"},
		{"plain cursor line", false, 2, []tea.Msg{altM}, "one\n", "# Src\n\ntwo\nthree\n", "Moved 1 line to Work/Standup notes.md"},
		{"plain selection", false, 2, []tea.Msg{shiftDown, shiftDown, altM}, "one\ntwo\n", "# Src\n\nthree\n", "Moved 2 lines to Work/Standup notes.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, opts := moveLinesSetup(t, tt.vim, tt.line)
			for _, k := range tt.keys {
				run(t, m, k)
			}
			if !pickerOpen(m) {
				t.Fatalf("the note picker did not open:\n%s", screen(m))
			}
			typeQuery(t, m, "src")
			if v := m.topOverlay().finder.View(); !strings.Contains(v, "0 results") {
				t.Errorf("the open note is offered as a target:\n%s", v)
			}
			for range len("src") {
				run(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
			}
			typeQuery(t, m, "standup")
			run(t, m, keyMsg("enter"))
			if m.overlayOpen() {
				t.Error("the picker is still open")
			}
			if got := readFile(t, opts.Vault, targetPath); got != targetNote+tt.wantMoved {
				t.Errorf("target = %q, want %q", got, targetNote+tt.wantMoved)
			}
			if got := m.editor.Content(); got != tt.wantLeft {
				t.Errorf("source buffer = %q, want %q", got, tt.wantLeft)
			}
			if !m.editor.Dirty() {
				t.Error("source buffer not dirty: autosave will not write the deletion")
			}
			if !hasToast(m, msgs.ToastInfo, tt.wantToast) {
				t.Errorf("toasts = %v, want %q", toastTexts(m), tt.wantToast)
			}
			if tt.vim && m.editor.ModeName() != "NORMAL" {
				t.Errorf("mode = %s, want NORMAL", m.editor.ModeName())
			}
			if n, ok := m.ix.Get(targetPath); !ok || !strings.Contains(n.Content, strings.TrimSuffix(tt.wantMoved, "\n")) {
				t.Error("target not re-indexed")
			}
		})
	}
}

// Undo restores the lines in the source; the target keeps its copy.
func TestMoveLinesUndo(t *testing.T) {
	for _, vimMode := range []bool{true, false} {
		m, opts := moveLinesSetup(t, vimMode, 3)
		run(t, m, altM)
		run(t, m, finder.PickedMsg{Path: targetPath})
		if m.editor.Content() == srcNote {
			t.Fatal("line not moved")
		}
		if vimMode {
			run(t, m, keyMsg("u"))
		} else {
			run(t, m, keyMsg("ctrl+z"))
		}
		if got := m.editor.Content(); got != srcNote {
			t.Errorf("vim=%v: after undo source = %q, want %q", vimMode, got, srcNote)
		}
		if got := readFile(t, opts.Vault, targetPath); got != targetNote+"two\n" {
			t.Errorf("vim=%v: target = %q", vimMode, got)
		}
	}
}

func TestMoveLinesCancelled(t *testing.T) {
	m, opts := moveLinesSetup(t, true, 3)
	run(t, m, altM)
	run(t, m, keyMsg("esc"))
	if m.overlayOpen() {
		t.Fatal("esc did not close the picker")
	}
	// A pick arriving after the cancel changes nothing.
	run(t, m, finder.PickedMsg{Path: targetPath})
	if got := m.editor.Content(); got != srcNote {
		t.Errorf("source = %q", got)
	}
	if got := readFile(t, opts.Vault, targetPath); got != targetNote {
		t.Errorf("target = %q", got)
	}
}

func TestMoveLinesAppendFails(t *testing.T) {
	m, opts := moveLinesSetup(t, true, 3)
	run(t, m, altM)
	if err := os.Remove(opts.Vault.Abs(targetPath)); err != nil {
		t.Fatal(err)
	}
	run(t, m, finder.PickedMsg{Path: targetPath})
	if got := m.editor.Content(); got != srcNote {
		t.Errorf("source changed after a failed append: %q", got)
	}
	if !hasToast(m, msgs.ToastError, "Could not move") {
		t.Errorf("toasts = %v, want an error", toastTexts(m))
	}
	if exists(opts.Vault, targetPath) {
		t.Error("the target was recreated")
	}
}

func TestMoveLinesRefusals(t *testing.T) {
	t.Run("conflicted source", func(t *testing.T) {
		for _, trigger := range []tea.Msg{altM, msgs.MoveLinesToNoteMsg{}} {
			m, _ := moveLinesSetup(t, true, 3)
			m.setConflicted(map[string]bool{"src.md": true})
			m.syncReadOnly()
			run(t, m, trigger)
			if m.overlayOpen() {
				t.Errorf("%T: picker opened on a conflicted note", trigger)
			}
			if !hasToast(m, msgs.ToastWarn, "'src' has a sync conflict") {
				t.Errorf("%T: toasts = %v", trigger, toastTexts(m))
			}
		}
	})
	t.Run("conflicted target", func(t *testing.T) {
		m, opts := moveLinesSetup(t, true, 3)
		run(t, m, altM)
		m.setConflicted(map[string]bool{targetPath: true})
		run(t, m, finder.PickedMsg{Path: targetPath})
		if !hasToast(m, msgs.ToastWarn, "'Standup notes' has a sync conflict") {
			t.Errorf("toasts = %v", toastTexts(m))
		}
		if got := readFile(t, opts.Vault, targetPath); got != targetNote {
			t.Errorf("conflicted target written: %q", got)
		}
		if m.editor.Content() != srcNote {
			t.Errorf("source changed: %q", m.editor.Content())
		}
	})
	t.Run("the open note as target", func(t *testing.T) {
		m, opts := moveLinesSetup(t, true, 3)
		run(t, m, altM)
		run(t, m, finder.PickedMsg{Path: "src.md"})
		if m.editor.Content() != srcNote || readFile(t, opts.Vault, "src.md") != srcNote {
			t.Error("lines moved onto their own note")
		}
	})
	t.Run("no note open", func(t *testing.T) {
		m := start(t, testOptions(t), 120, 30)
		run(t, m, msgs.MoveLinesToNoteMsg{})
		if m.overlayOpen() || !hasToast(m, msgs.ToastInfo, "No note open") {
			t.Errorf("overlay %v, toasts %v", m.overlayOpen(), toastTexts(m))
		}
	})
	t.Run("preview view", func(t *testing.T) {
		m, _ := moveLinesSetup(t, true, 3)
		run(t, m, keyMsg("ctrl+g"))
		run(t, m, keyMsg("ctrl+g"))
		run(t, m, msgs.MoveLinesToNoteMsg{})
		if m.overlayOpen() || !hasToast(m, msgs.ToastInfo, "No note open") {
			t.Errorf("overlay %v, toasts %v", m.overlayOpen(), toastTexts(m))
		}
	})
}

// alt+m belongs to the terminal and the text in insert mode, and to a
// pending vim command.
func TestMoveLinesKeyNotTaken(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{"insert mode", []string{"i"}},
		{"pending operator", []string{"d"}},
		{"command line", []string{":"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := moveLinesSetup(t, true, 3)
			pressKeys(t, m, tt.keys...)
			run(t, m, altM)
			if m.overlayOpen() {
				t.Error("alt+m opened the picker")
			}
		})
	}
}

// During a sync merge the append waits for the merge to end, like every
// other file change.
func TestMoveLinesDuringMerge(t *testing.T) {
	m, opts := moveLinesSetup(t, true, 3)
	run(t, m, altM)
	run(t, m, hostMsg{msg: lockMutationsMsg{}})
	run(t, m, finder.PickedMsg{Path: targetPath})
	if got := readFile(t, opts.Vault, targetPath); got != targetNote {
		t.Fatalf("target written during the merge: %q", got)
	}
	run(t, m, hostMsg{msg: unlockMutationsMsg{}})
	if got := readFile(t, opts.Vault, targetPath); got != targetNote+"two\n" {
		t.Errorf("target = %q after the merge", got)
	}
	if got := m.editor.Content(); got != "# Src\n\none\nthree\n" {
		t.Errorf("source = %q after the merge", got)
	}
}

// Lines that changed between the trigger and the append are copied but
// not deleted (amendment A7).
func TestMoveLinesStaleBuffer(t *testing.T) {
	m, opts := moveLinesSetup(t, true, 3)
	run(t, m, altM)
	run(t, m, hostMsg{msg: lockMutationsMsg{}})
	run(t, m, finder.PickedMsg{Path: targetPath})
	pressKeys(t, m, "A", "!", "esc") // queued by the locked editor
	run(t, m, hostMsg{msg: unlockMutationsMsg{}})
	if got := readFile(t, opts.Vault, targetPath); got != targetNote+"two\n" {
		t.Errorf("target = %q, want the lines as captured", got)
	}
	if got := m.editor.Content(); got != "# Src\n\none\ntwo!\nthree\n" {
		t.Errorf("source = %q, want the changed line kept", got)
	}
	if !hasToast(m, msgs.ToastWarn, "Copied to Work/Standup notes.md; left here because the note changed") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}

func TestMoveLinesPaletteCommand(t *testing.T) {
	m, _ := moveLinesSetup(t, true, 3)
	run(t, m, keyMsg("ctrl+k"))
	typeQuery(t, m, "move line")
	if !strings.Contains(screen(m), "Move line to note") {
		t.Fatalf("palette misses the command:\n%s", screen(m))
	}
	run(t, m, keyMsg("enter"))
	if !pickerOpen(m) {
		t.Errorf("the palette command did not open the picker:\n%s", screen(m))
	}
}
