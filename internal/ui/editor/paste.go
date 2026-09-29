package editor

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/attach"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vim"
)

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// handlePaste handles a bracketed paste (spec §6.2): an empty paste (a
// terminal that intercepted ctrl+v) checks the clipboard for an image, a
// paste that is only the path of an image file imports it, and anything
// else is inserted literally, never interpreted as keys. In vim command
// mode the paste is typed on the command line, without newlines.
func (m Model) handlePaste(msg tea.PasteMsg) (Model, tea.Cmd) {
	text := sanitize(msg.Content)
	if mc, ok := m.ed.(*vim.Machine); ok && mc.Mode() == vim.Command {
		// Typing a ":" or "/" line (allowed in read-only notes too): the
		// paste goes to the command line.
		text = strings.ReplaceAll(text, "\n", "")
		if text == "" {
			return m, nil
		}
		return m.apply(func() vim.Effect { return m.ed.Handle(m.buf, vim.Key{Text: text}) })
	}
	if m.readOnly {
		return m.flash()
	}
	if msg.Content == "" {
		return m, m.readClipboardCmd(false)
	}
	if text == "" {
		return m, nil
	}
	if p, ok := attach.ParsePastedPath(text, fileExists); ok {
		return m, m.emit(msgs.ImportImageMsg{Path: p})
	}
	return m.apply(func() vim.Effect {
		m.ed.PasteClipboard(m.buf, text, false)
		return vim.Effect{}
	})
}

// sanitize prepares pasted or clipboard text for the buffer: CRLF and lone
// CR become LF, and control characters other than tab and newline (escape
// sequences, backspaces, NUL...) are dropped so they never reach the
// terminal.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\t' && r != '\n') || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
