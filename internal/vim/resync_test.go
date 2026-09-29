package vim

import (
	"testing"

	"github.com/mathieucroset/notty/internal/buffer"
)

// external replaces the whole text, as a reload from disk does.
func external(b *buffer.Buffer, text string) func() {
	return func() { b.SetText(text) }
}

func TestResyncMachine(t *testing.T) {
	tests := []struct {
		name     string
		in, keys string
		text     string // the external change
		after    string // keys after the change
		want     string
		mode     Mode
	}{
		{"insert mode continues", "a|b", "ihello", "ahellob\nremote", "XY<esc>", "ahelloX|Yb\nremote", Normal},
		{"insert mode kept", "a|b", "i", "ab\nremote", "Z", "aZ|b\nremote", Insert},
		{"undo separates typing and change", "a|b", "ihi", "ahib\nremote", "yo<esc>u", "ahi|b\nremote", Normal},
		{"undo of change after typing", "a|b", "ihi<esc>", "ahib\nremote", "u", "ahi|b", Normal},
		{"normal clamps cursor", "abc\nde|f", "", "abc", "", "ab|c", Normal},
		{"visual kept", "a|bc\ndef", "v", "abc\ndef", "l", "abc\ndef", Visual},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			b := newBuf(tt.in)
			feed(m, b, tt.keys)
			m.Resync(b, external(b, tt.text))
			feed(m, b, tt.after)
			got := show(b)
			if tt.mode == Visual {
				got = b.String()
			}
			if got != tt.want || m.Mode() != tt.mode {
				t.Errorf("got %q mode %v, want %q mode %v", got, m.Mode(), tt.want, tt.mode)
			}
		})
	}
}

func TestResyncUndoSteps(t *testing.T) {
	// Typing, the external change and more typing are three undo steps.
	m := New()
	b := newBuf("|")
	feed(m, b, "ione")
	m.Resync(b, external(b, "one\nremote"))
	feed(m, b, "two<esc>")
	for _, want := range []string{"one\nremote", "one", ""} {
		feed(m, b, "u")
		if b.String() != want {
			t.Fatalf("undo gives %q, want %q", b.String(), want)
		}
	}
}

func TestResyncPlain(t *testing.T) {
	p := NewPlain()
	b := newBuf("|")
	feed(p, b, "one")
	p.Resync(b, external(b, "one\nremote"))
	feed(p, b, "two")
	if got := show(b); got != "onetwo|\nremote" {
		t.Fatalf("got %q", got)
	}
	for _, want := range []string{"one\nremote", "one", ""} {
		p.Handle(b, Key{Code: 'z', Ctrl: true})
		if b.String() != want {
			t.Fatalf("undo gives %q, want %q", b.String(), want)
		}
	}
}
