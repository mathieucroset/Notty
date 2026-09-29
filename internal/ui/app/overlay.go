package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/finder"
	"github.com/mathieucroset/notty/internal/ui/help"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/palette"
	"github.com/mathieucroset/notty/internal/ui/textutil"
	"github.com/mathieucroset/notty/internal/ui/toast"
)

// overlayKind identifies the overlay on top of the screen (spec §4.2).
type overlayKind int

// Overlay kinds.
const (
	overlayDialog overlayKind = iota + 1
	overlayPalette
	overlayHelp
	overlayLog
	overlayFinder
)

// overlayState is one overlay in the stack over the screen. Opening an
// overlay replaces the stack, except help, which stacks on top so closing
// it returns to what was open.
type overlayState struct {
	kind overlayKind

	dialog dialog.Model
	// pending says what to do with the dialog's result.
	pending pendingOp

	palette palette.Model
	// paletteTheme is the theme when the palette opened, restored if the
	// palette is dropped mid-preview.
	paletteTheme string
	help         help.Model
	log          toast.LogView
	finder       finder.Model
}

// toastTimer wraps the expiry command of an info or warning toast. Tests
// replace it to keep the 4s ticks out of their synchronous command loop.
var toastTimer = func(c tea.Cmd) tea.Cmd { return c }

// topOverlay returns the overlay on top of the stack, or nil.
func (m *Model) topOverlay() *overlayState {
	if len(m.overlays) == 0 {
		return nil
	}
	return m.overlays[len(m.overlays)-1]
}

// overlayOpen reports whether an overlay is open.
func (m *Model) overlayOpen() bool { return len(m.overlays) > 0 }

// openOverlay shows s as the only overlay, dropping any others. Help is
// the one overlay that stacks on top instead (pushOverlay).
func (m *Model) openOverlay(s *overlayState) {
	for i := len(m.overlays) - 1; i >= 0; i-- {
		m.leaveOverlay(m.overlays[i])
	}
	m.overlays = []*overlayState{s}
	m.syncOverlayFlag()
}

// pushOverlay shows s on top of the open overlays; closing it reveals
// them again.
func (m *Model) pushOverlay(s *overlayState) {
	m.overlays = append(m.overlays, s)
	m.syncOverlayFlag()
}

// leaveOverlay cleans up after an overlay dropped without closing itself:
// a palette previewing a theme cancels the preview, exactly as its own
// ThemeCancelMsg would.
func (m *Model) leaveOverlay(s *overlayState) {
	if s.kind == overlayPalette && m.opts.Palette.Name != s.paletteTheme {
		m.applyTheme(s.paletteTheme)
	}
	if s.kind == overlayDialog && s.pending.kind == opExternalChange {
		// An unanswered "changed on disk" dialog keeps the edits.
		m.later(m.resolveExternalChange(s.pending.path, choiceKeepMine))
	}
}

// openDialog shows d as the active overlay; its result is handled by op.
func (m *Model) openDialog(d dialog.Model, op pendingOp) {
	m.openOverlay(&overlayState{kind: overlayDialog, dialog: d, pending: op})
}

// closeOverlay closes the overlay on top.
func (m *Model) closeOverlay() {
	if len(m.overlays) > 0 {
		m.overlays = m.overlays[:len(m.overlays)-1]
	}
	m.syncOverlayFlag()
}

// closeOverlayKind closes the topmost overlay of kind k: an overlay's own
// close request. A request from an overlay that was since replaced finds
// nothing to close.
func (m *Model) closeOverlayKind(k overlayKind) {
	for i := len(m.overlays) - 1; i >= 0; i-- {
		if m.overlays[i].kind == k {
			m.overlays = append(m.overlays[:i:i], m.overlays[i+1:]...)
			m.syncOverlayFlag()
			return
		}
	}
}

// updateOverlay routes a key press to the overlay on top.
func (m *Model) updateOverlay(k tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	o := m.topOverlay()
	switch o.kind {
	case overlayDialog:
		o.dialog, cmd = o.dialog.Update(k)
	case overlayPalette:
		o.palette, cmd = o.palette.Update(k)
	case overlayHelp:
		o.help, cmd = o.help.Update(k)
	case overlayLog:
		o.log, cmd = o.log.Update(k)
	case overlayFinder:
		o.finder, cmd = o.finder.Update(k)
	}
	return cmd
}

// updateFinders forwards a non-key message to open finder overlays: their
// search debounce, search results and preview renders.
func (m *Model) updateFinders(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, o := range m.overlays {
		if o.kind == overlayFinder {
			var cmd tea.Cmd
			o.finder, cmd = o.finder.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// handleDialogResult closes the dialog that produced res and acts on it.
// A result from a dialog that is no longer on top is dropped.
func (m *Model) handleDialogResult(res dialog.ResultMsg) tea.Cmd {
	o := m.topOverlay()
	if o == nil || o.kind != overlayDialog || o.dialog.ID() != res.ID {
		return nil
	}
	m.closeOverlay()
	if !res.OK {
		if o.pending.kind == opExternalChange {
			// Dismissing the dialog keeps the edits.
			return m.resolveExternalChange(o.pending.path, choiceKeepMine)
		}
		return nil
	}
	return m.runPending(o.pending, res)
}

// pushToast shows a toast and logs it.
func (m *Model) pushToast(level msgs.ToastLevel, text string) tea.Cmd {
	var cmd tea.Cmd
	m.toast, cmd = m.toast.Push(level, text)
	if level == msgs.ToastError {
		m.stickyErrors++
		return nil
	}
	return toastTimer(cmd)
}

// dismissToast removes the newest sticky error toast, reporting whether
// there was one.
func (m *Model) dismissToast() bool {
	if m.stickyErrors == 0 {
		return false
	}
	m.toast = m.toast.Dismiss()
	m.stickyErrors--
	return true
}

// overlayBox renders an overlay's box.
func (m *Model) overlayBox(o *overlayState) string {
	switch o.kind {
	case overlayDialog:
		return o.dialog.View()
	case overlayPalette:
		return o.palette.View()
	case overlayHelp:
		return o.help.View()
	case overlayLog:
		return m.logBox(o)
	case overlayFinder:
		return o.finder.View()
	}
	return ""
}

// withOverlay composes the open overlays, bottom first, each centered over
// the dimmed screen below it.
func (m *Model) withOverlay(screen string) string {
	muted := m.opts.Palette.Muted
	dim := func(s string) string { return dialog.DimANSI(s, muted) }
	for _, o := range m.overlays {
		screen = dialog.Overlay(screen, m.overlayBox(o), m.width, m.height, dim)
	}
	return screen
}

// toastMaxWidth is the widest a toast box may be.
const toastMaxWidth = 48

// withToasts draws the visible toasts bottom-right, inside the pane border
// just above the status bar.
func (m *Model) withToasts(screen string) string {
	if !m.toast.Visible() || m.width < 6 || m.height < 3 {
		return screen
	}
	box := m.toast.View(min(toastMaxWidth, m.width-4))
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	x := max(m.width-bw-2, 0)
	y := max(m.height-2-bh, 0)
	return compose(screen, box, x, y, m.width, m.height)
}

// compose draws layer over base at (x, y) and returns exactly w×h cells.
func compose(base, layer string, x, y, w, h int) string {
	canvas := lipgloss.NewCanvas(w, h)
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(base).X(0).Y(0).Z(0),
		lipgloss.NewLayer(layer).X(x).Y(y).Z(1),
	))
	lines := strings.Split(canvas.Render(), "\n")
	return strings.Join(textutil.FitBlock(lines, w, h), "\n")
}
