package editor

import (
	"slices"
	"testing"

	"github.com/mathieucroset/notty/internal/buffer"
)

func TestKeysIdle(t *testing.T) {
	tests := []struct {
		name string
		vim  bool
		keys []string
		want bool
	}{
		{"vim normal", true, nil, true},
		{"vim visual", true, []string{"v"}, true},
		{"vim visual line", true, []string{"V"}, true},
		{"vim insert", true, []string{"i"}, false},
		{"vim command line", true, []string{":"}, false},
		{"vim pending operator", true, []string{"d"}, false},
		{"vim pending count", true, []string{"2"}, false},
		{"vim visual pending", true, []string{"v", "2"}, false},
		{"plain", false, nil, true},
		{"plain selecting", false, []string{"shift+right"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			opts.Vim = tt.vim
			m := newModel(t, opts, "one\ntwo\nthree", buffer.Pos{}, 40, 5)
			m, _ = typeKeys(m, tt.keys...)
			if got := m.KeysIdle(); got != tt.want {
				t.Errorf("KeysIdle() = %v, want %v (mode %s)", got, tt.want, m.ModeName())
			}
		})
	}
}

func TestSelectedLines(t *testing.T) {
	const text = "zero\none\ntwo\nthree\n\nfive"
	tests := []struct {
		name      string
		vim       bool
		cur       buffer.Pos
		keys      []string
		wantStart int
		wantEnd   int
	}{
		{"vim cursor line", true, buffer.Pos{Line: 2, Col: 1}, nil, 2, 2},
		{"vim charwise one line", true, buffer.Pos{Line: 1, Col: 1}, []string{"v", "l"}, 1, 1},
		{"vim charwise across lines", true, buffer.Pos{Line: 1, Col: 2}, []string{"v", "j"}, 1, 2},
		{"vim charwise to column 0", true, buffer.Pos{Line: 1, Col: 2}, []string{"v", "j", "0"}, 1, 2},
		{"vim charwise backwards", true, buffer.Pos{Line: 3, Col: 2}, []string{"v", "k", "k"}, 1, 3},
		{"vim linewise", true, buffer.Pos{Line: 1}, []string{"V", "j"}, 1, 2},
		{"vim linewise onto an empty line", true, buffer.Pos{Line: 3}, []string{"V", "j"}, 3, 4},
		{"vim linewise last line", true, buffer.Pos{Line: 4}, []string{"V", "j"}, 4, 5},
		{"plain cursor line", false, buffer.Pos{Line: 3, Col: 2}, nil, 3, 3},
		{"plain selection", false, buffer.Pos{Line: 1, Col: 1}, []string{"shift+down", "shift+right"}, 1, 2},
		{"plain selection ending at column 0", false, buffer.Pos{Line: 1}, []string{"shift+down", "shift+down"}, 1, 2},
		{"plain selection backwards", false, buffer.Pos{Line: 3, Col: 1}, []string{"shift+up"}, 2, 3},
	}
	lines := []string{"zero", "one", "two", "three", "", "five"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			opts.Vim = tt.vim
			m := newModel(t, opts, text, tt.cur, 40, 10)
			m, _ = typeKeys(m, tt.keys...)
			start, end, got := m.SelectedLines()
			if start != tt.wantStart || end != tt.wantEnd {
				t.Fatalf("SelectedLines() = %d..%d, want %d..%d", start, end, tt.wantStart, tt.wantEnd)
			}
			if want := lines[tt.wantStart : tt.wantEnd+1]; !slices.Equal(got, want) {
				t.Errorf("text = %q, want %q", got, want)
			}
		})
	}
}

// A selection reaching an empty last line takes it: the line is wholly
// selected, having no text and no line break after it.
func TestSelectedLinesEmptyLastLine(t *testing.T) {
	tests := []struct {
		name string
		vim  bool
		keys []string
	}{
		{"vim charwise", true, []string{"v", "j"}},
		{"vim linewise", true, []string{"V", "j"}},
		{"plain selection", false, []string{"shift+down"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			opts.Vim = tt.vim
			m := newModel(t, opts, "a\nb\n\n", buffer.Pos{Line: 1}, 40, 10)
			m, _ = typeKeys(m, tt.keys...)
			start, end, text := m.SelectedLines()
			if start != 1 || end != 2 || !slices.Equal(text, []string{"b", ""}) {
				t.Errorf("SelectedLines() = %d..%d %q, want 1..2 [b \"\"]", start, end, text)
			}
		})
	}
}

