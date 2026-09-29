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
	// Quit closes the viewer.
	Quit
	// QuitApp (ctrl+q) closes the viewer and asks the app to quit (spec
	// §4.4: ctrl+q passes through full-screen views).
	QuitApp
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
	case QuitApp:
		return "quit-app"
	}
	return "unknown"
}

const esc = 0x1b

// DecodeKeys decodes one read of raw terminal input into viewer keys:
//
//   - n, space, arrow right and arrow down: Next
//   - p, backspace, arrow left and arrow up: Prev
//   - o: Open
//   - q, a lone ESC byte and ctrl+c: Quit
//   - ctrl+q: QuitApp
//
// Arrows are accepted in CSI (ESC [ C) and SS3 (ESC O C) forms, with or
// without modifiers; kitty keyboard protocol reports (CSI code;mods u) for
// these keys are decoded too, honoring the ctrl modifier. Several keys per
// read are handled. Terminal replies (other CSI sequences, APC such as
// kitty's _G replies, OSC and DCS strings) are skipped, and so is a
// truncated escape sequence at the end of b. Any other key yields Unknown.
func DecodeKeys(b []byte) []Key {
	keys, _ := decode(b, true)
	return keys
}

// decode decodes b like [DecodeKeys]. When final is false, an incomplete
// sequence at the end of b (a trailing ESC, an unterminated CSI, SS3 or
// control string, a partial UTF-8 rune) is left undecoded and its length is
// returned as rest: it may be the start of a sequence split across reads,
// so the reader prepends it to the next read, or decodes it with final set
// when no more input comes shortly.
func decode(b []byte, final bool) (keys []Key, rest int) {
	for i := 0; i < len(b); {
		c := b[i]
		if c != esc {
			if !final && !utf8.FullRune(b[i:]) {
				return keys, len(b) - i
			}
			r, size := utf8.DecodeRune(b[i:])
			keys = append(keys, plainKey(r))
			i += size
			continue
		}
		if i+1 >= len(b) {
			if !final {
				return keys, 1
			}
			keys = append(keys, Quit) // lone ESC: the esc key
			i++
			continue
		}
		switch b[i+1] {
		case esc:
			keys = append(keys, Quit) // ESC followed by ESC
			i++
		case '[':
			k, n, ok, complete := decodeCSI(b[i+2:])
			if !complete && !final {
				return keys, len(b) - i
			}
			if ok {
				keys = append(keys, k)
			}
			i += 2 + n
		case 'O':
			if i+2 >= len(b) {
				if !final {
					return keys, len(b) - i
				}
				i += 2
				continue
			}
			if k, ok := arrowKey(b[i+2]); ok {
				keys = append(keys, k)
			}
			i += 3
		case '_', ']', 'P', '^', 'X':
			// APC, OSC, DCS, PM, SOS: skip the string up to ST or BEL.
			n, complete := skipString(b[i+2:])
			if !complete && !final {
				return keys, len(b) - i
			}
			i += 2 + n
		default:
			// Alt+key: not a lone esc, and not a viewer key.
			_, size := utf8.DecodeRune(b[i+1:])
			keys = append(keys, Unknown)
			i += 1 + size
		}
	}
	return keys, 0
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
	case 'q', 0x03:
		return Quit
	case 0x11:
		return QuitApp
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
// viewer key, that key. complete is false when b ends before the final
// byte; such a sequence consumes all of b.
func decodeCSI(b []byte) (k Key, n int, ok, complete bool) {
	for n < len(b) && b[n] >= 0x20 && b[n] <= 0x3f {
		n++ // parameter and intermediate bytes
	}
	if n >= len(b) {
		return Unknown, n, false, false
	}
	if b[n] < 0x40 || b[n] > 0x7e {
		return Unknown, n + 1, false, true // malformed: drop it
	}
	params, final := string(b[:n]), b[n]
	n++
	if final == 'u' {
		k, ok = kittyKey(params)
		return k, n, ok, true
	}
	if strings.IndexFunc(params, func(r rune) bool { return (r < '0' || r > '9') && r != ';' }) >= 0 {
		return Unknown, n, false, true // private replies such as CSI ? … c
	}
	k, ok = arrowKey(final)
	return k, n, ok, true
}

// Kitty keyboard modifier bits (the reported value is 1 + bits).
const (
	kittyShift    = 1
	kittyCtrl     = 4
	kittyCapsLock = 64
	kittyNumLock  = 128
)

// kittyKey decodes a kitty keyboard protocol report "code[:alt];mods[:event]"
// (CSI … u). Release events are ignored. Shift and the lock keys are
// ignored; ctrl selects the control key (ctrl+c, ctrl+q); any other
// modifier (alt, super, …) makes the key Unknown.
func kittyKey(params string) (Key, bool) {
	if params == "" || params[0] < '0' || params[0] > '9' {
		return Unknown, false
	}
	fields := strings.Split(params, ";")
	mods := 0
	if len(fields) > 1 {
		mod := strings.Split(fields[1], ":")
		if len(mod) > 1 && mod[1] == "3" {
			return Unknown, false
		}
		if m, err := strconv.Atoi(mod[0]); err == nil && m > 0 {
			mods = (m - 1) &^ (kittyShift | kittyCapsLock | kittyNumLock)
		}
	}
	code, err := strconv.Atoi(strings.Split(fields[0], ":")[0])
	if err != nil {
		return Unknown, false
	}
	switch mods {
	case 0:
	case kittyCtrl:
		switch code {
		case 'c':
			return Quit, true
		case 'q':
			return QuitApp, true
		}
		return Unknown, true
	default:
		return Unknown, true
	}
	switch code {
	case esc:
		return Quit, true
	case 0x03, 0x11: // only via ctrl, above
		return Unknown, true
	}
	return plainKey(rune(code)), true
}

// skipString returns the length of a control string body up to and
// including its terminator (ST = ESC \, or BEL), or len(b) and
// complete=false when b ends first.
func skipString(b []byte) (n int, complete bool) {
	for j := 0; j < len(b); j++ {
		switch b[j] {
		case 0x07:
			return j + 1, true
		case esc:
			if j+1 < len(b) && b[j+1] == '\\' {
				return j + 2, true
			}
		}
	}
	return len(b), false
}
