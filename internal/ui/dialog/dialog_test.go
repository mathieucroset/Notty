package dialog

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

func key(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: s, Code: rune(s[0])})
}

// typeText sends s as a sequence of individual key presses, as a real
// terminal would.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = m.Update(key(string(r)))
	}
	return m
}

func namedKey(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	case "tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
	case "shift+tab":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	case "left":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft})
	case "right":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyRight})
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "backspace":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace})
	case "ctrl+j":
		return tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModCtrl})
	case "ctrl+k":
		return tea.KeyPressMsg(tea.Key{Code: 'k', Mod: tea.ModCtrl})
	}
	panic("unknown named key: " + name)
}

func strip(s string) string { return ansi.Strip(s) }

func requireNonEmptyView(t *testing.T, m Model) {
	t.Helper()
	if strip(m.View()) == "" {
		t.Fatal("expected non-empty view")
	}
}

func TestInputValidationBlocksEnterAndShowsError(t *testing.T) {
	styles := testStyles(t)
	validate := func(v string) error {
		if v == "" {
			return errors.New("name is required")
		}
		return nil
	}
	m := NewInput("new-note", "New note", "note name", "", validate, styles)

	// Enter with an invalid (empty) value: blocked, no ResultMsg, error shown.
	m2, cmd := m.Update(namedKey("enter"))
	if cmd != nil {
		t.Fatal("expected enter to be blocked (no command) when validation fails")
	}
	if !strings.Contains(strip(m2.View()), "name is required") {
		t.Fatalf("expected validation error in view, got:\n%s", strip(m2.View()))
	}

	// Fix it: type a valid name, enter should now confirm.
	m3 := typeText(t, m2, "todo")
	m4, cmd := m3.Update(namedKey("enter"))
	if cmd == nil {
		t.Fatal("expected enter to confirm once input is valid")
	}
	msg := cmd()
	res, ok := msg.(ResultMsg)
	if !ok {
		t.Fatalf("expected ResultMsg, got %T", msg)
	}
	if !res.OK || res.Value != "todo" || res.ID != "new-note" {
		t.Fatalf("unexpected result: %+v", res)
	}
	requireNonEmptyView(t, m4)
}

func TestInputEscCancels(t *testing.T) {
	m := NewInput("id", "Title", "", "", nil, testStyles(t))
	_, cmd := m.Update(namedKey("esc"))
	if cmd == nil {
		t.Fatal("expected esc to emit a result")
	}
	res := cmd().(ResultMsg)
	if res.OK {
		t.Fatal("expected OK=false on esc")
	}
}

func TestInputSuggestionsTabCompletes(t *testing.T) {
	suggest := func(prefix string) []string {
		all := []string{"Work/Standup notes.md", "Work/ideas.md", "Personal/journal.md"}
		var out []string
		for _, s := range all {
			if strings.HasPrefix(s, prefix) {
				out = append(out, s)
			}
		}
		return out
	}
	m := NewInput("move", "Move to…", "folder", "", nil, testStyles(t)).WithSuggestions(suggest)

	m = typeText(t, m, "Work/")
	view := strip(m.View())
	if !strings.Contains(view, "Work/Standup notes.md") || !strings.Contains(view, "Work/ideas.md") {
		t.Fatalf("expected suggestions in view, got:\n%s", view)
	}

	// Move the highlight down, then tab-complete it.
	m, _ = m.Update(namedKey("down"))
	m, _ = m.Update(namedKey("tab"))
	if got := m.contentWidth(); got <= 0 {
		t.Fatal("expected non-zero content width")
	}
	if !strings.Contains(strip(m.View()), "Work/ideas.md") {
		t.Fatalf("expected the completed value in view, got:\n%s", strip(m.View()))
	}
}