func TestDeleteLines(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		start, end int
		want       string
		wantCursor int // line
	}{
		{"middle line", "a\nb\nc\n", 1, 1, "a\nc\n", 1},
		{"first lines", "a\nb\nc\n", 0, 1, "c\n", 0},
		{"last line", "a\nb\nc", 2, 2, "a\nb", 1},
		{"last lines with trailing newline", "a\nb\nc\n", 1, 2, "a\n", 0},
		{"everything", "a\nb", 0, 1, "", 0},
	}
	for _, tt := range tests {
		for _, vimMode := range []bool{true, false} {
			t.Run(tt.name, func(t *testing.T) {
				opts := testOptions(t)
				opts.Vim = vimMode
				m := newModel(t, opts, tt.text, buffer.Pos{Line: tt.start}, 40, 10)
				want := make([]string, 0, tt.end-tt.start+1)
				for i := tt.start; i <= tt.end; i++ {
					want = append(want, m.buf.Line(i))
				}
				m, cmd, ok := m.DeleteLines(tt.start, tt.end, want)
				if !ok {
					t.Fatal("DeleteLines refused")
				}
				if m.Content() != tt.want {
					t.Errorf("content = %q, want %q", m.Content(), tt.want)
				}
				if m.Cursor().Line != tt.wantCursor {
					t.Errorf("cursor line = %d, want %d", m.Cursor().Line, tt.wantCursor)
				}
				if !m.Dirty() || cmd == nil {
					t.Errorf("dirty = %v, cmd = %v; want a change reported for autosave", m.Dirty(), cmd != nil)
				}
			})
		}
	}
}

// The deletion is one undo step, and leaves visual mode.
func TestDeleteLinesUndo(t *testing.T) {
	tests := []struct {
		name string
		vim  bool
		keys []string
		undo string
	}{
		{"vim visual", true, []string{"V", "j"}, "u"},
		{"vim normal", true, nil, "u"},
		{"plain selection", false, []string{"shift+down", "shift+right"}, "ctrl+z"},
	}
	const text = "a\nb\nc\nd\n"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			opts.Vim = tt.vim
			m := newModel(t, opts, text, buffer.Pos{Line: 1}, 40, 10)
			m, _ = typeKeys(m, tt.keys...)
			start, end, lines := m.SelectedLines()
			m, _, ok := m.DeleteLines(start, end, lines)
			if !ok {
				t.Fatal("DeleteLines refused")
			}
			if _, has := m.ed.Selection(m.buf); has {
				t.Error("selection kept after the deletion")
			}
			if tt.vim && m.ModeName() != "NORMAL" {
				t.Errorf("mode = %s, want NORMAL", m.ModeName())
			}
			m, _ = typeKeys(m, tt.undo)
			if m.Content() != text {
				t.Errorf("after undo content = %q, want %q", m.Content(), text)
			}
		})
	}
}

func TestDeleteLinesRefuses(t *testing.T) {
	const text = "a\nb\nc\n"
	tests := []struct {
		name       string
		setup      func(Model) Model
		start, end int
		want       []string
	}{
		{"lines changed", nil, 1, 1, []string{"B"}},
		{"line count differs", nil, 1, 2, []string{"b"}},
		{"out of range", nil, 3, 4, []string{"x", "y"}},
		{"read-only", func(m Model) Model { return m.SetReadOnly(true, "") }, 1, 1, []string{"b"}},
		{"locked", func(m Model) Model { return m.Lock() }, 1, 1, []string{"b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, testOptions(t), text, buffer.Pos{}, 40, 10)
			if tt.setup != nil {
				m = tt.setup(m)
			}
			m, cmd, ok := m.DeleteLines(tt.start, tt.end, tt.want)
			if ok || cmd != nil {
				t.Errorf("DeleteLines ok = %v, cmd = %v; want refused", ok, cmd != nil)
			}
			if m.Content() != text {
				t.Errorf("content = %q, want unchanged", m.Content())
			}
		})
	}
}
