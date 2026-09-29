package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Styles holds every lipgloss.Style Notty's UI components use, all built
// from a single Palette's semantic tokens.
type Styles struct {
	// Panes.
	PaneBorder        lipgloss.Style
	PaneBorderFocused lipgloss.Style
	PaneTitle         lipgloss.Style
	PaneTitleFocused  lipgloss.Style

	// Status bar.
	StatusBar   lipgloss.Style
	StatusMode  map[string]lipgloss.Style
	StatusText  lipgloss.Style
	StatusMuted lipgloss.Style

	// Sidebar.
	SidebarSection         lipgloss.Style
	SidebarItem            lipgloss.Style
	SidebarSelected        lipgloss.Style
	SidebarSelectedFocused lipgloss.Style
	SidebarDim             lipgloss.Style

	// General-purpose.
	Muted  lipgloss.Style
	Accent lipgloss.Style
	Chip   lipgloss.Style

	// Dialogs and overlays.
	Dialog      lipgloss.Style
	DialogTitle lipgloss.Style
	Overlay     lipgloss.Style

	// Toasts.
	ToastInfo  lipgloss.Style
	ToastWarn  lipgloss.Style
	ToastError lipgloss.Style

	// Text ranges.
	Selection lipgloss.Style
	Match     lipgloss.Style

	// Semantic text.
	Error   lipgloss.Style
	Warning lipgloss.Style
	Success lipgloss.Style
}

// pillStyle builds a bold, padded "pill" style with the given background and
// text colored from the base token, used for status-bar mode indicators and
// toasts.
func pillStyle(bg, fg color.Color) lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Background(bg).
		Foreground(fg).
		Padding(0, 1)
}

// NewStyles builds a Styles value from p. Every field is derived only from
// p's semantic tokens, which are always non-nil for a Palette returned by
// Get, so this never panics.
func NewStyles(p Palette) Styles {
	border := lipgloss.RoundedBorder()

	modeColors := map[string]color.Color{
		"NORMAL":    p.Accent,
		"INSERT":    p.Success,
		"VISUAL":    p.Accent2,
		"COMMAND":   p.Warning,
		"PLAIN":     p.Muted,
		"READ-ONLY": p.Error,
	}
	statusMode := make(map[string]lipgloss.Style, len(modeColors))
	for mode, c := range modeColors {
		statusMode[mode] = pillStyle(c, p.Base)
	}

	return Styles{
		PaneBorder: lipgloss.NewStyle().
			Border(border).
			BorderForeground(p.Muted),
		PaneBorderFocused: lipgloss.NewStyle().
			Border(border).
			BorderForeground(p.Accent),
		PaneTitle: lipgloss.NewStyle().
			Foreground(p.Subtext),
		PaneTitleFocused: lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true),

		StatusBar: lipgloss.NewStyle().
			Background(p.Surface).
			Foreground(p.Text),
		StatusMode: statusMode,
		StatusText: lipgloss.NewStyle().
			Foreground(p.Text),
		StatusMuted: lipgloss.NewStyle().
			Foreground(p.Muted),

		SidebarSection: lipgloss.NewStyle().
			Foreground(p.Subtext).
			Bold(true),
		SidebarItem: lipgloss.NewStyle().
			Foreground(p.Text),
		SidebarSelected: lipgloss.NewStyle().
			Background(p.Overlay).
			Foreground(p.Text),
		SidebarSelectedFocused: lipgloss.NewStyle().
			Background(p.Overlay).
			Foreground(p.Accent).
			Bold(true),
		SidebarDim: lipgloss.NewStyle().
			Foreground(p.Muted),

		Muted:  lipgloss.NewStyle().Foreground(p.Muted),
		Accent: lipgloss.NewStyle().Foreground(p.Accent),
		Chip: lipgloss.NewStyle().
			Background(p.Surface).
			Foreground(p.Accent2).
			Padding(0, 1),

		Dialog: lipgloss.NewStyle().
			Border(border).
			BorderForeground(p.Accent).
			Background(p.Surface).
			Padding(1, 2),
		DialogTitle: lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true),
		Overlay: lipgloss.NewStyle().
			Background(p.Overlay),

		ToastInfo:  pillStyle(p.Accent2, p.Base),
		ToastWarn:  pillStyle(p.Warning, p.Base),
		ToastError: pillStyle(p.Error, p.Base),

		Selection: lipgloss.NewStyle().
			Background(p.Overlay),
		Match: lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true),

		Error:   lipgloss.NewStyle().Foreground(p.Error),
		Warning: lipgloss.NewStyle().Foreground(p.Warning),
		Success: lipgloss.NewStyle().Foreground(p.Success),
	}
}
