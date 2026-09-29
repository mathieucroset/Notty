package vim

import (
	"testing"

	"github.com/mathieucroset/notty/internal/buffer"
)

// runPlain feeds keys to a new Plain editor.
func runPlain(t *testing.T, text, keys string) (*Plain, *buffer.Buffer, Effect) {
	t.Helper()
	p := NewPlain()
	b := newBuf(text)
	eff := feed(p, b, keys)
	return p, b, eff
}

func TestPlainEditing(t *testing.T) {
	tests := []struct {
		name, in, keys, want string
	}{
		{"type", "|x", "abc", "abc|x"},
		{"type letters that are vim commands", "|", "dd:wq", "dd:wq|"},
		{"space", "a|b", "<space>", "a |b"},
		{"backspace", "ab|c", "<bs>", "a|c"},
		{"backspace joins", "ab\n|cd", "<bs>", "ab|cd"},
		{"delete", "a|bc", "<del>", "a|c"},
		{"delete joins", "ab|\ncd", "<del>", "ab|cd"},
		{"enter", "ab|cd", "<cr>", "ab\n|cd"},
		{"enter keeps indent", "  ab|", "<cr>x", "  ab\n  x|"},
		{"left right", "a|bc", "<right><right><left>", "ab|c"},
		{"left wraps", "ab\n|cd", "<left>", "ab|\ncd"},
		{"right wraps", "ab|\ncd", "<right>", "ab\n|cd"},
		{"right at end of buffer", "ab|", "<right>", "ab|"},
		{"up down sticky", "abcd|\nx\nabcdef", "<down><down>", "abcd\nx\nabcd|ef"},
		{"up on first line goes home", "ab|c", "<up>", "|abc"},
		{"down on last line goes end", "a|bc", "<down>", "abc|"},
		{"home end", "a|bc", "<end>X<home>Y", "Y|abcX"},
		{"typing replaces selection", "|abcd", "<s-right><s-right>X", "X|cd"},
		{"backspace deletes selection", "ab|cd", "<s-left><s-left><bs>", "|cd"},
		{"delete deletes selection", "ab|cd", "<s-right><del>", "ab|d"},
		{"enter replaces selection", "a|bcd", "<s-right><s-right><cr>", "a\n|d"},
		{"left collapses selection", "a|bcd", "<s-right><s-right><left>", "a|bcd"},
		{"right collapses selection", "a|bcd", "<s-right><s-right><right>", "abc|d"},
		{"shift down selects lines", "|ab\ncd", "<s-down><bs>", "|cd"},
		{"shift end", "a|bc", "<s-end><bs>", "a|"},
		{"shift home", "ab|c", "<s-home><bs>", "|c"},
		{"select all", "a|b\ncd", "<c-a>X", "X|"},
		{"tab indents line", "a|b", "<tab>", "  a|b"},
		{"tab in tab-indented line", "\ta|b", "<tab>", "\t\ta|b"},
		{"shift tab outdents", "    a|b", "<s-tab>", "  a|b"},
		{"shift tab cursor in indent", " | ab", "<s-tab>", "|ab"},
		{"tab indents selected lines", "|a\nb\nc", "<s-down><s-right><tab>", "  a\n  b|\nc"},
		{"tab skips empty selected lines", "|a\n\nb\nc", "<s-down><s-down><s-right><tab>", "  a\n\n  b|\nc"},
		{"undo typing group", "|x", "abc<c-z>", "|x"},
		{"typing groups break on move", "|x", "ab<left>cd<c-z>", "a|bx"},
		{"redo", "|x", "abc<c-z><c-y>", "|abcx"},
		{"undo backspace", "ab|c", "<bs><c-z>", "a|bc"},
		{"cut line", "a|b\ncd", "<c-x>", "|cd"},
		{"cut last line", "ab\nc|d", "<c-x>", "ab|"},
		{"cut selection", "a|bcd", "<s-right><s-right><c-x>", "a|d"},
		{"copy keeps text", "a|bcd", "<s-right><c-c>", "ab|cd"},
		{"esc clears selection", "a|bcd", "<s-right><esc>X", "abX|cd"},
		{"multi-rune text", "|x", "é👍", "é👍|x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, b, _ := runPlain(t, tt.in, tt.keys)
			if got := show(b); got != tt.want {
				t.Errorf("%q + %q: got %q, want %q", tt.in, tt.keys, got, tt.want)
			}
		})
	}
}

