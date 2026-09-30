package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// moveDialogFor reports whether the move dialog is open for p.
func moveDialogFor(m *Model, p string) bool {
	o := m.topOverlay()
	return o != nil && o.kind == overlayDialog && o.dialog.ID() == dlgMove && o.pending.kind == opMove && o.pending.path == p
}

func TestMoveOpenNoteOpensDialog(t *testing.T) {
	tests := []struct {
		name    string
		vim     bool
		keys    []string
		trigger tea.Msg
	}{
		{"alt+shift+m", true, nil, tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt | tea.ModShift}},
		{"alt+M", true, nil, tea.KeyPressMsg{Code: 'M', Mod: tea.ModAlt}},
		{"visual mode", true, []string{"v"}, tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt | tea.ModShift}},
		{"plain mode", false, nil, tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt | tea.ModShift}},
		{"palette", true, nil, msgs.MoveOpenNoteMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			opts.Config.Vim = tt.vim
			m := openNote(t, opts, "ideas.md")
			pressKeys(t, m, tt.keys...)
			run(t, m, tt.trigger)
			if !moveDialogFor(m, "ideas.md") {
				t.Fatalf("the move dialog is not open for the note:\n%s", screen(m))
			}
			if s := screen(m); !strings.Contains(s, "created") {
				t.Errorf("the dialog does not say new folders are created:\n%s", s)
			}
		})
	}
}

func TestMoveOpenNoteRefusals(t *testing.T) {
	t.Run("no note open", func(t *testing.T) {
		m := start(t, testOptions(t), 120, 30)
		run(t, m, msgs.MoveOpenNoteMsg{})
		if m.overlayOpen() || !hasToast(m, msgs.ToastInfo, "No note open") {
			t.Errorf("overlay %v, toasts %v", m.overlayOpen(), toastTexts(m))
		}
	})
	t.Run("conflicted note", func(t *testing.T) {
		for _, trigger := range []tea.Msg{msgs.MoveOpenNoteMsg{}, tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt | tea.ModShift}} {
			m := openNote(t, testOptions(t), "ideas.md")
			m.setConflicted(map[string]bool{"ideas.md": true})
			m.syncReadOnly()
			run(t, m, trigger)
			if m.overlayOpen() || !hasToast(m, msgs.ToastWarn, "'ideas' has a sync conflict") {
				t.Errorf("%T: overlay %v, toasts %v", trigger, m.overlayOpen(), toastTexts(m))
			}
		}
	})
	t.Run("insert mode", func(t *testing.T) {
		m := openNote(t, testOptions(t), "ideas.md")
		pressKeys(t, m, "i")
		run(t, m, tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt | tea.ModShift})
		if m.overlayOpen() {
			t.Error("alt+M opened the move dialog in insert mode")
		}
	})
}

// submitMove replaces the move dialog's initial folder (the entry's own)
// with folder and presses enter.
func submitMove(t *testing.T, m *Model, folder string) {
	t.Helper()
	for range len([]rune(parentOf(m.topOverlay().pending.path))) {
		run(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	typeText(t, m, folder)
	run(t, m, keyMsg("enter"))
}

func TestMoveToMissingFolder(t *testing.T) {
	tests := []struct {
		name      string
		confirm   bool
		wantPath  string
		wantFound bool
	}{
		{"confirmed", true, "Projects/Client A/ideas.md", true},
		{"declined", false, "ideas.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			m := openNote(t, opts, "ideas.md")
			run(t, m, msgs.MoveOpenNoteMsg{})
			submitMove(t, m, "Projects/Client A")
			o := m.topOverlay()
			if o == nil || o.kind != overlayDialog || o.dialog.ID() != dlgMoveConfirm {
				t.Fatalf("no confirmation for the missing folder:\n%s", screen(m))
			}
			if s := screen(m); !strings.Contains(s, `Create folder "Projects/Client A" and move "ideas" there?`) {
				t.Errorf("confirmation text:\n%s", s)
			}
			if exists(opts.Vault, "Projects") {
				t.Fatal("folder created before the confirmation")
			}
			run(t, m, dialog.ResultMsg{ID: dlgMoveConfirm, OK: tt.confirm})
			if got := exists(opts.Vault, "Projects/Client A"); got != tt.wantFound {
				t.Errorf("folder exists = %v, want %v", got, tt.wantFound)
			}
			if !exists(opts.Vault, tt.wantPath) {
				t.Errorf("note not at %s; toasts %v", tt.wantPath, toastTexts(m))
			}
			if m.NotePath() != tt.wantPath {
				t.Errorf("open note = %q, want %q", m.NotePath(), tt.wantPath)
			}
			if m.overlayOpen() {
				t.Error("a dialog is still open")
			}
		})
	}
}

// The open note's unsaved edits are saved before it moves to a new folder.
func TestMoveToMissingFolderSavesFirst(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "unsaved ")
	run(t, m, msgs.RequestMove{Path: "ideas.md"})
	submitMove(t, m, "Later")
	run(t, m, dialog.ResultMsg{ID: dlgMoveConfirm, OK: true})
	if !exists(opts.Vault, "Later/ideas.md") {
		t.Fatalf("note not moved; toasts %v", toastTexts(m))
	}
	if got := readFile(t, opts.Vault, "Later/ideas.md"); !strings.HasPrefix(got, "unsaved ") {
		t.Errorf("moved note = %q, want the unsaved edits", got)
	}
	if m.NotePath() != "Later/ideas.md" || m.editor.Dirty() {
		t.Errorf("open note %q dirty %v", m.NotePath(), m.editor.Dirty())
	}
}

