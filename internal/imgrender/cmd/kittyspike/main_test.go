//go:build spike

package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// syncBuffer is a goroutine-safe bytes.Buffer used as program output.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Len()
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// runSpike runs the spike program m headless, sends pre right after start,
// then presses each key in turn followed by "q". It returns the raw output
// produced before the first key, after each key, and while quitting.
func runSpike(t *testing.T, m model, profile colorprofile.Profile, pre []tea.Msg, keys ...rune) []string {
	t.Helper()
	out := &syncBuffer{}
	p := tea.NewProgram(m,
		tea.WithOutput(out),
		tea.WithInput(nil),
		tea.WithWindowSize(80, 24),
		tea.WithColorProfile(profile),
		tea.WithoutSignals(),
		tea.WithEnvironment([]string{"TERM=xterm-kitty"}),
	)
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	for _, msg := range pre {
		p.Send(msg)
	}

	// waitStable waits until the output has not grown for well over
	// readyDelay, so gated transmissions are included.
	waitStable := func() string {
		prev, stable := -1, 0
		for stable < 25 {
			time.Sleep(10 * time.Millisecond)
			if n := out.Len(); n == prev && n > 0 {
				stable++
			} else {
				prev, stable = n, 0
			}
		}
		return out.String()
	}
	var chunks []string
	seen := 0
	var last rune
	for _, k := range append(keys, 'q') {
		if last == 'e' {
			// With no input reader, releasing the terminal for tea.Exec
			// waits 500ms for the read loop; wait for the round trip.
			deadline := time.Now().Add(3 * time.Second)
			for !strings.Contains(out.String()[seen:], enterAlt) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
		}
		last = k
		s := waitStable()
		chunks = append(chunks, s[seen:])
		seen = len(s)
		p.Send(tea.KeyPressMsg{Code: k, Text: string(k)})
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("program did not quit")
	}
	return append(chunks, out.String()[seen:])
}

// cell is one screen cell of the tiny emulator below.
type cell struct {
	content string
	fg      string // SGR foreground parameters as emitted, "" for default
}

// screen is a deliberately small VT emulator: enough cursor movement, erase
// and SGR handling to replay what the Bubble Tea renderer emits, and strict
// about anything it does not understand so the test notices.
type screen struct {
	w, h    int
	x, y    int
	fg      string
	cells   [][]cell
	unknown []string
	apcs    []string
	last    string // last printed grapheme, for REP
	lastW   int
	reps    []string // graphemes that were repeated with REP
}

func newScreen(w, h int) *screen {
	s := &screen{w: w, h: h}
	s.cells = make([][]cell, h)
	for i := range s.cells {
		s.cells[i] = make([]cell, w)
	}
	return s
}

func (s *screen) put(g string, width int) {
	s.last, s.lastW = g, width
	if s.y < s.h && s.x < s.w {
		s.cells[s.y][s.x] = cell{content: g, fg: s.fg}
		for i := 1; i < width && s.x+i < s.w; i++ {
			s.cells[s.y][s.x+i] = cell{}
		}
	}
	s.x += width
}

func (s *screen) erase(y, from, to int) {
	for x := max(from, 0); x < min(to, s.w); x++ {
		s.cells[y][x] = cell{content: " "}
	}
}

func (s *screen) sgr(params []int) {
	if len(params) == 0 {
		s.fg = ""
		return
	}
	for i := 0; i < len(params); i++ {
		switch p := params[i]; {
		case p == 0:
			s.fg = ""
		case p == 39:
			s.fg = ""
		case p >= 30 && p <= 37, p >= 90 && p <= 97:
			s.fg = strconv.Itoa(p)
		case p == 38 && i+1 < len(params) && params[i+1] == 5 && i+2 < len(params):
			s.fg = fmt.Sprintf("38;5;%d", params[i+2])
			i += 2
		case p == 38 && i+1 < len(params) && params[i+1] == 2 && i+4 < len(params):
			s.fg = fmt.Sprintf("38;2;%d;%d;%d", params[i+2], params[i+3], params[i+4])
			i += 4
		case p == 48 || p == 58:
			if i+1 < len(params) && params[i+1] == 5 {
				i += 2
			} else if i+1 < len(params) && params[i+1] == 2 {
				i += 4
			}
		}
	}
}

