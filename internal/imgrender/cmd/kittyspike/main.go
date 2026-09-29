//go:build spike

// Command kittyspike checks that Kitty unicode placeholders work through the
// Bubble Tea v2 renderer (spec §6.4). It transmits a generated gradient with
// tea.Raw, draws the placeholder cells inside a rounded Lip Gloss border and
// toggles a centered overlay layer with "o". "e" runs a no-op tea.Exec round
// trip (like the image viewer or $EDITOR handoff). Quit with "q".
//
// Kitty keeps separate image stores for the main and the alternate screen and
// clears the alternate one whenever it is entered (DECSET 1049). The renderer
// enters the alternate screen in its first frame, after flushing any tea.Raw
// output queued in the same tick, and leaves/re-enters it around tea.Exec. So
// the spike gates the transmission: it is sent only once the renderer is known
// to be on the alternate screen (see readyMsg), and re-sent after every exec.
// Pass -naive to send it straight from Init to observe the problem.
//
//	go run -tags spike ./internal/imgrender/cmd/kittyspike
//	kitty --hold -e go run -tags spike ./internal/imgrender/cmd/kittyspike
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/imgrender"
)

const (
	imageID  = 0x0A0B0C // exercises all three color bytes
	imgCols  = 20
	imgRows  = 8
	overlayW = 14
	overlayH = 3

	// readyDelay is how long after start (or after an exec) the transmission
	// waits when the terminal never answers the kitty keyboard query. Several
	// frames at the default 60 fps.
	readyDelay = 100 * time.Millisecond
)

// readyMsg reports that the renderer has flushed at least one frame since the
// last (re)start, so the terminal is on the alternate screen. gen ties it to
// the start it was scheduled for.
type readyMsg struct{ gen int }

type execDoneMsg struct{}

type model struct {
	width, height int
	overlay       bool
	transmit      string
	placeholders  []string
	naive         bool
	gen           int  // incremented on every (re)start of the renderer
	sent          bool // transmit sent for the current gen
}

func newModel() model {
	return model{
		width:        80,
		height:       24,
		transmit:     imgrender.KittyTransmit(gradient(160, 128), imageID, imgCols, imgRows),
		placeholders: imgrender.KittyPlaceholders(imageID, imgCols, imgRows),
	}
}

// gradient returns a w x h image: red grows left to right, blue top to
// bottom; the green x*y pattern keeps the PNG large enough to need chunks.
func gradient(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(x * y), uint8(y * 255 / h), 255})
		}
	}
	return img
}

func (m model) Init() tea.Cmd {
	if m.naive {
		return tea.Raw(m.transmit)
	}
	return m.scheduleReady()
}

func (m model) scheduleReady() tea.Cmd {
	gen := m.gen
	return tea.Tick(readyDelay, func(time.Time) tea.Msg { return readyMsg{gen: gen} })
}

// sendTransmit sends the image once per renderer start.
func (m model) sendTransmit() (model, tea.Cmd) {
	if m.sent || m.naive {
		return m, nil
	}
	m.sent = true
	return m, tea.Raw(m.transmit)
}

// noopExec stands in for the image viewer or $EDITOR: it does nothing, but
// Bubble Tea still releases and restores the terminal around it.
type noopExec struct{}

func (noopExec) Run() error          { return nil }
func (noopExec) SetStdin(io.Reader)  {}
func (noopExec) SetStdout(io.Writer) {}
func (noopExec) SetStderr(io.Writer) {}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyboardEnhancementsMsg:
		// The renderer queries kitty keyboard flags in the same write that
		// enters the alternate screen, so the reply proves we are there.
		// Only the first start queries; restarts rely on readyMsg.
		return m.sendTransmit()
	case readyMsg:
		if msg.gen == m.gen {
			return m.sendTransmit()
		}
	case execDoneMsg:
		// Re-entering the alternate screen wiped kitty's image store.
		m.gen++
		m.sent = false
		return m, m.scheduleReady()
	case tea.KeyPressMsg:
		switch msg.String() {
		case "e":
			return m, tea.Exec(noopExec{}, func(error) tea.Msg { return execDoneMsg{} })
		case "o":
			m.overlay = !m.overlay
		case "q", "ctrl+c":
			return m, tea.Sequence(tea.Raw(imgrender.KittyDelete(imageID)), tea.Quit)
		}
	}
	return m, nil
}

// box draws the placeholder rows inside a rounded border. The border style
// sets a foreground color only on the border; the placeholder rows are joined
// by plain concatenation so their id color is untouched (spec §6.4).
func (m model) box() string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(0, 1)
	return style.Render(strings.Join(m.placeholders, "\n"))
}

func (m model) content() string {
	base := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		lipgloss.JoinVertical(lipgloss.Center,
			"kitty spike: o overlay, q quit", m.box()))
	if !m.overlay {
		return base
	}
	ov := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(overlayW).Height(overlayH).
		Render("overlay")
	x := max(0, (m.width-lipgloss.Width(ov))/2)
	y := max(0, (m.height-lipgloss.Height(ov))/2)
	comp := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(ov).X(x).Y(y).Z(1),
	)
	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(comp)
	return canvas.Render()
}

func (m model) View() tea.View {
	v := tea.NewView(m.content())
	v.AltScreen = true
	return v
}

func main() {
	naive := flag.Bool("naive", false, "send the transmission from Init (before the alternate screen)")
	flag.Parse()
	m := newModel()
	m.naive = *naive
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "kittyspike:", err)
		os.Exit(1)
	}
}