func TestPlainClipboard(t *testing.T) {
	tests := []struct {
		name, in, keys, clip string
	}{
		{"copy selection", "a|bcd", "<s-right><s-right><c-c>", "bc"},
		{"copy line without selection", "a|b\ncd", "<c-c>", "ab\n"},
		{"cut selection", "a|bcd", "<s-right><c-x>", "b"},
		{"cut line", "ab\n|cd", "<c-x>", "cd\n"},
		{"copy multi-line", "a|b\ncd", "<s-down><c-c>", "b\nc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, eff := runPlain(t, tt.in, tt.keys)
			if eff.Clipboard == nil || *eff.Clipboard != tt.clip {
				t.Errorf("Clipboard = %v, want %q", eff.Clipboard, tt.clip)
			}
		})
	}
}

func TestPlainPaste(t *testing.T) {
	p, b, eff := runPlain(t, "a|bcd", "<s-right><s-right><c-v>")
	if !eff.NeedClipboard {
		t.Fatal("NeedClipboard not set")
	}
	p.PasteClipboard(b, "XY\nZ", false)
	if got := show(b); got != "aXY\nZ|d" {
		t.Errorf("got %q", got)
	}
	feed(p, b, "<c-z>")
	if got := b.String(); got != "abcd" {
		t.Errorf("undo paste: %q", got)
	}
}

func TestPlainEffects(t *testing.T) {
	_, _, eff := runPlain(t, "|a", "<esc>")
	if !eff.FocusSidebar {
		t.Error("esc should focus the sidebar")
	}
	p := NewPlain()
	if p.ModeName() != "PLAIN" {
		t.Errorf("ModeName %q", p.ModeName())
	}
	p.SetReadOnly(true)
	if p.ModeName() != "READ-ONLY" {
		t.Errorf("ModeName %q", p.ModeName())
	}
}

func TestPlainSelection(t *testing.T) {
	p, b, _ := runPlain(t, "a|bcd", "<s-right><s-right>")
	r, ok := p.Selection(b)
	if !ok || r != (buffer.Range{Start: pos(0, 1), End: pos(0, 3)}) {
		t.Errorf("Selection = %v %v", r, ok)
	}
	p, b, _ = runPlain(t, "ab|cd", "<s-left><s-left>")
	r, ok = p.Selection(b)
	if !ok || r != (buffer.Range{Start: pos(0, 0), End: pos(0, 2)}) {
		t.Errorf("backward Selection = %v %v", r, ok)
	}
	p, b, _ = runPlain(t, "ab|cd", "<s-left><right>")
	if _, ok := p.Selection(b); ok {
		t.Error("selection should be cleared by an arrow")
	}
}

func TestPlainReadOnly(t *testing.T) {
	tests := []struct {
		name, keys, want string
		blocked          bool
	}{
		{"type", "x", "a|bc", true},
		{"backspace", "<bs>", "a|bc", true},
		{"enter", "<cr>", "a|bc", true},
		{"cut", "<c-x>", "a|bc", true},
		{"paste", "<c-v>", "a|bc", true},
		{"undo", "<c-z>", "a|bc", true},
		{"tab focuses sidebar", "<tab>", "a|bc", false},
		{"shift tab", "<s-tab>", "a|bc", true},
		{"move ok", "<right>", "ab|c", false},
		{"select ok", "<s-right>", "ab|c", false},
		{"copy ok", "<c-c>", "a|bc", false},
		{"esc ok", "<esc>", "a|bc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPlain()
			p.SetReadOnly(true)
			b := newBuf("a|bc")
			eff := feed(p, b, tt.keys)
			if got := show(b); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if eff.Blocked != tt.blocked {
				t.Errorf("Blocked = %v", eff.Blocked)
			}
			if want := tt.keys == "<tab>" || tt.keys == "<esc>"; eff.FocusSidebar != want {
				t.Errorf("FocusSidebar = %v, want %v", eff.FocusSidebar, want)
			}
			p.PasteClipboard(b, "zz", false)
			if b.Dirty() {
				t.Error("buffer modified")
			}
		})
	}
}
