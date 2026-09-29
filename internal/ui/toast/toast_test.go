package toast

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func testStyles(t *testing.T) theme.Styles {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	return theme.NewStyles(p)
}

func strip(s string) string { return ansi.Strip(s) }

// TestPushInfoExpiresViaTick verifies that Push returns an expiry command
// for the toast (carrying its id) and that Update removes exactly that
// toast on the resulting expireMsg. It calls Update with the expire message
// directly rather than actually waiting out the real tea.Tick timer.
func TestPushInfoExpiresViaTick(t *testing.T) {
	m := New(testStyles(t))

	m, cmd := m.Push(msgs.ToastInfo, "saved")
	if !m.Visible() {
		t.Fatal("expected the toast to be visible right after Push")
	}
	if cmd == nil {
		t.Fatal("expected Push to return an expiry command for an info toast")
	}
	if len(m.active) != 1 {
		t.Fatalf("expected 1 active toast, got %d", len(m.active))
	}

	id := m.active[0].id
	m, _ = m.Update(expireMsg{id: id})
	if m.Visible() {
		t.Fatal("expected the toast to be gone after its expire message")
	}
	if len(m.Log()) != 1 {
		t.Fatalf("expected the expired toast to remain in the log, got %d entries", len(m.Log()))
	}
}

func TestPushWarnExpiresViaTick(t *testing.T) {
	m := New(testStyles(t))
	m, cmd := m.Push(msgs.ToastWarn, "check this")
	if cmd == nil {
		t.Fatal("expected a warn toast to expire too")
	}
	id := m.active[0].id
	m, _ = m.Update(expireMsg{id: id})
	if m.Visible() {
		t.Fatal("expected the warn toast to be gone after it expires")
	}
}

// TestExpireOnlyRemovesMatchingID checks that expiring one toast leaves
// others (including a later one pushed with the same numeric slot cleared)
// untouched.
func TestExpireOnlyRemovesMatchingID(t *testing.T) {
	m := New(testStyles(t))
	m, _ = m.Push(msgs.ToastInfo, "first")
	m, _ = m.Push(msgs.ToastInfo, "second")
	firstID := m.active[0].id

	m, _ = m.Update(expireMsg{id: firstID})
	if !m.Visible() {
		t.Fatal("expected the second toast to still be visible")
	}
	view := strip(m.View(80))
	if strings.Contains(view, "first") {
		t.Fatal("expected the first toast to be gone")
	}
	if !strings.Contains(view, "second") {
		t.Fatal("expected the second toast to remain")
	}
}

func TestPushErrorIsSticky(t *testing.T) {
	m := New(testStyles(t))
	m, cmd := m.Push(msgs.ToastError, "sync failed")
	if cmd != nil {
		t.Fatal("expected an error toast to have no expiry command")
	}
	if !m.Visible() {
		t.Fatal("expected the error toast to be visible")
	}

	m = m.Dismiss()
	if m.Visible() {
		t.Fatal("expected Dismiss to remove the sticky error")
	}
	if len(m.Log()) != 1 {
		t.Fatal("expected the dismissed error to remain in the log")
	}
}

func TestDismissRemovesNewestError(t *testing.T) {
	m := New(testStyles(t))
	m, _ = m.Push(msgs.ToastError, "first error")
	m, _ = m.Push(msgs.ToastError, "second error")

	m = m.Dismiss()
	view := strip(m.View(80))
	if strings.Contains(view, "second error") {
		t.Fatal("expected the newest error to be dismissed first")
	}
	if !strings.Contains(view, "first error") {
		t.Fatalf("expected the older error to remain visible, got:\n%s", view)
	}
}

func TestMaxThreeVisible(t *testing.T) {
	m := New(testStyles(t))
	for i := 0; i < 5; i++ {
		m, _ = m.Push(msgs.ToastError, "error "+string(rune('A'+i)))
	}
	view := strip(m.View(80))
	for _, want := range []string{"error C", "error D", "error E"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q to be visible, got:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"error A", "error B"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("expected %q to be dropped from view, got:\n%s", unwanted, view)
		}
	}
	// But every push is still logged.
	if len(m.Log()) != 5 {
		t.Fatalf("expected 5 log entries, got %d", len(m.Log()))
	}
}

func TestLogCappedAt100NewestFirst(t *testing.T) {
	m := New(testStyles(t))
	for i := 0; i < 150; i++ {
		m, _ = m.Push(msgs.ToastInfo, "n"+string(rune('0'+i%10)))
	}
	log := m.Log()
	if len(log) != 100 {
		t.Fatalf("expected the log capped at 100, got %d", len(log))
	}
	// The very last push (i=149 -> "n9") should be first (newest first).
	if log[0].Text != "n9" {
		t.Fatalf("expected the newest entry first, got %q", log[0].Text)
	}
}

func TestViewSnapshotToastStack(t *testing.T) {
	m := New(testStyles(t))
	m, _ = m.Push(msgs.ToastInfo, "Note saved")
	m, _ = m.Push(msgs.ToastWarn, "Sync is behind")
	m, _ = m.Push(msgs.ToastError, "Sync failed: network unreachable")

	view := strip(m.View(48))
	for _, want := range []string{"ℹ", "Note saved", "⚠", "Sync is behind", "✗", "Sync failed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in the toast stack view, got:\n%s", want, view)
		}
	}
	for _, l := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(l); w > 48 {
			t.Fatalf("expected no line wider than 48, got %d: %q", w, l)
		}
	}
}

func TestNotVisibleWhenEmpty(t *testing.T) {
	m := New(testStyles(t))
	if m.Visible() {
		t.Fatal("expected a fresh toast stack to be invisible")
	}
	if m.View(80) != "" {
		t.Fatalf("expected an empty view, got %q", m.View(80))
	}
}
