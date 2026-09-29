package toast

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func key(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: s, Code: rune(s[0])})
}

func namedKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

func testEntries(n int) []Entry {
	entries := make([]Entry, n)
	for i := range entries {
		level := msgs.ToastInfo
		switch i % 3 {
		case 1:
			level = msgs.ToastWarn
		case 2:
			level = msgs.ToastError
		}
		entries[i] = Entry{
			Time:  time.Date(2026, 9, 29, 10, 0, i, 0, time.UTC),
			Level: level,
			Text:  "entry " + string(rune('A'+i)),
		}
	}
	return entries
}

func TestLogViewRendersEntries(t *testing.T) {
	v := NewLogView(testEntries(3), testStyles(t))
	got := strip(v.View(60, 10))
	for _, want := range []string{"entry A", "entry B", "entry C", "ℹ", "⚠", "✗"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in log view, got:\n%s", want, got)
		}
	}
}

func TestLogViewScrolling(t *testing.T) {
	v := NewLogView(testEntries(5), testStyles(t))

	// Scroll down twice: entry A and B should fall out of view when the
	// viewport is short.
	v, _ = v.Update(key("j"))
	v, _ = v.Update(key("j"))
	got := strip(v.View(60, 1))
	if strings.Contains(got, "entry A") {
		t.Fatalf("expected entry A to have scrolled out of view, got:\n%s", got)
	}
	if !strings.Contains(got, "entry C") {
		t.Fatalf("expected entry C to be the top visible entry, got:\n%s", got)
	}

	// Scroll back up.
	v, _ = v.Update(key("k"))
	got = strip(v.View(60, 1))
	if !strings.Contains(got, "entry B") {
		t.Fatalf("expected entry B back in view, got:\n%s", got)
	}

	// Scrolling past the end stays clamped on the last entry.
	for range 10 {
		v, _ = v.Update(key("j"))
	}
	got = strip(v.View(60, 1))
	if !strings.Contains(got, "entry E") {
		t.Fatalf("expected clamped scrolling to land on the last entry, got:\n%s", got)
	}
}

func TestLogViewEscEmitsCloseLogMsg(t *testing.T) {
	v := NewLogView(testEntries(1), testStyles(t))
	_, cmd := v.Update(namedKey(tea.KeyEscape))
	if cmd == nil {
		t.Fatal("expected esc to emit a command")
	}
	if _, ok := cmd().(CloseLogMsg); !ok {
		t.Fatalf("expected CloseLogMsg, got %T", cmd())
	}
}

func TestLogViewFixedDimensions(t *testing.T) {
	v := NewLogView(testEntries(2), testStyles(t))
	got := v.View(40, 8)
	lines := strings.Split(got, "\n")
	if len(lines) != 8 {
		t.Fatalf("expected 8 lines, got %d", len(lines))
	}
}

func TestLogViewEmpty(t *testing.T) {
	v := NewLogView(nil, testStyles(t))
	got := strip(v.View(40, 5))
	if !strings.Contains(got, "No errors") {
		t.Fatalf("expected an empty-state message, got:\n%s", got)
	}
}
