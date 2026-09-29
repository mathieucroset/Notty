// Package imageviewer is the full-screen image viewer (spec §6.4). It runs as
// a tea.Exec command: while it runs, Bubble Tea has released the terminal, so
// the viewer owns the tty itself (raw mode, key decoding, resize handling).
package imageviewer

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Key is a viewer action decoded from terminal input.
type Key int

// Keys.
const (
	Unknown Key = iota
	Next
	Prev
	Open
	Quit
)

// String returns the key's name.
func (k Key) String() string {
	switch k {
	case Next:
		return "next"
	case Prev:
		return "prev"
	case Open:
		return "open"
	case Quit:
		return "quit"
	}
	return "unknown"
}

const esc = 0x1b

// DecodeKeys decodes one read of raw terminal input into viewer keys:
//
//   - n, space, arrow right and arrow down: Next
//   - p, backspace, arrow left and arrow up: Prev
//   - o: Open
//   - q, a lone ESC byte, ctrl+c and ctrl+q: Quit
//
// Arrows are accepted in CSI (ESC [ C) and SS3 (ESC O C) forms, with or
// without modifiers; kitty keyboard protocol reports (CSI code u) for these
// keys are decoded too. Several keys per read are handled. Terminal replies
// (other CSI sequences, APC such as kitty's _G replies, OSC and DCS strings)
// are skipped, and so is a truncated escape sequence at the end of b. Any
// other key yields Unknown.
func DecodeKeys(b []byte) []Key {
	var keys []Key
	for i := 0; i < len(b); {
		c := b[i]
		if c != esc {
			r, size := utf8.DecodeRune(b[i:])
			keys = append(keys, plainKey(r))
			i += size
			continue
		}
		// ESC as the last byte, or followed by another ESC, is the esc key.
		if i+1 >= len(b) || b[i+1] == esc {
			keys = append(keys, Quit)
			i++
			continue
		}
		switch b[i+1] {
		case '[':
			k, n, ok := decodeCSI(b[i+2:])
			if ok {
				keys = append(keys, k)
			}
			i += 2 + n
		case 'O':
			if i+2 < len(b) {
				if k, ok := arrowKey(b[i+2]); ok {
					keys = append(keys, k)
				}
				i += 3
			} else {
				i += 2
			}
		case '_', ']', 'P', '^', 'X':
			// APC, OSC, DCS, PM, SOS: skip the string up to ST or BEL.
			i += 2 + skipString(b[i+2:])
		default:
			// Alt+key: not a lone esc, and not a viewer key.
			_, size := utf8.DecodeRune(b[i+1:])
			keys = append(keys, Unknown)
			i += 1 + size
		}
	}
	return keys
}

// plainKey maps a single (non-escape) input rune to a key.
func plainKey(r rune) Key {
	switch r {
	case 'n', ' ':
		return Next
	case 'p', 0x7f, 0x08:
		return Prev
	case 'o':
		return Open
	case 'q', 0x03, 0x11:
		return Quit
	}
	return Unknown
}

// arrowKey maps an arrow final byte (CSI or SS3) to a key.
func arrowKey(final byte) (Key, bool) {
	switch final {
	case 'C', 'B':
		return Next, true
	case 'D', 'A':
		return Prev, true
	}
	return Unknown, false
}

// decodeCSI parses the CSI sequence whose body (after ESC [) starts b. It
// returns the number of body bytes consumed and, when the sequence is a
// viewer key, that key. An unterminated sequence consumes all of b.
func decodeCSI(b []byte) (k Key, n int, ok bool) {
	for n < len(b) && b[n] >= 0x20 && b[n] <= 0x3f {
		n++ // parameter and intermediate bytes
	}
	if n >= len(b) || b[n] < 0x40 || b[n] > 0x7e {
		// Unterminated or malformed: drop the rest of this read.
		if n < len(b) {
			n++
		}
		return Unknown, n, false
	}
	params, final := string(b[:n]), b[n]
	n++
	if final == 'u' {
		k, ok = kittyKey(params)
		return k, n, ok
	}
	if strings.IndexFunc(params, func(r rune) bool { return (r < '0' || r > '9') && r != ';' }) >= 0 {
		return Unknown, n, false // private replies such as CSI ? … c
	}
	k, ok = arrowKey(final)
	return k, n, ok
}

// kittyKey decodes a kitty keyboard protocol report "code[:alt];mods[:event]"
// (CSI … u). Release events are ignored.
func kittyKey(params string) (Key, bool) {
	if params == "" || params[0] < '0' || params[0] > '9' {
		return Unknown, false
	}
	fields := strings.Split(params, ";")
	if len(fields) > 1 {
		if mod := strings.Split(fields[1], ":"); len(mod) > 1 && mod[1] == "3" {
			return Unknown, false
		}
	}
	code, err := strconv.Atoi(strings.Split(fields[0], ":")[0])
	if err != nil {
		return Unknown, false
	}
	if code == esc {
		return Quit, true
	}
	k := plainKey(rune(code))
	return k, true
}

// skipString returns the length of a control string body up to and
// including its terminator (ST = ESC \, or BEL), or len(b) when unterminated.
func skipString(b []byte) int {
	for j := 0; j < len(b); j++ {
		switch b[j] {
		case 0x07:
			return j + 1
		case esc:
			if j+1 < len(b) && b[j+1] == '\\' {
				return j + 2
			}
		}
	}
	return len(b)
}
