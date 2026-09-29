package editor

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/clipboard"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vim"
)

// clipboardTextMsg delivers clipboard text read after a NeedClipboard effect
// (or an empty bracketed paste) back to the editor that asked for it.
type clipboardTextMsg struct {
	path   string
	text   string
	before bool
}

// copyCmd puts text on the system clipboard, falling back to OSC52 when the
// clipboard tool fails (e.g. over SSH) or there is none.
func (m Model) copyCmd(text string) tea.Cmd {
	cb := m.opts.Clipboard
	if cb == nil {
		return tea.SetClipboard(text)
	}
	return func() tea.Msg {
		if err := cb.WriteText(text); err != nil {
			return tea.SetClipboard(text)()
		}
		return nil
	}
}

// readClipboardCmd reads the clipboard for a paste (spec §6.2): an image
// becomes msgs.ImportImageMsg (the app imports it), otherwise the text is
// delivered back as clipboardTextMsg. When there is no text either and the
// image tool is missing, a toast names the tool.
func (m Model) readClipboardCmd(before bool) tea.Cmd {
	cb, p := m.opts.Clipboard, m.path
	if cb == nil {
		return emit(msgs.ToastMsg{Level: msgs.ToastWarn, Text: "No clipboard available"})
	}
	return func() tea.Msg {
		data, err := cb.ReadImage()
		if err == nil && len(data) > 0 {
			return msgs.ImportImageMsg{Data: data, Ext: "png"}
		}
		var noTool clipboard.ErrNoTool
		missing := errors.As(err, &noTool)
		text, terr := cb.ReadText()
		switch {
		case terr == nil && text != "":
			return clipboardTextMsg{path: p, text: text, before: before}
		case missing:
			return msgs.ToastMsg{Level: msgs.ToastWarn, Text: "Image paste needs " + noTool.Tool + " (not installed)"}
		case terr != nil:
			return msgs.ToastMsg{Level: msgs.ToastWarn, Text: "Could not read the clipboard: " + terr.Error()}
		}
		return nil // empty clipboard
	}
}

// pasteClipboardText pastes text read from the clipboard, literally.
func (m Model) pasteClipboardText(msg clipboardTextMsg) (Model, tea.Cmd) {
	if msg.path != m.path {
		return m, nil // the note changed while the clipboard was read
	}
	if m.readOnly {
		return m.flash()
	}
	return m.apply(func() vim.Effect {
		m.ed.PasteClipboard(m.buf, sanitize(msg.text), msg.before)
		return vim.Effect{}
	})
}
