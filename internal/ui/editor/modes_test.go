package editor

import (
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestReadOnly(t *testing.T) {
	for _, vimMode := range []bool{true, false} {
		name := "plain"
		if vimMode {
			name = "vim"
		}
		t.Run(name, func(t *testing.T) {
			opts := testOptions(t)
			opts.Vim = vimMode
			m := newModel(t, opts, "conflicted text", buffer.Pos{}, 70, 5).SetReadOnly(true, "")
			if m.ModeName() != "READ-ONLY" {
				t.Errorf("ModeName = %q", m.ModeName())
			}
			if m.CursorPosition() != nil {
				t.Error("read-only editor shows a cursor")
			}
			rows := plainView(m)
			if want := "⚠ " + DefaultBanner; strings.TrimSpace(rows[0]) != want {
				t.Errorf("banner row = %q, want %q", rows[0], want)
			}

			_, out := typeKeys(m, "c")
			if r, ok := find[msgs.OpenResolverMsg](out); !ok || r.Path != "notes/test.md" {
				t.Errorf("c: messages = %#v, want OpenResolverMsg", out)
			}

			normal := m.View()
			edit := []string{"x"}
			if !vimMode {
				edit = []string{"z"}
			}
			m2, out := typeKeys(m, edit...)
			if m2.Content() != "conflicted text" {
				t.Errorf("edit applied in read-only mode: %q", m2.Content())
			}
			if !m2.flashing || m2.View() == normal {
				t.Error("blocked edit does not flash the banner")
			}
			var end flashEndMsg
			for _, msg := range out {
				if f, ok := msg.(flashEndMsg); ok {
					end = f
				}
			}
			m2, _ = m2.Update(end)
			if m2.flashing || m2.View() != normal {
				t.Error("flash does not end")
			}
		})
	}
}

func TestReadOnlyPendingCIsNotResolver(t *testing.T) {
	m := newModel(t, testOptions(t), "abc", buffer.Pos{}, 40, 5).SetReadOnly(true, "")
	m, out := typeKeys(m, "f", "c")
	if _, ok := find[msgs.OpenResolverMsg](out); ok {
		t.Error("fc opened the resolver")
	}
	if got := m.Cursor(); got.Col != 2 {
		t.Errorf("fc moved to %+v", got)
	}
}

func TestReadOnlyMotionsWork(t *testing.T) {
	m := newModel(t, testOptions(t), "a\nb\nc", buffer.Pos{}, 40, 5).SetReadOnly(true, "")
	m, _ = typeKeys(m, "j")
	if m.CursorLine() != 1 {
		t.Errorf("j in read-only: line %d", m.CursorLine())
	}
}

func TestLockQueueReplay(t *testing.T) {
	m := newModel(t, testOptions(t), "", buffer.Pos{}, 40, 5).SetLocked(true)
	m, _ = typeKeys(m, "i", "a", "b", "esc")
	if m.Content() != "" {
		t.Fatalf("locked editor applied keys: %q", m.Content())
	}
	m, cmd, dropped := m.Unlock(true)
	if dropped != 0 {
		t.Errorf("dropped = %d on replay", dropped)
	}
	if got := m.Content(); got != "ab" {
		t.Errorf("replayed content = %q, want %q", got, "ab")
	}
	if _, ok := find[ChangedMsg](collect(cmd)); !ok {
		t.Error("replay does not report the change")
	}
}

func TestLockQueueDrop(t *testing.T) {
	m := newModel(t, testOptions(t), "text", buffer.Pos{}, 40, 5).SetLocked(true)
	m, _ = typeKeys(m, "d", "d", "x")
	m, _, dropped := m.Unlock(false)
	if dropped != 3 {
		t.Errorf("dropped = %d, want 3", dropped)
	}
	if m.Content() != "text" {
		t.Errorf("content = %q", m.Content())
	}
	m, _ = typeKeys(m, "x")
	if m.Content() != "ext" {
		t.Errorf("keys after unlock not applied: %q", m.Content())
	}
}

func TestAutosave(t *testing.T) {
	m := newModel(t, testOptions(t), "text", buffer.Pos{}, 40, 5)
	m, out := typeKeys(m, "x")
	tick, ok := find[AutosaveTickMsg](out)
	if !ok || tick.Version != m.Version() {
		t.Fatalf("no autosave tick for the current version: %#v", out)
	}
	_, cmd := m.Update(tick)
	if _, ok := find[msgs.SaveRequestMsg](collect(cmd)); !ok {
		t.Error("matching tick does not request a save")
	}

	// A newer change makes the old tick stale.
	m2, _ := typeKeys(m, "x")
	if _, cmd := m2.Update(tick); cmd != nil {
		t.Error("stale tick requested a save")
	}
	// A clean buffer is not saved.
	clean := m.MarkSaved(m.Version())
	if _, cmd := clean.Update(tick); cmd != nil {
		t.Error("clean buffer saved")
	}
	// Read-only and locked editors never autosave.
	if _, cmd := m.SetReadOnly(true, "").Update(tick); cmd != nil {
		t.Error("read-only buffer autosaved")
	}
	if _, cmd := m.SetLocked(true).Update(tick); cmd != nil {
		t.Error("locked buffer autosaved")
	}
}

func TestMarkSaved(t *testing.T) {
	m := newModel(t, testOptions(t), "text", buffer.Pos{}, 40, 5)
	m, _ = typeKeys(m, "x")
	v := m.Version()
	m, _ = typeKeys(m, "x") // typed while the save was running
	m = m.MarkSaved(v)
	if !m.Dirty() {
		t.Error("stale MarkSaved cleaned the buffer")
	}
	m = m.MarkSaved(m.Version())
	if m.Dirty() {
		t.Error("MarkSaved with the current version left the buffer dirty")
	}
}

func TestApplyToggle(t *testing.T) {
	doc := "# Tasks\n- [ ] one\n- [ ] two"
	tests := []struct {
		name string
		line int
		text string
		ok   bool
		want string
	}{
		{"exact", 2, "- [ ] two", true, "# Tasks\n- [ ] one\n- [x] two"},
		{"drifted line", 0, "- [ ] two", true, "# Tasks\n- [ ] one\n- [x] two"},
		{"not found", 1, "- [ ] three", false, doc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, testOptions(t), doc, buffer.Pos{Line: 1, Col: 3}, 40, 5)
			m, ok := m.ApplyToggle(tt.line, tt.text)
			if ok != tt.ok || m.Content() != tt.want {
				t.Fatalf("ApplyToggle = %v, %q; want %v, %q", ok, m.Content(), tt.ok, tt.want)
			}
			if !ok {
				return
			}
			if m.Cursor() != (buffer.Pos{Line: 1, Col: 3}) {
				t.Errorf("cursor moved to %+v", m.Cursor())
			}
			if rows := plainView(m); strings.TrimSpace(rows[2]) != "☑ two" {
				t.Errorf("rendering not updated: %q", rows[2])
			}
			m, _ = typeKeys(m, "u")
			if m.Content() != doc {
				t.Errorf("one undo gives %q", m.Content())
			}
		})
	}
	ro := newModel(t, testOptions(t), doc, buffer.Pos{}, 40, 5).SetReadOnly(true, "")
	if _, ok := ro.ApplyToggle(1, "- [ ] one"); ok {
		t.Error("toggle applied to a read-only note")
	}
}

