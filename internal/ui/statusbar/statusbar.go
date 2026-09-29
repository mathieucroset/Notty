// Package statusbar renders Notty's one-row status bar: the editor mode
// pill, the note path, the word count, the sync state (spec §4.3), and the
// help hint.
package statusbar

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

const helpHint = "F1 help"

// Model is the status bar. Set its exported fields, then call View.
type Model struct {
	// Mode is the editor mode label, such as NORMAL or INSERT.
	Mode string
	// Path is the vault-relative path of the open note, or "".
	Path string
	// Words is the open note's word count.
	Words int
	// Sync is the latest sync state.
	Sync msgs.SyncStatusMsg

	styles theme.Styles
	width  int
}

// New returns a status bar styled with styles.
func New(styles theme.Styles) Model {
	return Model{styles: styles, Mode: "NORMAL"}
}

// SetSize sets the width of the bar in columns.
func (m *Model) SetSize(width int) { m.width = width }

// SyncText returns the status-bar text for a sync state (spec §4.3), or ""
// when the state is unknown.
func SyncText(st msgs.SyncStatusMsg) string {
	switch st.State {
	case msgs.SyncSynced:
		return "✓ synced"
	case msgs.SyncSyncing:
		return "↻ syncing…"
	case msgs.SyncOffline:
		return fmt.Sprintf("⊘ offline (%d pending)", st.Pending)
	case msgs.SyncConflict:
		if st.Conflicts == 1 {
			return "⚠ 1 conflict"
		}
		return fmt.Sprintf("⚠ %d conflicts", st.Conflicts)
	case msgs.SyncError:
		return "✗ sync error"
	case msgs.SyncLocalOnly:
		return "○ local only"
	}
	return ""
}

// modeStyle returns the pill style for mode. Visual variants (V-LINE,
// V-BLOCK) use the VISUAL style; unknown modes fall back to NORMAL.
func (m Model) modeStyle(mode string) lipgloss.Style {
	if s, ok := m.styles.StatusMode[mode]; ok {
		return s
	}
	if strings.HasPrefix(mode, "V-") {
		if s, ok := m.styles.StatusMode["VISUAL"]; ok {
			return s
		}
	}
	return m.styles.StatusMode["NORMAL"]
}

// syncStyle colors the sync text by severity.
func (m Model) syncStyle() lipgloss.Style {
	switch m.Sync.State {
	case msgs.SyncSynced:
		return m.styles.Success
	case msgs.SyncOffline, msgs.SyncConflict:
		return m.styles.Warning
	case msgs.SyncError:
		return m.styles.Error
	}
	return m.styles.StatusMuted
}

func wordsText(n int) string {
	if n == 1 {
		return "· 1 word"
	}
	return fmt.Sprintf("· %d words", n)
}

// View renders the bar at exactly the configured width.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}
	bar := m.styles.StatusBar
	on := func(s lipgloss.Style) lipgloss.Style { return s.Inherit(bar) }

	mode := m.Mode
	if mode == "" {
		mode = "NORMAL"
	}
	pill := m.modeStyle(mode).Render(mode)
	left := pill + on(lipgloss.NewStyle()).Render(" ")
	leftW := lipgloss.Width(left)

	syncText := SyncText(m.Sync)
	showSync, showHelp := syncText != "", true

	// right builds the right-hand segment for the current choices.
	right := func() (string, int) {
		var parts []string
		if showSync {
			parts = append(parts, on(m.syncStyle()).Render(syncText))
		}
		if showHelp {
			parts = append(parts, on(m.styles.StatusMuted).Render(helpHint))
		}
		if len(parts) == 0 {
			return "", 0
		}
		s := strings.Join(parts, on(lipgloss.NewStyle()).Render("   ")) + on(lipgloss.NewStyle()).Render(" ")
		return s, lipgloss.Width(s)
	}

	// Drop the help hint, then the sync text, until at least a short path
	// (or, with no path, a one-column gap) fits. Conflict and error states
	// need attention, so their text stays and the path gives way instead.
	urgent := m.Sync.State == msgs.SyncConflict || m.Sync.State == msgs.SyncError
	minMiddle := 1
	if m.Path != "" {
		minMiddle = min(ansi.StringWidth(m.Path), 8) + 1
	}
	var r string
	var rw int
	fits := func() bool {
		r, rw = right()
		return leftW+rw+minMiddle <= m.width
	}
	if !fits() {
		showHelp = false
	}
	if !fits() {
		if urgent {
			minMiddle = 0
		} else {
			showSync = false
		}
	}
	fits()

	// Middle: path and word count, shortened to fit. A one-column gap
	// always separates it from the right segment.
	avail := m.width - leftW - rw - 1
	middle := ""
	if m.Path != "" && avail > 0 {
		full := m.Path + " " + wordsText(m.Words)
		switch {
		case ansi.StringWidth(full) <= avail:
			middle = on(m.styles.StatusText).Render(m.Path) +
				on(m.styles.StatusMuted).Render(" "+wordsText(m.Words))
		default:
			path := m.Path
			if w := ansi.StringWidth(path); w > avail {
				// Keep the end of the path (the file name) and mark the cut.
				path = ansi.TruncateLeft(path, w-avail+1, "…")
			}
			middle = on(m.styles.StatusText).Render(path)
		}
	}

	gap := m.width - leftW - lipgloss.Width(middle) - rw
	var line string
	if gap < 0 {
		line = ansi.Truncate(left+middle+r, m.width, "")
	} else {
		line = left + middle + on(lipgloss.NewStyle()).Render(strings.Repeat(" ", gap)) + r
	}
	if w := lipgloss.Width(line); w < m.width {
		line += on(lipgloss.NewStyle()).Render(strings.Repeat(" ", m.width-w))
	}
	return line
}
