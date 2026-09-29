package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/attach"
	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/help"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/palette"
	"github.com/mathieucroset/notty/internal/ui/statusbar"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/ui/toast"
	"github.com/mathieucroset/notty/internal/vault"
)

// dlgCleanAttachments confirms deleting unused attachments.
const dlgCleanAttachments = "clean-attachments"

// unusedAttachmentsMsg carries the attachments no note references.
type unusedAttachmentsMsg struct {
	files []string
	err   error
}

// attachmentsCleanedMsg reports deleted attachments.
type attachmentsCleanedMsg struct {
	n   int
	err error
}

// configEditedMsg reports that $EDITOR exited after editing config.toml.
type configEditedMsg struct{ err error }

// openPalette opens the command palette (ctrl+k).
func (m *Model) openPalette() {
	p := palette.New(palette.DefaultCommands(), m.opts.Styles).
		WithCurrentTheme(m.opts.Palette.Name).
		SetSize(m.width, m.height)
	m.openOverlay(&overlayState{kind: overlayPalette, palette: p, paletteTheme: m.opts.Palette.Name})
}

// toggleHelp opens the help overlay, or closes it when it is open (F1
// passes through overlays).
func (m *Model) toggleHelp() {
	if o := m.topOverlay(); o != nil && o.kind == overlayHelp {
		m.closeOverlay()
		return
	}
	h := help.New(m.opts.Styles).SetSize(m.width, m.height)
	m.pushOverlay(&overlayState{kind: overlayHelp, help: h})
}

// openErrorLog opens the error log overlay.
func (m *Model) openErrorLog() {
	// The error log is for problems: info toasts are left out.
	var entries []toast.Entry
	for _, e := range m.toast.Log() {
		if e.Level >= msgs.ToastWarn {
			entries = append(entries, e)
		}
	}
	lv := toast.NewLogView(entries, m.opts.Styles)
	w, h := m.logSize()
	lv.SetSize(w, h)
	m.openOverlay(&overlayState{kind: overlayLog, log: lv})
}

// logBoxSize is the error log box's outer size.
func (m *Model) logBoxSize() (int, int) {
	w := min(max(m.width*80/100, min(40, m.width)), 100)
	h := max(m.height*70/100, min(10, m.height))
	return w, h
}

// logSize is the size of the log itself inside its box: minus the border
// and padding, and the title with its blank line.
func (m *Model) logSize() (int, int) {
	w, h := m.logBoxSize()
	return max(w-6, 1), max(h-6, 1)
}

// logBox renders the error log overlay box.
func (m *Model) logBox(o *overlayState) string {
	bw, _ := m.logBoxSize()
	w, h := m.logSize()
	st := m.opts.Styles
	title := st.DialogTitle.Render("Error log") + "  " + st.Muted.Render("j/k scroll · esc close")
	return st.Dialog.Width(bw).Render(title + "\n\n" + o.log.View(w, h))
}

// resizeOverlay fits the open overlay to a new terminal size.
func (m *Model) resizeOverlay() {
	for _, o := range m.overlays {
		switch o.kind {
		case overlayPalette:
			o.palette = o.palette.SetSize(m.width, m.height)
		case overlayHelp:
			o.help = o.help.SetSize(m.width, m.height)
		case overlayLog:
			w, h := m.logSize()
			o.log.SetSize(w, h)
		}
	}
}

// applyTheme re-themes every component with the named theme. It changes
// nothing on disk.
func (m *Model) applyTheme(name string) {
	p, ok := theme.Get(name)
	if !ok {
		return
	}
	st := theme.NewStyles(p)
	m.opts.Palette, m.opts.Styles = p, st
	m.sidebar.SetStyles(st)
	m.status = statusbar.New(st)
	m.toast = m.toast.SetStyles(st)
	m.tasks = m.tasks.SetStyles(st)
	m.trash = m.trash.SetTheme(st, p)
	m.editor = m.editor.SetTheme(st, p)
	var cmd tea.Cmd
	m.preview, cmd = m.preview.SetTheme(st, p)
	m.later(cmd)
	for _, o := range m.overlays {
		if o.kind == overlayPalette {
			o.palette = o.palette.SetStyles(st)
		}
	}
	m.relayout()
}

// setConfigCmd writes one key to the local config.toml (spec §8: only
// that key changes).
func (m *Model) setConfigCmd(key string, value any) tea.Cmd {
	path := m.configPath()
	return func() tea.Msg {
		if err := config.SetKey(path, key, value); err != nil {
			return msgs.ToastMsg{Level: msgs.ToastWarn, Text: fmt.Sprintf("Could not save the setting: %v", err)}
		}
		return nil
	}
}