// Existing folders move straight away, matched ignoring case.
func TestMoveToExistingFolder(t *testing.T) {
	for _, typed := range []string{"Work", "work", "WORK/"} {
		t.Run(typed, func(t *testing.T) {
			opts := testOptions(t)
			m := openNote(t, opts, "ideas.md")
			run(t, m, msgs.RequestMove{Path: "ideas.md"})
			run(t, m, dialog.ResultMsg{ID: dlgMove, OK: true, Value: typed})
			if m.overlayOpen() {
				t.Fatalf("a dialog opened for an existing folder:\n%s", screen(m))
			}
			if !exists(opts.Vault, "Work/ideas.md") {
				t.Fatalf("note not moved into Work; toasts %v", toastTexts(m))
			}
			if m.NotePath() != "Work/ideas.md" {
				t.Errorf("open note = %q, want Work/ideas.md", m.NotePath())
			}
		})
	}
}

// A new folder under an existing one keeps the existing folder's case.
func TestMoveToMissingSubfolderKeepsCase(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	run(t, m, msgs.RequestMove{Path: "ideas.md"})
	run(t, m, dialog.ResultMsg{ID: dlgMove, OK: true, Value: "work/Clients"})
	if s := screen(m); !strings.Contains(s, `Create folder "Work/Clients"`) {
		t.Fatalf("confirmation does not use the existing case:\n%s", s)
	}
	run(t, m, dialog.ResultMsg{ID: dlgMoveConfirm, OK: true})
	if m.NotePath() != "Work/Clients/ideas.md" || !exists(opts.Vault, "Work/Clients/ideas.md") {
		t.Errorf("open note = %q; toasts %v", m.NotePath(), toastTexts(m))
	}
}

func TestMoveRejectsInvalidFolders(t *testing.T) {
	tests := []struct {
		name, path, folder, want string
	}{
		{"parent segment", "ideas.md", "../outside", "Not a valid folder name"},
		{"dot folder", "ideas.md", ".hidden", "Not a valid folder name"},
		{"reserved", "ideas.md", "attachments", "Not a valid folder name"},
		{"empty segment", "ideas.md", "a//b", "Not a valid folder name"},
		{"forbidden character", "ideas.md", "Wo:rk", "Not a valid folder name"},
		{"a file", "Work/Standup notes.md", "ideas.md", "Not a folder"},
		{"under a file", "Work/Standup notes.md", "ideas.md/x", "Not a folder"},
		{"into itself", "Work", "Work/Sub", "A folder cannot move into itself"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			m := start(t, opts, 120, 30)
			run(t, m, msgs.RequestMove{Path: tt.path})
			submitMove(t, m, tt.folder)
			if !moveDialogFor(m, tt.path) {
				t.Fatalf("the move dialog closed on %q:\n%s", tt.folder, screen(m))
			}
			if s := screen(m); !strings.Contains(s, tt.want) {
				t.Errorf("validation error %q missing:\n%s", tt.want, s)
			}
			if !exists(opts.Vault, tt.path) {
				t.Error("the entry moved")
			}
			if exists(opts.Vault, "outside") || exists(opts.Vault, ".hidden") || exists(opts.Vault, "Work/Sub") || exists(opts.Vault, "a") {
				t.Error("a folder was created")
			}
		})
	}
}

// An invalid folder reaching runPending (not typed) is refused too.
func TestMoveInvalidResultRefused(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.RequestMove{Path: "ideas.md"})
	run(t, m, dialog.ResultMsg{ID: dlgMove, OK: true, Value: "../x"})
	if !exists(opts.Vault, "ideas.md") || m.overlayOpen() {
		t.Errorf("moved %v, overlay %v", !exists(opts.Vault, "ideas.md"), m.overlayOpen())
	}
	if !hasToast(m, msgs.ToastWarn, "Not a valid folder name") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}

func TestMoveNotePaletteCommand(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	run(t, m, keyMsg("ctrl+k"))
	typeQuery(t, m, "move note")
	if !strings.Contains(screen(m), "Move note to folder") {
		t.Fatalf("palette misses the command:\n%s", screen(m))
	}
	run(t, m, keyMsg("enter"))
	if !moveDialogFor(m, "ideas.md") {
		t.Errorf("the palette command did not open the move dialog:\n%s", screen(m))
	}
}
