package editor

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/clipboard"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestPaste(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(img, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("image path imports", func(t *testing.T) {
		m := newModel(t, testOptions(t), "x", buffer.Pos{}, 40, 3)
		m, cmd := m.Update(tea.PasteMsg{Content: "'" + img + "'\n"})
		imp, ok := find[msgs.ImportImageMsg](collect(cmd))
		if !ok || imp.Path != img {
			t.Errorf("paste of an image path: got %#v, want ImportImageMsg{Path: %q}", imp, img)
		}
		if m.Content() != "x" {
			t.Errorf("path inserted as text: %q", m.Content())
		}
	})

	t.Run("missing image path is text", func(t *testing.T) {
		m := newModel(t, testOptions(t), "", buffer.Pos{}, 40, 3)
		m, _ = typeKeys(m, "i")
		missing := filepath.Join(dir, "nope.png")
		m, _ = m.Update(tea.PasteMsg{Content: missing})
		if m.Content() != missing {
			t.Errorf("content = %q", m.Content())
		}
	})

	t.Run("empty paste checks the image clipboard", func(t *testing.T) {
		opts := testOptions(t)
		opts.Clipboard = &fakeClipboard{image: []byte("PNG")}
		m := newModel(t, opts, "x", buffer.Pos{}, 40, 3)
		m, cmd := m.Update(tea.PasteMsg{})
		img, ok := find[clipboardImageMsg](collect(cmd))
		if !ok {
			t.Fatal("empty paste does not read the clipboard image")
		}
		_, cmd = m.Update(img)
		imp, ok := find[msgs.ImportImageMsg](collect(cmd))
		if !ok || string(imp.Data) != "PNG" || imp.Ext != "png" {
			t.Errorf("empty paste: got %#v, want ImportImageMsg with data", imp)
		}
	})

	t.Run("empty paste without image pastes clipboard text", func(t *testing.T) {
		opts := testOptions(t)
		opts.Clipboard = &fakeClipboard{imageErr: clipboard.ErrNoImage, text: "clip"}
		m := newModel(t, opts, "", buffer.Pos{}, 40, 3)
		m, cmd := m.Update(tea.PasteMsg{})
		txt, ok := find[clipboardTextMsg](collect(cmd))
		if !ok {
			t.Fatal("no clipboard text message")
		}
		m, _ = m.Update(txt)
		if m.Content() != "clip" {
			t.Errorf("content = %q", m.Content())
		}
	})

	t.Run("text is inserted literally in normal mode", func(t *testing.T) {
		m := newModel(t, testOptions(t), "abc\ndef", buffer.Pos{}, 40, 3)
		m, cmd := m.Update(tea.PasteMsg{Content: "dG"})
		if got := m.Content(); got != "adGbc\ndef" {
			t.Errorf("content = %q, want the paste inserted as text after the cursor", got)
		}
		if _, ok := find[ChangedMsg](collect(cmd)); !ok {
			t.Error("paste does not report a change")
		}
		if m.ModeName() != "NORMAL" {
			t.Errorf("paste changed the mode to %s", m.ModeName())
		}
	})

	t.Run("text in insert mode", func(t *testing.T) {
		m := newModel(t, testOptions(t), "ab", buffer.Pos{Col: 1}, 40, 3)
		m, _ = typeKeys(m, "i")
		m, _ = m.Update(tea.PasteMsg{Content: "<esc>:q\n"})
		if got := m.Content(); got != "a<esc>:q\nb" {
			t.Errorf("content = %q", got)
		}
	})

	t.Run("read-only blocks paste", func(t *testing.T) {
		m := newModel(t, testOptions(t), "x", buffer.Pos{}, 40, 3).SetReadOnly(true, "")
		m, _ = m.Update(tea.PasteMsg{Content: "text"})
		if m.Content() != "x" || !m.flashing {
			t.Errorf("read-only paste: content %q flashing %v", m.Content(), m.flashing)
		}
	})
}

func TestSanitize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a\r\nb", "a\nb"},
		{"one\rtwo\rthree", "one\ntwo\nthree"},
		{"esc\x1b[2Jseq", "esc[2Jseq"},
		{"osc\x1b]52;c;aGk=\x07end", "osc]52;c;aGk=end"},
		{"bs\x08\x00del\x7f", "bsdel"},
		{"tab\tkept\nnl", "tab\tkept\nnl"},
		{"日本語 😀", "日本語 😀"},
	}
	for _, tt := range tests {
		if got := sanitize(tt.in); got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPasteIsSanitized(t *testing.T) {
	m := newModel(t, testOptions(t), "", buffer.Pos{}, 40, 5)
	m, _ = typeKeys(m, "i")
	m, _ = m.Update(tea.PasteMsg{Content: "one\rtwo\x1b[2J"})
	if got := m.Content(); got != "one\ntwo[2J" {
		t.Errorf("paste content = %q", got)
	}
	m, _ = m.Update(clipboardTextMsg{path: m.Path(), text: "\r\nx\x07"})
	if got := m.Content(); got != "one\ntwo[2J\nx" {
		t.Errorf("clipboard content = %q", got)
	}
}

func TestPasteInCommandMode(t *testing.T) {
	tests := []struct {
		name, prefix, paste, want string
	}{
		{"ex command", ":", "e ideas", ":e ideas"},
		{"search", "/", "alp", "/alp"},
		{"newlines stripped", ":", "e\nwork/\nnotes\n", ":ework/notes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, testOptions(t), "alpha\n", buffer.Pos{}, 40, 5)
			m, _ = typeKeys(m, tt.prefix)
			m, _ = m.Update(tea.PasteMsg{Content: tt.paste})
			if m.CommandLine() != tt.want || m.Content() != "alpha\n" {
				t.Errorf("command line %q content %q, want %q and the buffer untouched", m.CommandLine(), m.Content(), tt.want)
			}
		})
	}
	ro := newModel(t, testOptions(t), "alpha\n", buffer.Pos{}, 40, 5).SetReadOnly(true, "")
	ro, _ = typeKeys(ro, "/")
	ro, _ = ro.Update(tea.PasteMsg{Content: "lph"})
	if ro.CommandLine() != "/lph" {
		t.Errorf("read-only search paste: command line %q", ro.CommandLine())
	}

	m := newModel(t, testOptions(t), "alpha\n", buffer.Pos{}, 40, 5)
	m, _ = typeKeys(m, ":")
	m, _ = m.Update(tea.PasteMsg{Content: "e ideas"})
	_, out := typeKeys(m, "enter")
	if o, ok := find[msgs.OpenNoteMsg](out); !ok || o.Path != "ideas.md" {
		t.Errorf("pasted command does not run: %#v", out)
	}
}
