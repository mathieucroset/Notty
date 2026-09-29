package wizard

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/setup"
)

const (
	// cardMaxWidth is the widest the wizard card gets, borders included.
	cardMaxWidth = 72
	// cardChrome is the horizontal space the border and padding take.
	cardChrome = 2 + 2*cardPadX
	cardPadX   = 3
)

// View renders the wizard full screen: a centered card with a step
// indicator. It always fits in the size given to SetSize.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	outer := min(cardMaxWidth, m.w)
	padX := cardPadX
	if outer < 40 {
		padX = 1
	}
	inner := max(1, outer-2-2*padX)
	padY := 1
	if m.h < 20 {
		padY = 0
	}

	var b strings.Builder
	b.WriteString(m.styles.DialogTitle.Render(m.title()))
	b.WriteString("\n")
	b.WriteString(m.indicator())
	b.WriteString("\n\n")
	b.WriteString(m.body(inner))
	b.WriteString("\n\n")
	b.WriteString(m.wrap(m.styles.Muted, m.footer(), inner))

	content := padLines(fitWidth(b.String(), inner), inner)
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.PaneBorder.GetBorderTopForeground()).
		Padding(padY, padX).
		Render(content)
	return fit(lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, card), m.w, m.h)
}

func (m Model) title() string {
	if m.mode == SetupSync {
		return "Set up sync"
	}
	return "Welcome to Notty"
}

// indicator renders "1 Vault · 2 Sync · 3 Theme" with the current step in
// the accent color.
func (m Model) indicator() string {
	cur := 1
	switch m.stage {
	case StageVault:
		cur = 0
	case StageTheme:
		cur = 2
	}
	names := []string{"1 Vault", "2 Sync", "3 Theme"}
	if m.mode == SetupSync {
		names = names[:2]
	}
	parts := make([]string, len(names))
	for i, n := range names {
		if i == cur {
			parts[i] = m.styles.Accent.Bold(true).Render(n)
		} else {
			parts[i] = m.styles.Muted.Render(n)
		}
	}
	return strings.Join(parts, m.styles.Muted.Render(" · "))
}

func (m Model) body(w int) string {
	switch m.stage {
	case StageVault:
		return m.vaultView(w)
	case StageSync:
		return m.syncView(w)
	case StageIdentity:
		return m.identityView(w)
	case StageRun:
		return m.runView(w)
	case StageTheme:
		return m.themeView(w)
	}
	return ""
}

func (m Model) identityView(w int) string {
	lines := []string{
		m.wrap(m.styles.StatusText, textIdentity, w), "",
		m.styles.StatusText.Render("Name"), m.run.nameInput.View(),
		m.styles.StatusText.Render("Email"), m.run.emailInput.View(),
	}
	switch {
	case m.run.settingIdentity:
		lines = append(lines, "", "  "+m.spinner.View()+" "+m.styles.Muted.Render("Saving…"))
	case m.run.identityErr != "":
		lines = append(lines, "", m.note(m.styles.Error, m.run.identityErr, w))
	default:
		lines = append(lines, "", m.note(m.styles.Muted, "Saved in your global git config (user.name, user.email).", w))
	}
	return strings.Join(lines, "\n")
}

// maxErrorLines caps the raw error text shown after a failed run.
const maxErrorLines = 4

func (m Model) runView(w int) string {
	var lines []string
	title := "Setting up your vault"
	if m.mode == SetupSync {
		title = "Setting up sync"
	}
	lines = append(lines, m.styles.StatusText.Render(title), "")
	if len(m.run.steps) == 0 && (m.run.phase == phaseChecking || m.run.phase == phasePreparing) {
		lines = append(lines, m.spinner.View()+" "+m.styles.Muted.Render("Getting ready…"))
	}
	for i, s := range m.run.steps {
		var marker, text string
		switch {
		case i == m.run.failed:
			marker, text = m.styles.Error.Render(m.styles.Icons.Error), m.styles.Error.Render(s.Desc)
		case i < m.run.current || m.run.phase == phaseDone:
			marker, text = m.styles.Success.Render(m.styles.Icons.Check), m.styles.StatusText.Render(s.Desc)
		case i == m.run.current && m.run.phase == phaseRunning:
			marker, text = m.spinner.View(), m.styles.Accent.Render(s.Desc)
		default:
			marker, text = m.styles.Muted.Render(m.styles.Icons.Pending), m.styles.Muted.Render(s.Desc)
		}
		lines = append(lines, marker+" "+ansi.Truncate(text, max(1, w-2), "…"))
	}
	if m.run.phase == phaseFailed && m.run.runErr != nil {
		headline, hint, detail := runErrorLines(m.run.runErr)
		lines = append(lines, "", m.wrap(m.styles.Error.Bold(true), headline, w))
		if hint != "" {
			lines = append(lines, m.wrap(m.styles.Error, hint, w))
		}
		d := strings.Split(ansi.Wrap(strings.TrimSpace(detail), w, ""), "\n")
		if len(d) > maxErrorLines {
			d = append(d[:maxErrorLines-1], "…")
		}
		lines = append(lines, m.styles.Muted.Render(strings.Join(d, "\n")))
	}
	return strings.Join(lines, "\n")
}

func (m Model) footer() string {
	switch m.stage {
	case StageVault:
		if m.confirmQuit {
			return "y quit · n stay"
		}
		return "enter continue · esc quit"
	case StageSync:
		back := "esc back"
		if m.mode == SetupSync && m.sub == subList {
			back = "esc cancel"
		}
		if m.sub == subList {
			return "↑/↓ choose · enter select · " + back
		}
		return "enter continue · " + back
	case StageIdentity:
		return "tab next field · enter continue · esc back"
	case StageRun:
		if m.run.phase == phaseFailed {
			return "r retry · esc back"
		}
		return "working…"
	case StageTheme:
		return "↑/↓ preview · enter finish · esc back"
	}
	return ""
}

