package editor

import (
	"path"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vim"
)

// AutosaveTickMsg fires AutosaveMS after a change. When it arrives while the
// buffer is still at Version and dirty, the editor emits
// msgs.SaveRequestMsg.
type AutosaveTickMsg struct {
	Path    string
	Version uint64
}

// ChangedMsg is emitted after each buffer change so the app can update the
// index, the preview and the dirty marker.
type ChangedMsg struct {
	Path    string
	Version uint64
}

// StatusMsg carries a vim message (search wrapped, unknown command...) for
// the app's status line.
type StatusMsg struct{ Text string }

// flashEndMsg ends the read-only banner flash started with the same id.
type flashEndMsg struct{ id int }

// flashDuration is how long the banner flashes on a blocked edit (a
// variable so tests can shorten it).
var flashDuration = 300 * time.Millisecond

// emit returns a Cmd delivering msg.
func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// Update handles key presses, pastes and the editor's own messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case clipboardTextMsg:
		return m.pasteClipboardText(msg)
	case flashEndMsg:
		if msg.id == m.flashID {
			m.flashing = false
		}
		return m, nil
	}
	return m, nil
}

// handleKey feeds one key to the vim engine. In a read-only note `c` opens
// the resolver instead (unless it is the argument of a pending command such
// as `fc`).
func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	k := translateKey(msg)
	if m.readOnly && m.resolverKey(k) {
		return m, emit(msgs.OpenResolverMsg{Path: m.path})
	}
	return m.apply(func() vim.Effect { return m.ed.Handle(m.buf, k) })
}

// resolverKey reports whether k is a bare `c` the engine is not waiting for.
func (m Model) resolverKey(k vim.Key) bool {
	if k.Text != "c" || k.Ctrl || k.Alt || k.Name != "" {
		return false
	}
	if mc, ok := m.ed.(*vim.Machine); ok {
		return mc.Mode() != vim.Command && mc.Mode() != vim.Insert && mc.Pending() == ""
	}
	return true
}

// apply runs an engine call, then restyles changed lines, performs the
// effect and keeps the cursor in view.
func (m Model) apply(run func() vim.Effect) (Model, tea.Cmd) {
	before := m.buf.Version()
	eff := run()
	var cmds []tea.Cmd
	if m.buf.Version() != before {
		m.syncStyle()
		cmds = append(cmds, m.ChangeCmd())
	}
	m, effCmds := m.effects(eff)
	cmds = append(cmds, effCmds...)
	return m.ensureVisible(), tea.Batch(cmds...)
}

// ChangeCmd returns the Cmd to run after a buffer change: it emits
// ChangedMsg and schedules the autosave tick for the current version. The
// editor runs it itself after keys and pastes; the app runs it after
// ApplyToggle and InsertText.
func (m Model) ChangeCmd() tea.Cmd {
	p, v := m.path, m.buf.Version()
	delay := time.Duration(m.opts.AutosaveMS) * time.Millisecond
	return tea.Batch(
		emit(ChangedMsg{Path: p, Version: v}),
		tea.Tick(delay, func(time.Time) tea.Msg { return AutosaveTickMsg{Path: p, Version: v} }),
	)
}

// effects turns an engine Effect into messages and commands.
func (m Model) effects(eff vim.Effect) (Model, []tea.Cmd) {
	var cmds []tea.Cmd
	add := func(msg tea.Msg) { cmds = append(cmds, emit(msg)) }
	blocked := eff.Blocked
	if eff.Save {
		if m.readOnly {
			blocked = true // never write a conflicted note
		} else {
			add(msgs.SaveRequestMsg{})
		}
	}
	if eff.Quit {
		add(msgs.QuitMsg{})
	}
	if eff.OpenNote && eff.NoteArg != "" {
		add(msgs.OpenNoteMsg{Path: notePath(eff.NoteArg), Line: -1})
	}
	if eff.Help {
		add(msgs.OpenHelpMsg{})
	}
	if eff.ImgArg != "" {
		if m.readOnly {
			blocked = true
		} else {
			add(msgs.ImportImageMsg{Path: eff.ImgArg})
		}
	}
	if eff.Clipboard != nil {
		cmds = append(cmds, m.copyCmd(*eff.Clipboard))
	}
	if eff.NeedClipboard {
		cmds = append(cmds, m.readClipboardCmd(eff.PasteBefore))
	}
	if eff.FocusSidebar {
		add(msgs.FocusSidebarMsg{})
	}
	if eff.FocusMain {
		add(msgs.FocusMainMsg{})
	}
	if eff.Message != "" {
		add(StatusMsg{Text: eff.Message})
	}
	if blocked {
		var cmd tea.Cmd
		m, cmd = m.flash()
		cmds = append(cmds, cmd)
	}
	return m, cmds
}

// notePath turns a `:e` argument into a vault-relative note path.
func notePath(arg string) string {
	p := path.Clean("/" + strings.ReplaceAll(strings.TrimSpace(arg), "\\", "/"))[1:]
	if !strings.HasSuffix(strings.ToLower(p), ".md") {
		p += ".md"
	}
	return p
}

// flash starts the read-only banner flash (spec §4.4, plan A8).
func (m Model) flash() (Model, tea.Cmd) {
	m.flashID++
	m.flashing = true
	id := m.flashID
	return m, tea.Tick(flashDuration, func(time.Time) tea.Msg { return flashEndMsg{id: id} })
}
