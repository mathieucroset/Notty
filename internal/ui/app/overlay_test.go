package app

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestMain(m *testing.M) {
	// Info and warning toasts expire through a 4s tick, which would stall
	// the synchronous command loop in run.
	toastTimer = func(tea.Cmd) tea.Cmd { return nil }
	os.Exit(m.Run())
}

// assertSize fails unless the screen is exactly w×h cells.
func assertSize(t *testing.T, m *Model, w, h int) {
	t.Helper()
	lines := strings.Split(screen(m), "\n")
	if len(lines) != h {
		t.Errorf("screen has %d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("line %d has width %d, want %d", i, got, w)
		}
	}
}

func hasMsg[T any](ms []tea.Msg) bool {
	for _, m := range ms {
		if _, ok := m.(T); ok {
			return true
		}
	}
	return false
}

func TestToastDrawnBottomRight(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.ToastMsg{Level: msgs.ToastError, Text: "Disk on fire"})
	lines := strings.Split(screen(m), "\n")
	row := -1
	for i, l := range lines {
		if strings.Contains(l, "Disk on fire") {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("toast missing:\n%s", screen(m))
	}
	if row < 20 || row > 28 {
		t.Errorf("toast on row %d, want it at the bottom above the status bar", row)
	}
	if col := strings.Index(lines[row], "Disk on fire"); ansi.StringWidth(lines[row][:col]) < 60 {
		t.Errorf("toast not on the right:\n%s", lines[row])
	}
	if !strings.Contains(lines[29], "F1 help") {
		t.Errorf("status bar covered: %q", lines[29])
	}
	assertSize(t, m, 120, 30)
}

func TestEscDismissesStickyErrorBeforeClearingFilter(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.ToastMsg{Level: msgs.ToastError, Text: "Disk on fire"})
	got := run(t, m, keyMsg("esc"))
	if hasMsg[msgs.ClearFilterMsg](got) {
		t.Error("esc cleared the filter while an error toast was shown")
	}
	if strings.Contains(screen(m), "Disk on fire") {
		t.Errorf("esc did not dismiss the error toast:\n%s", screen(m))
	}
	if got := run(t, m, keyMsg("esc")); !hasMsg[msgs.ClearFilterMsg](got) {
		t.Errorf("second esc produced %#v, want ClearFilterMsg", got)
	}
}

func TestInfoToastLogged(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.ToastMsg{Level: msgs.ToastInfo, Text: "Saved"})
	if !strings.Contains(screen(m), "Saved") {
		t.Errorf("info toast missing:\n%s", screen(m))
	}
	if log := m.toast.Log(); len(log) != 1 || log[0].Text != "Saved" {
		t.Errorf("toast log = %#v", log)
	}
}

func TestDialogOverlayGetsKeys(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	m.openDialog(dialog.NewInput("test", "Test dialog", "type here", "", nil, opts.Styles), pendingOp{})

	if got := m.keyContext(); got != keys.Overlay {
		t.Errorf("keyContext = %v, want Overlay", got)
	}
	s := screen(m)
	if !strings.Contains(s, "Test dialog") || !strings.Contains(s, "NOTES") {
		t.Errorf("dialog not drawn over the screen:\n%s", s)
	}
	assertSize(t, m, 120, 30)

	// Keys go to the dialog: q types, ctrl+b does not toggle the sidebar.
	if hasQuit(run(t, m, keyMsg("q"))) {
		t.Error("q quit while a dialog was open")
	}
	run(t, m, keyMsg("ctrl+b"))
	if !m.SidebarVisible() {
		t.Error("ctrl+b toggled the sidebar under a dialog")
	}
	// ctrl+q still passes through.
	if !hasQuit(run(t, m, keyMsg("ctrl+q"))) {
		t.Error("ctrl+q did not quit under a dialog")
	}

	run(t, m, keyMsg("esc"))
	if m.overlayOpen() {
		t.Error("esc did not close the dialog")
	}
	if strings.Contains(screen(m), "Test dialog") {
		t.Errorf("closed dialog still drawn:\n%s", screen(m))
	}
}

func TestStaleDialogResultDropped(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	m.openDialog(dialog.NewInput("current", "Current", "", "", nil, opts.Styles), pendingOp{})
	run(t, m, dialog.ResultMsg{ID: "other", OK: true})
	if !m.overlayOpen() {
		t.Error("a result from another dialog closed the open one")
	}
}