func TestInsertText(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		cur  buffer.Pos
		want string
		line int
	}{
		{"blank line replaced", "a\n\nb", buffer.Pos{Line: 1}, "a\n![](/attachments/x.png)\nb", 1},
		{"new line below", "a\nb", buffer.Pos{Line: 0, Col: 0}, "a\n![](/attachments/x.png)\nb", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, testOptions(t), tt.doc, tt.cur, 40, 5)
			m = m.InsertText("![](/attachments/x.png)")
			if m.Content() != tt.want || m.CursorLine() != tt.line {
				t.Errorf("got %q line %d, want %q line %d", m.Content(), m.CursorLine(), tt.want, tt.line)
			}
			m, _ = typeKeys(m, "u")
			if m.Content() != tt.doc {
				t.Errorf("undo gives %q", m.Content())
			}
		})
	}
}

func TestReload(t *testing.T) {
	m := newModel(t, testOptions(t), "a\nb\nc\nd", buffer.Pos{Line: 2, Col: 0}, 40, 5)
	m, _ = typeKeys(m, "x")
	m = m.Reload("a\nB\nc\nd\ne")
	if m.Content() != "a\nB\nc\nd\ne" || m.Dirty() || m.CursorLine() != 2 {
		t.Errorf("Reload: content %q dirty %v line %d", m.Content(), m.Dirty(), m.CursorLine())
	}
	if rows := plainView(m); strings.TrimSpace(rows[1]) != "B" {
		t.Errorf("view not restyled after reload: %q", rows)
	}
	m = m.Reload("only")
	if m.CursorLine() != 0 {
		t.Errorf("cursor not clamped: %d", m.CursorLine())
	}
}

func TestLoadReplacesBuffer(t *testing.T) {
	m := newModel(t, testOptions(t), "first", buffer.Pos{}, 40, 5)
	m, _ = typeKeys(m, "x")
	m = m.Load("other.md", "second", buffer.Pos{Col: 3})
	if m.Path() != "other.md" || m.Content() != "second" || m.Dirty() || m.Cursor().Col != 3 {
		t.Errorf("Load: %q %q dirty=%v cursor=%+v", m.Path(), m.Content(), m.Dirty(), m.Cursor())
	}
	m, _ = typeKeys(m, "u")
	if m.Content() != "second" {
		t.Errorf("undo crossed notes: %q", m.Content())
	}
}