func (s *screen) feed(t *testing.T, data string) {
	t.Helper()
	p := ansi.NewParser()
	var state byte
	for len(data) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(data, state, p)
		state = newState
		data = data[n:]
		switch {
		case width > 0:
			s.put(seq, width)
		case seq == "\r":
			s.x = 0
		case seq == "\n":
			s.y = min(s.y+1, s.h-1)
		case seq == "\b":
			s.x = max(s.x-1, 0)
		case ansi.HasApcPrefix(seq):
			s.apcs = append(s.apcs, seq)
		case ansi.HasCsiPrefix(seq):
			s.csi(p, seq)
		case ansi.HasOscPrefix(seq), ansi.HasDcsPrefix(seq), ansi.HasEscPrefix(seq):
			// titles, modes, charsets: irrelevant here
		case seq == "\x07":
		default:
			if strings.TrimSpace(seq) != "" {
				s.unknown = append(s.unknown, fmt.Sprintf("%q", seq))
			}
		}
	}
}

func (s *screen) csi(p *ansi.Parser, seq string) {
	cmd := ansi.Cmd(p.Command())
	params := p.Params()
	arg := func(i, def int) int {
		if i < len(params) {
			if v := params[i].Param(def); v != 0 {
				return v
			}
		}
		return def
	}
	if cmd.Prefix() == '?' && cmd.Final() == 'h' && params0(params) == 1049 {
		// Entering the alternate screen clears it.
		for y := range s.h {
			s.erase(y, 0, s.w)
		}
		s.x, s.y = 0, 0
		return
	}
	if cmd.Prefix() != 0 || cmd.Intermediate() != 0 {
		return // other private modes (DECSET etc.), kitty keyboard, ...
	}
	switch cmd.Final() {
	case 'H', 'f':
		s.y, s.x = arg(0, 1)-1, arg(1, 1)-1
	case 'A':
		s.y = max(s.y-arg(0, 1), 0)
	case 'B':
		s.y = min(s.y+arg(0, 1), s.h-1)
	case 'C':
		s.x = min(s.x+arg(0, 1), s.w-1)
	case 'D':
		s.x = max(s.x-arg(0, 1), 0)
	case 'G', '`':
		s.x = arg(0, 1) - 1
	case 'd':
		s.y = arg(0, 1) - 1
	case 'E':
		s.y, s.x = min(s.y+arg(0, 1), s.h-1), 0
	case 'F':
		s.y, s.x = max(s.y-arg(0, 1), 0), 0
	case 'X':
		s.erase(s.y, s.x, s.x+arg(0, 1))
	case 'b': // REP: repeat the last graphic character
		s.reps = append(s.reps, s.last)
		g, w := s.last, s.lastW
		for range arg(0, 1) {
			s.put(g, w)
		}
	case 'K':
		switch params0(params) {
		case 0:
			s.erase(s.y, s.x, s.w)
		case 1:
			s.erase(s.y, 0, s.x+1)
		case 2:
			s.erase(s.y, 0, s.w)
		}
	case 'J':
		switch params0(params) {
		case 0:
			s.erase(s.y, s.x, s.w)
			for y := s.y + 1; y < s.h; y++ {
				s.erase(y, 0, s.w)
			}
		case 2, 3:
			for y := range s.h {
				s.erase(y, 0, s.w)
			}
		}
	case 'm':
		var ps []int
		for _, prm := range params {
			ps = append(ps, prm.Param(0))
		}
		s.sgr(ps)
	case 't', 'r', 'n', 'c', 'q', 'u':
		// window ops, scroll region reset, reports: no effect on cells here
	default:
		s.unknown = append(s.unknown, fmt.Sprintf("%q", seq))
	}
}

func params0(params ansi.Params) int {
	if len(params) == 0 {
		return 0
	}
	return params[0].Param(0)
}

// placeholderCells returns every placeholder cell on the screen keyed by
// "row,col" of the image (decoded from the diacritics), with its color.
func (s *screen) placeholderCells(t *testing.T) map[[2]int]cell {
	t.Helper()
	idx := map[rune]int{}
	for i := range 297 {
		idx[kitty.Diacritic(i)] = i
	}
	got := map[[2]int]cell{}
	for y := range s.h {
		for x := range s.w {
			c := s.cells[y][x]
			rs := []rune(c.content)
			if len(rs) == 0 || rs[0] != kitty.Placeholder {
				continue
			}
			if len(rs) != 3 {
				t.Errorf("screen (%d,%d): placeholder cluster has %d runes: %q", x, y, len(rs), c.content)
				continue
			}
			got[[2]int{idx[rs[1]], idx[rs[2]]}] = c
		}
	}
	return got
}