// configPath is the local config.toml.
func (m *Model) configPath() string {
	if m.opts.ConfigPath != "" {
		return m.opts.ConfigPath
	}
	return config.ConfigPath()
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// toggleVim flips vim mode, live in the editor, and saves it.
func (m *Model) toggleVim() tea.Cmd {
	m.opts.Config.Vim = !m.opts.Config.Vim
	m.editor = m.editor.SetVim(m.opts.Config.Vim)
	return tea.Batch(m.setConfigCmd("vim", m.opts.Config.Vim),
		m.pushToast(msgs.ToastInfo, "Vim mode "+onOff(m.opts.Config.Vim)))
}

// toggleLineNumbers flips the editor's line numbers and saves the setting.
func (m *Model) toggleLineNumbers() tea.Cmd {
	m.opts.Config.LineNumbers = !m.opts.Config.LineNumbers
	m.editor = m.editor.SetLineNumbers(m.opts.Config.LineNumbers)
	return tea.Batch(m.setConfigCmd("line_numbers", m.opts.Config.LineNumbers),
		m.pushToast(msgs.ToastInfo, "Line numbers "+onOff(m.opts.Config.LineNumbers)))
}

// syncNow asks the syncer for an immediate cycle.
// TODO(syncer pass, Task 32): hand off to the syncer.
func (m *Model) syncNow() tea.Cmd {
	return m.pushToast(msgs.ToastInfo, "Sync is not set up yet")
}

// setupSync opens the sync setup wizard.
// TODO(wizard pass, Task 33): open the wizard in "set up sync" mode.
func (m *Model) setupSync() tea.Cmd {
	return m.pushToast(msgs.ToastInfo, "Sync is not set up yet")
}

// openResolver opens the conflict resolver on p ("" for the first
// conflict).
// TODO(resolver pass, Task 34): open the resolver when there are conflicts.
func (m *Model) openResolver(p string) tea.Cmd {
	_ = p
	return m.pushToast(msgs.ToastInfo, "No conflicts to resolve")
}

// openConfig opens config.toml in $EDITOR.
func (m *Model) openConfig() tea.Cmd {
	return execProcess(m.editorCommand(m.configPath()), func(err error) tea.Msg {
		return configEditedMsg{err: err}
	})
}

func (m *Model) handleConfigEdited(msg configEditedMsg) tea.Cmd {
	ready := m.afterExec()
	if msg.err != nil {
		return tea.Batch(ready, m.pushToast(msgs.ToastError, fmt.Sprintf("The editor failed: %v", msg.err)))
	}
	return tea.Batch(ready, m.pushToast(msgs.ToastInfo, "Restart Notty to apply every config change"))
}

// findUnusedCmd lists the attachments no note references.
func findUnusedCmd(v *vault.Vault) tea.Cmd {
	return func() tea.Msg {
		files, err := attach.Unused(v)
		return unusedAttachmentsMsg{files: files, err: err}
	}
}

func (m *Model) handleUnusedAttachments(msg unusedAttachmentsMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not look for unused attachments: %v", msg.err))
	case len(msg.files) == 0:
		return m.pushToast(msgs.ToastInfo, "No unused attachments")
	}
	what := "1 unused attachment"
	if len(msg.files) > 1 {
		what = fmt.Sprintf("%d unused attachments", len(msg.files))
	}
	d := dialog.NewConfirm(dlgCleanAttachments, "Clean attachments", "Delete "+what+"? No note links to them.",
		"Delete", "Cancel", true, m.opts.Styles)
	m.openDialog(d, pendingOp{kind: opCleanAttachments, files: msg.files})
	return nil
}

// deleteFilesCmd deletes vault files, carrying on past failures.
// TODO(syncer pass): commit the deletion through the syncer.
func deleteFilesCmd(v *vault.Vault, files []string) tea.Cmd {
	return func() tea.Msg {
		n := 0
		var errs []error
		for _, f := range files {
			if err := os.Remove(v.Abs(f)); err != nil {
				errs = append(errs, err)
				continue
			}
			n++
		}
		return attachmentsCleanedMsg{n: n, err: errors.Join(errs...)}
	}
}

func (m *Model) handleAttachmentsCleaned(msg attachmentsCleanedMsg) tea.Cmd {
	text := fmt.Sprintf("Deleted %d unused attachment", msg.n)
	if msg.n != 1 {
		text += "s"
	}
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, text+"; some could not be deleted: "+strings.ReplaceAll(msg.err.Error(), "\n", "; "))
	}
	return m.pushToast(msgs.ToastInfo, text)
}

// updateCommandMsg handles the palette, help and error log messages.
func (m *Model) updateCommandMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case msgs.OpenPaletteMsg:
		if !m.opts.WizardNeeded {
			m.openPalette()
		}
	case palette.CloseMsg:
		m.closeOverlayKind(overlayPalette)
	case msgs.OpenHelpMsg:
		if !m.opts.WizardNeeded {
			m.toggleHelp()
		}
	case help.CloseMsg:
		m.closeOverlayKind(overlayHelp)
	case msgs.OpenErrorLogMsg:
		m.openErrorLog()
	case toast.CloseLogMsg:
		m.closeOverlayKind(overlayLog)
	case palette.ThemePreviewMsg:
		m.applyTheme(msg.Name)
	case palette.ThemeCancelMsg:
		m.applyTheme(msg.Original)
	case palette.ThemeChosenMsg:
		m.applyTheme(msg.Name)
		m.opts.Config.Theme = m.opts.Palette.Name
		return m.setConfigCmd("theme", m.opts.Palette.Name), true
	case palette.ToggleVimMsg:
		return m.toggleVim(), true
	case palette.ToggleLineNumbersMsg:
		return m.toggleLineNumbers(), true
	case palette.SyncNowMsg:
		return m.syncNow(), true
	case palette.SetupSyncMsg:
		return m.setupSync(), true
	case msgs.OpenResolverMsg:
		return m.openResolver(msg.Path), true
	case palette.OpenConfigMsg:
		return m.openConfig(), true
	case configEditedMsg:
		return m.handleConfigEdited(msg), true
	// TODO(integration pass B): the finder overlays and the editor save
	// replace these placeholders.
	case msgs.OpenFinderMsg:
		if msg.FullText {
			return m.pushToast(msgs.ToastInfo, "Full-text search is coming soon"), true
		}
		return m.pushToast(msgs.ToastInfo, "The fuzzy finder is coming soon"), true
	case palette.CleanAttachmentsMsg:
		if m.opts.Vault == nil {
			return nil, true
		}
		return findUnusedCmd(m.opts.Vault), true
	case unusedAttachmentsMsg:
		return m.handleUnusedAttachments(msg), true
	case attachmentsCleanedMsg:
		return m.handleAttachmentsCleaned(msg), true
	default:
		return nil, false
	}
	return nil, true
}
