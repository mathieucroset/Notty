package imgrender

import (
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Protocol is a way of drawing images in the terminal.
type Protocol int

// Protocols. The constants carry a Proto prefix because the plan's API also
// has functions named HalfBlocks, Sixel and ITerm in this package.
const (
	ProtoOff Protocol = iota
	ProtoHalfBlocks
	ProtoKitty
	ProtoSixel
	ProtoITerm
)

// String returns the config spelling of p ("off", "halfblocks", "kitty",
// "sixel", "iterm").
func (p Protocol) String() string {
	switch p {
	case ProtoOff:
		return "off"
	case ProtoHalfBlocks:
		return "halfblocks"
	case ProtoKitty:
		return "kitty"
	case ProtoSixel:
		return "sixel"
	case ProtoITerm:
		return "iterm"
	}
	return "Protocol(" + strconv.Itoa(int(p)) + ")"
}

// Caps describes what the terminal can draw (spec §6.3).
type Caps struct {
	// Inline is used inside the preview: ProtoKitty, ProtoHalfBlocks or
	// ProtoOff.
	Inline Protocol
	// Viewer is used by the full-screen image viewer: the best of
	// ProtoKitty, ProtoITerm, ProtoSixel, then ProtoHalfBlocks (ProtoOff when
	// images are configured off).
	Viewer Protocol
	// TmuxPassthrough reports that we run inside tmux with
	// allow-passthrough enabled; Kitty, Sixel and iTerm2 sequences must then
	// be wrapped with [WrapTmux].
	TmuxPassthrough bool
	// CellW and CellH are the cell size in pixels (default 8x16).
	CellW, CellH int
}

// Terminal queries sent by [Detect].
const (
	kittyQuery   = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"
	cellQuery    = "\x1b[16t"
	da1Query     = "\x1b[c"
	queryTimeout = 100 * time.Millisecond
)

var (
	reKittyOK = regexp.MustCompile("\x1b_G(?:[^;\x1b]*,)?i=31(?:,[^;\x1b]*)?;OK\x1b\\\\")
	reDA1     = regexp.MustCompile("\x1b\\[\\?([0-9;]*)c")
	reCell    = regexp.MustCompile("\x1b\\[6;([0-9]+);([0-9]+)t")
)

// Detect works out the terminal's image capabilities, in the order of spec
// §6.3:
//
//  1. cfgProtocol ("kitty", "sixel", "iterm", "halfblocks", "off"); "auto",
//     "" or anything unknown means detect.
//  2. Environment: TERM=xterm-kitty, KITTY_WINDOW_ID or TERM_PROGRAM=ghostty
//     mean Kitty; TERM_PROGRAM=WezTerm or iTerm.app mean iTerm2 in the viewer
//     (WezTerm also does Sixel).
//  3. If tty is non-nil (the caller has put it in raw mode), a Kitty graphics
//     query, a cell size query (CSI 16 t) and DA1 are written, and replies are
//     read for at most 100ms. DA1 goes last: every terminal answers it, and
//     answers arrive in order, so its reply ends the wait early. A Kitty
//     "OK" means Kitty; a 4 in the DA1 reply means Sixel. The cell size is
//     read even when the protocol is configured.
//
// Inside tmux (TMUX set), run("tmux", "show", "-gv", "allow-passthrough")
// must print "on" or "all"; otherwise everything falls back to half-blocks.
//
// When tty does not implement SetReadDeadline, replies are read by a goroutine
// that may stay blocked in Read after Detect returns; closing tty releases it.
func Detect(cfgProtocol string, env func(string) string, tty io.ReadWriter, run func(name string, args ...string) ([]byte, error)) Caps {
	if env == nil {
		env = func(string) string { return "" }
	}
	caps := Caps{CellW: 8, CellH: 16}

	var kitty, sixel, iterm bool
	cfg := strings.ToLower(strings.TrimSpace(cfgProtocol))
	configured := true
	switch cfg {
	case "off":
		caps.Inline, caps.Viewer = ProtoOff, ProtoOff
		return caps
	case "kitty":
		kitty = true
	case "sixel":
		sixel = true
	case "iterm", "iterm2":
		iterm = true
	case "halfblocks":
	default:
		configured = false
	}

	if !configured {
		term, prog := env("TERM"), env("TERM_PROGRAM")
		switch {
		case term == "xterm-kitty", term == "xterm-ghostty", env("KITTY_WINDOW_ID") != "",
			strings.EqualFold(prog, "ghostty"):
			kitty = true
		case strings.EqualFold(prog, "WezTerm"):
			iterm, sixel = true, true
		case strings.EqualFold(prog, "iTerm.app"):
			iterm = true
		}
	}

	if tty != nil {
		r := queryTTY(tty)
		if !configured {
			kitty = kitty || r.kitty
			sixel = sixel || r.sixel
		}
		if r.cellW > 0 && r.cellH > 0 {
			caps.CellW, caps.CellH = r.cellW, r.cellH
		}
	}

	caps.Inline = ProtoHalfBlocks
	if kitty {
		caps.Inline = ProtoKitty
	}
	switch {
	case kitty:
		caps.Viewer = ProtoKitty
	case iterm:
		caps.Viewer = ProtoITerm
	case sixel:
		caps.Viewer = ProtoSixel
	default:
		caps.Viewer = ProtoHalfBlocks
	}

	if env("TMUX") != "" {
		if run != nil {
			out, err := run("tmux", "show", "-gv", "allow-passthrough")
			v := strings.TrimSpace(string(out))
			caps.TmuxPassthrough = err == nil && (v == "on" || v == "all")
		}
		if !caps.TmuxPassthrough {
			caps.Inline, caps.Viewer = ProtoHalfBlocks, ProtoHalfBlocks
		}
	}
	return caps
}

type ttyReplies struct {
	kitty, sixel bool
	cellW, cellH int
}

func parseReplies(s string) (r ttyReplies, done bool) {
	r.kitty = reKittyOK.MatchString(s)
	if m := reCell.FindStringSubmatch(s); m != nil {
		r.cellH, _ = strconv.Atoi(m[1])
		r.cellW, _ = strconv.Atoi(m[2])
	}
	if m := reDA1.FindStringSubmatch(s); m != nil {
		done = true
		for p := range strings.SplitSeq(m[1], ";") {
			if p == "4" {
				r.sixel = true
			}
		}
	}
	return r, done
}

type readDeadliner interface {
	SetReadDeadline(t time.Time) error
}

// queryTTY writes the capability queries and collects the replies until the
// DA1 answer arrives or queryTimeout passes.
func queryTTY(tty io.ReadWriter) ttyReplies {
	if _, err := io.WriteString(tty, kittyQuery+cellQuery+da1Query); err != nil {
		return ttyReplies{}
	}
	deadline := time.Now().Add(queryTimeout)
	var got strings.Builder
	buf := make([]byte, 256)

	if d, ok := tty.(readDeadliner); ok && d.SetReadDeadline(deadline) == nil {
		defer d.SetReadDeadline(time.Time{}) //nolint:errcheck // best effort reset
		for time.Now().Before(deadline) {
			n, err := tty.Read(buf)
			got.Write(buf[:n])
			if r, done := parseReplies(got.String()); done {
				return r
			}
			if err != nil {
				break
			}
		}
		r, _ := parseReplies(got.String())
		return r
	}

	chunks := make(chan []byte)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		defer close(chunks)
		for {
			b := make([]byte, 256)
			n, err := tty.Read(b)
			if n > 0 {
				select {
				case chunks <- b[:n]:
				case <-stop:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(queryTimeout)
	defer timer.Stop()
	for {
		select {
		case b, ok := <-chunks:
			if !ok {
				r, _ := parseReplies(got.String())
				return r
			}
			got.Write(b)
			if r, done := parseReplies(got.String()); done {
				return r
			}
		case <-timer.C:
			r, _ := parseReplies(got.String())
			return r
		}
	}
}