func (s *screen) dump() string {
	var b strings.Builder
	for y := range s.h {
		for x := range s.w {
			c := s.cells[y][x].content
			switch {
			case c == "":
				b.WriteByte('.')
			case []rune(c)[0] == kitty.Placeholder:
				b.WriteByte('#')
			default:
				b.WriteString(c)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

const (
	wantFG      = "38;2;10;11;12" // imageID 0x0A0B0C
	enterAlt    = "\x1b[?1049h"
	leaveAlt    = "\x1b[?1049l"
	deleteImage = "\x1b_Gq=2,i=658188,d=I,a=d\x1b\\"
)

func TestSpikeTrueColor(t *testing.T) {
	tests := []struct {
		name string
		pre  []tea.Msg
	}{
		{name: "default wcwidth renderer"},
		{name: "grapheme width (mode 2027, e.g. ghostty)", pre: []tea.Msg{
			tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet},
		}},
		{name: "synchronized output", pre: []tea.Msg{
			tea.ModeReportMsg{Mode: ansi.ModeSynchronizedOutput, Value: ansi.ModeReset},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { spikeTrueColor(t, tt.pre...) })
	}
}

func spikeTrueColor(t *testing.T, pre ...tea.Msg) {
	m := newModel()
	out := runSpike(t, m, colorprofile.TrueColor, pre, 'o', 'e')
	initial, overlay, afterExec, closing := out[0], out[1], out[2], out[3]

	// (a) tea.Raw delivers the transmit sequence intact and contiguous, and
	// the gate makes it arrive after the renderer entered the alt screen.
	if n := strings.Count(m.transmit, "\x1b_G"); n < 2 {
		t.Fatalf("spike image should need several chunks, got %d", n)
	}
	tx := strings.Index(initial, m.transmit)
	if tx < 0 {
		t.Fatalf("transmit sequence not found intact in output (len %d)", len(m.transmit))
	}
	alt := strings.Index(initial, enterAlt)
	if alt < 0 || alt > tx {
		t.Errorf("transmit at byte %d must follow alt screen entry at byte %d", tx, alt)
	}
	t.Logf("transmit: %d bytes in %d chunks, found intact at byte %d (alt screen entered at %d)",
		len(m.transmit), strings.Count(m.transmit, "\x1b_G"), tx, alt)
	if !strings.Contains(closing, deleteImage) {
		t.Errorf("delete sequence missing on quit: %q", closing)
	}

	scr := newScreen(80, 24)
	scr.feed(t, initial)
	checkFrame(t, scr, "initial", imgRows*imgCols)

	// Overlay composition: the overlay covers the middle of the image; the
	// remaining placeholder cells must keep their diacritics and color.
	scr.feed(t, overlay)
	n := checkFrame(t, scr, "overlay", -1)
	if n == 0 || n >= imgRows*imgCols {
		t.Fatalf("overlay frame: %d placeholder cells, want some covered\n%s", n, scr.dump())
	}
	for _, g := range scr.reps {
		if strings.ContainsRune(g, kitty.Placeholder) {
			t.Errorf("renderer used REP on a placeholder cell %q", g)
		}
	}

	// tea.Exec leaves and re-enters the alt screen, which wipes kitty's
	// alt-screen image store; the spike must re-send the image afterwards.
	l, e, x := strings.Index(afterExec, leaveAlt), strings.LastIndex(afterExec, enterAlt), strings.LastIndex(afterExec, m.transmit)
	t.Logf("exec: leave alt at %d, re-enter at %d, re-transmit at %d", l, e, x)
	if l < 0 || e < l {
		t.Fatalf("expected leave then re-enter alt screen around exec, got leave=%d enter=%d", l, e)
	}
	if x < e {
		t.Errorf("image must be re-transmitted after re-entering the alt screen (transmit=%d, enter=%d)", x, e)
	}
	scr.feed(t, afterExec)
	checkFrame(t, scr, "after exec", -1)
}

// checkFrame validates every placeholder cell on scr and returns how many
// there are; want >= 0 also checks the count.
func checkFrame(t *testing.T, scr *screen, name string, want int) int {
	t.Helper()
	if len(scr.unknown) > 0 {
		t.Fatalf("%s: emulator saw unhandled sequences: %v", name, scr.unknown)
	}
	cells := scr.placeholderCells(t)
	if want >= 0 && len(cells) != want {
		t.Fatalf("%s: %d placeholder cells, want %d\n%s", name, len(cells), want, scr.dump())
	}
	for pos, c := range cells {
		if c.fg != wantFG {
			t.Errorf("%s: cell %v fg=%q, want %q", name, pos, c.fg, wantFG)
		}
	}
	checkLayout(t, scr)
	t.Logf("%s frame (%d placeholder cells):\n%s", name, len(cells), scr.dump())
	return len(cells)
}

// TestSpikeKeyboardReplyGate: kitty and ghostty answer the kitty keyboard
// query the renderer sends right after entering the alt screen; that reply
// releases the transmission without waiting for the timer.
func TestSpikeKeyboardReplyGate(t *testing.T) {
	m := newModel()
	// Delivered right after start: in a real terminal it can only arrive
	// after the first frame, since the query is part of that frame.
	out := runSpike(t, m, colorprofile.TrueColor, []tea.Msg{tea.KeyboardEnhancementsMsg{Flags: 1}})
	if c := strings.Count(out[0], m.transmit); c != 1 {
		t.Fatalf("transmit sent %d times, want exactly once", c)
	}
}

// TestSpikeNaiveInit documents the hazard: tea.Raw from Init is flushed before
// the renderer's first frame, i.e. before the alt screen is entered, so kitty
// would store the image on the main screen and the alt-screen placeholders
// would show nothing.
func TestSpikeNaiveInit(t *testing.T) {
	m := newModel()
	m.naive = true
	out := runSpike(t, m, colorprofile.TrueColor, nil)
	tx, alt := strings.Index(out[0], m.transmit), strings.Index(out[0], enterAlt)
	t.Logf("naive: transmit at byte %d, alt screen entered at byte %d", tx, alt)
	if tx < 0 || alt < 0 || tx > alt {
		t.Fatalf("expected the naive transmit to precede alt screen entry (tx=%d alt=%d)", tx, alt)
	}
}

// TestSpikeANSI256 documents what happens when the renderer believes the
// terminal cannot do truecolor: the id color is downsampled and the image is
// lost. The app must force a TrueColor profile when Kitty is detected.
func TestSpikeANSI256(t *testing.T) {
	out := runSpike(t, newModel(), colorprofile.ANSI256, nil)
	scr := newScreen(80, 24)
	scr.feed(t, out[0])
	cells := scr.placeholderCells(t)
	if len(cells) != imgRows*imgCols {
		t.Fatalf("got %d cells", len(cells))
	}
	for _, c := range cells {
		if c.fg == wantFG {
			t.Fatalf("expected downsampled color under ANSI256, got truecolor %q", c.fg)
		}
		t.Logf("ANSI256 profile: id color %s became %s", wantFG, c.fg)
		break
	}
}

// checkLayout verifies that every visible placeholder sits at the screen
// position implied by its diacritics relative to the image origin.
func checkLayout(t *testing.T, scr *screen) {
	t.Helper()
	ox, oy := -1, -1
	for y := range scr.h {
		for x := range scr.w {
			rs := []rune(scr.cells[y][x].content)
			if len(rs) == 3 && rs[0] == kitty.Placeholder && rs[1] == kitty.Diacritic(0) && rs[2] == kitty.Diacritic(0) {
				ox, oy = x, y
			}
		}
	}
	if ox < 0 {
		t.Fatal("image origin cell (0,0) not found")
	}
	for r := range imgRows {
		for c := range imgCols {
			got := scr.cells[oy+r][ox+c].content
			rs := []rune(got)
			if len(rs) == 0 || rs[0] != kitty.Placeholder {
				continue // covered by the overlay
			}
			want := string([]rune{kitty.Placeholder, kitty.Diacritic(r), kitty.Diacritic(c)})
			if got != want {
				t.Errorf("screen (%d,%d): got %q, want image cell r=%d c=%d", ox+c, oy+r, got, r, c)
			}
		}
	}
}
