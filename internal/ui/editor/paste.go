package editor

import (
	"os"

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
// else is inserted literally, never interpreted as keys.
func (m Model) handlePaste(msg tea.PasteMsg) (Model, tea.Cmd) {
	if m.readOnly {
		return m.flash()
	}
	if msg.Content == "" {
		return m, m.readClipboardCmd(false)
	}
	if p, ok := attach.ParsePastedPath(msg.Content, fileExists); ok {
		return m, emit(msgs.ImportImageMsg{Path: p})
	}
	return m.apply(func() vim.Effect {
		m.ed.PasteClipboard(m.buf, msg.Content, false)
		return vim.Effect{}
	})
}