func TestConfirmYesNoEscEnter(t *testing.T) {
	styles := testStyles(t)

	m := NewConfirm("trash", "Delete note?", "This moves the note to trash.", "Delete", "Cancel", true, styles)
	_, cmd := m.Update(key("y"))
	res := cmd().(ResultMsg)
	if !res.OK || res.ID != "trash" {
		t.Fatalf("expected y to confirm, got %+v", res)
	}

	m2 := NewConfirm("trash", "Delete note?", "msg", "Delete", "Cancel", true, styles)
	_, cmd = m2.Update(key("n"))
	res = cmd().(ResultMsg)
	if res.OK {
		t.Fatalf("expected n to cancel, got %+v", res)
	}

	m3 := NewConfirm("trash", "Delete note?", "msg", "Delete", "Cancel", true, styles)
	_, cmd = m3.Update(namedKey("esc"))
	res = cmd().(ResultMsg)
	if res.OK {
		t.Fatalf("expected esc to cancel, got %+v", res)
	}

	// Danger confirms default to the safe (No) button; enter should cancel.
	m4 := NewConfirm("trash", "Delete note?", "msg", "Delete", "Cancel", true, styles)
	_, cmd = m4.Update(namedKey("enter"))
	res = cmd().(ResultMsg)
	if res.OK {
		t.Fatalf("expected enter on a fresh danger confirm to cancel (No is the default), got %+v", res)
	}

	// Toggling with tab then enter should confirm.
	m5, _ := m4.Update(namedKey("tab"))
	_, cmd = m5.Update(namedKey("enter"))
	res = cmd().(ResultMsg)
	if !res.OK {
		t.Fatalf("expected enter after tab to confirm, got %+v", res)
	}

	// Non-danger confirms default to Yes.
	m6 := NewConfirm("save", "Save changes?", "msg", "Save", "Discard", false, styles)
	_, cmd = m6.Update(namedKey("enter"))
	res = cmd().(ResultMsg)
	if !res.OK {
		t.Fatalf("expected enter on a fresh non-danger confirm to confirm, got %+v", res)
	}
}

func TestChoiceDigitsAndEnter(t *testing.T) {
	styles := testStyles(t)
	options := []string{"Reload from disk", "Keep mine"}

	m := NewChoice("reload-or-keep", "File changed on disk", "Someone else changed this note.", options, styles)
	_, cmd := m.Update(key("2"))
	res := cmd().(ResultMsg)
	if !res.OK || res.Choice != 1 || res.ID != "reload-or-keep" {
		t.Fatalf("expected digit 2 to choose index 1, got %+v", res)
	}

	m2 := NewChoice("id", "Title", "msg", options, styles)
	m2, _ = m2.Update(namedKey("down"))
	_, cmd = m2.Update(namedKey("enter"))
	res = cmd().(ResultMsg)
	if !res.OK || res.Choice != 1 {
		t.Fatalf("expected down+enter to choose index 1, got %+v", res)
	}

	m3 := NewChoice("id", "Title", "msg", options, styles)
	_, cmd = m3.Update(namedKey("esc"))
	res = cmd().(ResultMsg)
	if res.OK {
		t.Fatalf("expected esc to cancel, got %+v", res)
	}

	// A digit beyond the option count is ignored.
	m4 := NewChoice("id", "Title", "msg", options, styles)
	_, cmd = m4.Update(key("9"))
	if cmd != nil {
		t.Fatalf("expected out-of-range digit to be ignored, got a command")
	}
}

func TestWidthBounds(t *testing.T) {
	styles := testStyles(t)

	short := NewInput("id", "Hi", "x", "", nil, styles)
	if w := short.Width(); w != minWidth {
		t.Errorf("expected minimum width %d for short content, got %d", minWidth, w)
	}

	long := NewChoice("id", strings.Repeat("Very long title that keeps going ", 5), "msg", []string{"a"}, styles)
	if w := long.Width(); w != maxWidth {
		t.Errorf("expected maximum width %d for long content, got %d", maxWidth, w)
	}
}

func TestViewSnapshotInput(t *testing.T) {
	styles := testStyles(t)
	m := NewInput("new-note", "New note", "note name", "", nil, styles)
	got := strip(m.View())
	if !strings.Contains(got, "New note") {
		t.Fatalf("expected title in view, got:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	for _, l := range lines {
		if w := ansi.StringWidth(l); w != m.Width() {
			t.Fatalf("expected every line to be %d wide, line %q was %d", m.Width(), l, w)
		}
	}
}

func TestViewSnapshotConfirm(t *testing.T) {
	styles := testStyles(t)
	m := NewConfirm("trash", "Delete note?", "This moves the note to trash.", "Delete", "Cancel", true, styles)
	got := strip(m.View())
	for _, want := range []string{"Delete note?", "This moves the note to trash.", "Delete", "Cancel"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in view, got:\n%s", want, got)
		}
	}
}
