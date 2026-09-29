package vim

import (
	"strings"
	"unicode"

	"github.com/mathieucroset/notty/internal/buffer"
)

// Search messages (vim's wording).
const (
	msgWrapBottom = "search hit BOTTOM, continuing at TOP"
	msgWrapTop    = "search hit TOP, continuing at BOTTOM"
	msgNoPrevious = "E35: No previous regular expression"
)

// startCmdline enters command mode with prefix ":", "/" or "?".
func (m *Machine) startCmdline(prefix string) {
	m.cmdPrev = m.mode
	m.mode = Command
	m.cmdPrefix = prefix
	m.cmdText = ""
}

// leaveCmdline returns to the mode the command line was opened from.
func (m *Machine) leaveCmdline() {
	m.mode = m.cmdPrev
	m.cmdPrefix, m.cmdText = "", ""
}

// CommandLine returns the command line including its prefix (":", "/" or
// "?") while in command mode, else "".
func (m *Machine) CommandLine() string {
	if m.mode != Command {
		return ""
	}
	return m.cmdPrefix + m.cmdText
}

// commandKey handles a key while typing a ":" command or a search.
func (m *Machine) commandKey(b *buffer.Buffer, k Key) {
	switch tok := specialToken(k); tok {
	case "<esc>", "<c-c>":
		m.leaveCmdline()
	case "<cr>":
		prefix, text := m.cmdPrefix, m.cmdText
		m.leaveCmdline()
		if prefix == ":" {
			m.runEx(b, text)
		} else {
			m.doSearch(b, text, prefix == "/")
		}
		if m.mode == Normal || m.isVisual() {
			m.clampNormal(b)
		}
	case "<bs>":
		if m.cmdText == "" {
			m.leaveCmdline()
			return
		}
		gs := buffer.Graphemes(m.cmdText)
		m.cmdText = strings.Join(gs[:len(gs)-1], "")
	case "<c-u>":
		m.cmdText = ""
	case "<space>":
		m.cmdText += " "
	default:
		if isPrintable(k) {
			m.cmdText += k.Text
		}
	}
}

// doSearch runs /pat or ?pat. An empty pattern reuses the last one.
func (m *Machine) doSearch(b *buffer.Buffer, pat string, fwd bool) {
	if pat != "" {
		m.lastSearch = pat
	}
	m.lastSearchFwd = fwd
	r := m.searchMotion(b, b.Cursor(), false, 1)
	if r.ok {
		b.SetCursor(r.pos)
		m.curswant = r.pos.Col
	}
}

// searchMotion is n (reverse=false) or N: the count-th match of the last
// search in its direction (or the opposite one for N).
func (m *Machine) searchMotion(b *buffer.Buffer, p buffer.Pos, reverse bool, count int) motionRes {
	if m.lastSearch == "" {
		m.eff.Message = msgNoPrevious
		return motionRes{}
	}
	fwd := m.lastSearchFwd != reverse
	q := p
	wrapped := false
	for ; count > 0; count-- {
		next, found, w := searchFind(b, q, m.lastSearch, fwd)
		if !found {
			m.eff.Message = "Pattern not found: " + m.lastSearch
			return motionRes{}
		}
		q, wrapped = next, wrapped || w
	}
	if wrapped {
		m.eff.Message = msgWrapTop
		if fwd {
			m.eff.Message = msgWrapBottom
		}
	}
	return motionRes{pos: q, kind: exclusive, ok: true}
}

// smartCase reports whether a search for pat ignores case: only when the
// pattern has no uppercase letter.
func smartCase(pat string) bool {
	return !strings.ContainsFunc(pat, unicode.IsUpper)
}

// matchCols returns the grapheme columns where pat (as runes, lowered when
// fold) starts in line.
func matchCols(line string, pat []rune, fold bool) []int {
	var rs []rune
	var offs []int // byte offset of rs[i] in line
	for off, r := range line {
		if fold {
			r = unicode.ToLower(r)
		}
		rs = append(rs, r)
		offs = append(offs, off)
	}
	var cols []int
	for i := 0; i+len(pat) <= len(rs); i++ {
		match := true
		for j, r := range pat {
			if rs[i+j] != r {
				match = false
				break
			}
		}
		if match {
			c := buffer.ByteToCol(line, offs[i])
			if len(cols) == 0 || cols[len(cols)-1] != c {
				cols = append(cols, c)
			}
		}
	}
	return cols
}

// searchFind finds the next match of pat after p (before p when !fwd),
// wrapping around the buffer. It reports whether it wrapped.
func searchFind(b *buffer.Buffer, p buffer.Pos, pat string, fwd bool) (buffer.Pos, bool, bool) {
	fold := smartCase(pat)
	pr := []rune(pat)
	if fold {
		for i, r := range pr {
			pr[i] = unicode.ToLower(r)
		}
	}
	n := b.LineCount()
	for i := 0; i <= n; i++ {
		var l int
		var wrapped bool
		if fwd {
			l, wrapped = (p.Line+i)%n, p.Line+i >= n
		} else {
			l, wrapped = ((p.Line-i)%n+n)%n, p.Line-i < 0
		}
		cols := matchCols(b.Line(l), pr, fold)
		if !fwd {
			for a, z := 0, len(cols)-1; a < z; a, z = a+1, z-1 {
				cols[a], cols[z] = cols[z], cols[a]
			}
		}
		for _, c := range cols {
			switch {
			case i == 0 && fwd && c <= p.Col, i == 0 && !fwd && c >= p.Col:
				continue // not past the cursor yet
			case i == n && fwd && c > p.Col, i == n && !fwd && c < p.Col:
				continue // back on the cursor line after wrapping: only up to the cursor
			}
			return pos(l, c), true, wrapped
		}
	}
	return p, false, false
}
