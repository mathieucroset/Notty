package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/ui/dialog"
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
)

// overlayState is the one overlay open over the screen. Opening another
// overlay replaces it, so at most one is ever active.
type overlayState struct {
	kind overlayKind

	dialog dialog.Model
	// pending says what to do with the dialog's result.
	pending pendingOp

	palette palette.Model
	help    help.Model
	log     toast.LogView
}

// toastTimer wraps the expiry command of an info or warning toast. Tests
// replace it to keep the 4s ticks out of their synchronous command loop.
var toastTimer = func(c tea.Cmd) tea.Cmd { return c }

// overlayOpen reports whether an overlay is open.
func (m *Model) overlayOpen() bool { return m.overlay != nil }

// openDialog shows d as the active overlay; its result is handled by op.
func (m *Model) openDialog(d dialog.Model, op pendingOp) {
	m.overlay = &overlayState{kind: overlayDialog, dialog: d, pending: op}
}

// closeOverlay closes the active overlay.
func (m *Model) closeOverlay() { m.overlay = nil }

// updateOverlay routes a key press to the active overlay.
func (m *Model) updateOverlay(k tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	switch m.overlay.kind {
	case overlayDialog:
		m.overlay.dialog, cmd = m.overlay.dialog.Update(k)
	case overlayPalette:
		m.overlay.palette, cmd = m.overlay.palette.Update(k)
	case overlayHelp:
		m.overlay.help, cmd = m.overlay.help.Update(k)
	case overlayLog:
		m.overlay.log, cmd = m.overlay.log.Update(k)
	}
	return cmd
}

// handleDialogResult closes the dialog that produced res and acts on it.
// A result from a dialog that is no longer open is dropped.
func (m *Model) handleDialogResult(res dialog.ResultMsg) tea.Cmd {
	if m.overlay == nil || m.overlay.kind != overlayDialog || m.overlay.dialog.ID() != res.ID {
		return nil
	}
	op := m.overlay.pending
	m.closeOverlay()
	if !res.OK {
		return nil
	}
	return m.runPending(op, res)
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

// overlayBox renders the active overlay's box.
func (m *Model) overlayBox() string {
	switch m.overlay.kind {
	case overlayDialog:
		return m.overlay.dialog.View()
	case overlayPalette:
		return m.overlay.palette.View()
	case overlayHelp:
		return m.overlay.help.View()
	case overlayLog:
		return m.logBox()
	}
	return ""
}

// withOverlay composes the active overlay, centered, over the dimmed
// screen.
func (m *Model) withOverlay(screen string) string {
	if m.overlay == nil {
		return screen
	}
	muted := m.opts.Palette.Muted
	dim := func(s string) string { return dialog.DimANSI(s, muted) }
	return dialog.Overlay(screen, m.overlayBox(), m.width, m.height, dim)
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