func (m Model) vaultView(w int) string {
	var lines []string
	lines = append(lines, m.styles.StatusText.Render("Where should your notes live?"), "")
	lines = append(lines, m.vaultInput.View())
	if typed, exp := m.vaultPath(), m.expandedVault(); typed != exp {
		lines = append(lines, m.note(m.styles.Muted, exp, w))
	}
	switch {
	case m.vaultErr != "":
		lines = append(lines, m.note(m.styles.Error, m.vaultErr, w))
	case m.hint.ok && m.hint.path == m.expandedVault():
		if m.hint.err != nil {
			lines = append(lines, m.note(m.styles.Error, m.hint.err.Error(), w))
		} else {
			lines = append(lines, m.note(m.styles.Success, vaultHintText(m.hint), w))
		}
	}
	if m.confirmQuit {
		lines = append(lines, "", m.styles.Warning.Bold(true).Render("Quit Notty? y/n"))
	}
	return strings.Join(lines, "\n")
}

func vaultHintText(h vaultHint) string {
	switch h.state {
	case setup.Missing:
		return "New folder — a new vault will be created"
	case setup.Empty:
		return "Empty folder — a new vault will be created"
	case setup.FilesNoRepo:
		switch h.notes {
		case 0:
			return "Folder has files — they'll be kept"
		case 1:
			return "Folder has 1 note — it'll be kept"
		}
		return fmt.Sprintf("Folder has %d notes — they'll be kept", h.notes)
	case setup.Repo:
		return "Existing git repo"
	}
	return ""
}

var choiceLabels = [...]string{
	setup.CreateGitHub: "Create a private GitHub repo",
	setup.ExistingURL:  "Use an existing repo URL",
	setup.LocalOnly:    "Local only (set up sync later)",
}

func (m Model) syncView(w int) string {
	var lines []string
	if m.mode == SetupSync {
		lines = append(lines, m.wrap(m.styles.Muted, "Vault "+m.vaultPath(), w))
	}
	lines = append(lines, m.styles.StatusText.Render("How should your notes sync?"), "")
	for c := setup.CreateGitHub; c <= setup.LocalOnly; c++ {
		label := choiceLabels[c]
		selected := c == m.choice
		var line string
		switch {
		case !m.choiceEnabled(c):
			line = "  " + m.styles.Muted.Render(label)
		case selected && m.sub == subList:
			line = m.styles.Accent.Render("› ") + m.styles.Accent.Bold(true).Render(label)
		case selected:
			line = m.styles.Accent.Render("› ") + m.styles.StatusText.Render(label)
		default:
			line = "  " + m.styles.StatusText.Render(label)
		}
		lines = append(lines, line)
		if c == setup.CreateGitHub && !m.ghOK {
			note := textGHMissing
			if !m.ghKnown {
				note = "checking for gh…"
			}
			lines = append(lines, "  "+m.note(m.styles.Muted.Italic(true), note, w-2))
		}
	}
	switch m.sub {
	case subRepo:
		lines = append(lines, "", m.styles.StatusText.Render("Repository name"), m.repoInput.View())
		if m.repoErr != "" {
			lines = append(lines, m.note(m.styles.Error, m.repoErr, w))
		} else {
			lines = append(lines, m.note(m.styles.Muted, "A private repo on your GitHub account", w))
		}
	case subURL:
		lines = append(lines, "", m.styles.StatusText.Render("Repository URL"), m.urlInput.View())
		switch {
		case m.urlErr != "":
			lines = append(lines, m.note(m.styles.Error, m.urlErr, w))
		case m.remote.busy:
			lines = append(lines, "  "+m.spinner.View()+" "+m.styles.Muted.Render("Checking the remote…"))
		case m.remote.done && m.remote.err != nil:
			lines = append(lines, m.note(m.styles.Error, remoteError(m.remote.err), w))
		case m.remote.done && m.remote.state.HasHistory:
			lines = append(lines, m.note(m.styles.Success, "Remote has history — it will be merged into your vault", w))
		case m.remote.done:
			lines = append(lines, m.note(m.styles.Success, "Remote is empty — your notes will be pushed", w))
		}
	}
	return strings.Join(lines, "\n")
}

// wrap renders s in style st, word-wrapped to w columns.
func (m Model) wrap(st lipgloss.Style, s string, w int) string {
	return st.Render(ansi.Wrap(s, w, ""))
}

// note renders s in style st, word-wrapped to w columns with every line
// indented by two spaces.
func (m Model) note(st lipgloss.Style, s string, w int) string {
	lines := strings.Split(ansi.Wrap(s, max(1, w-2), ""), "\n")
	for i, l := range lines {
		lines[i] = "  " + st.Render(l)
	}
	return strings.Join(lines, "\n")
}

// fitWidth truncates every line of s to w columns.
func fitWidth(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			lines[i] = ansi.Truncate(l, w, "…")
		}
	}
	return strings.Join(lines, "\n")
}

// padLines pads every line of s with spaces to w columns, so the card keeps
// the same width from step to step.
func padLines(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if lw := ansi.StringWidth(l); lw < w {
			lines[i] = l + strings.Repeat(" ", w-lw)
		}
	}
	return strings.Join(lines, "\n")
}

// fit truncates s to at most w columns and h lines.
func fit(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	return fitWidth(strings.Join(lines, "\n"), w)
}

// maxCounted caps the note count walk.
const maxCounted = 100000

// countNotes counts the .md files under dir, skipping hidden directories.
func countNotes(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			n++
			if n >= maxCounted {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return n
}
