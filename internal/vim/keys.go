package vim

import (
	"unicode"

	"github.com/mathieucroset/notty/internal/buffer"
)

// Key is one key press, adapted by the UI from its own key events.
//
// Text holds printable input (it may be several runes for IME input or a
// bracketed paste). Name identifies a special key: "esc", "enter",
// "backspace", "delete", "tab", "up", "down", "left", "right", "home", "end"
// or "space". Code is the base rune of the key, used with Ctrl (ctrl+r is
// Key{Code: 'r', Ctrl: true}).
type Key struct {
	Text             string
	Code             rune
	Ctrl, Alt, Shift bool
	Name             string
}

// Tokens are the internal spelling of keys: a single grapheme for printable
// input, or vim's angle-bracket notation ("<esc>", "<c-r>", "<s-tab>") for
// special keys. Printable input is always split into single graphemes before
// it is used as a token, so a typed "<" never collides with a special token.

// keyToken returns the token for k. Printable text is returned whole; callers
// that need single-grapheme tokens use keyTokens.
func keyToken(k Key) string {
	if k.Ctrl {
		c := k.Code
		if c == 0 && k.Text != "" {
			c = []rune(k.Text)[0]
		}
		if c == 0 {
			if k.Name == "" {
				return ""
			}
			return "<c-" + k.Name + ">"
		}
		return "<c-" + string(unicode.ToLower(c)) + ">"
	}
	if k.Alt {
		c := k.Code
		if c == 0 && k.Text != "" {
			c = []rune(k.Text)[0]
		}
		if c == 0 {
			return "<a-" + k.Name + ">"
		}
		return "<a-" + string(c) + ">"
	}
	switch k.Name {
	case "esc", "escape":
		return "<esc>"
	case "enter", "return":
		return "<cr>"
	case "backspace":
		return "<bs>"
	case "delete":
		return "<del>"
	case "tab":
		if k.Shift {
			return "<s-tab>"
		}
		return "<tab>"
	case "space":
		return "<space>"
	case "up", "down", "left", "right", "home", "end":
		if k.Shift {
			return "<s-" + k.Name + ">"
		}
		return "<" + k.Name + ">"
	}
	if k.Text == " " {
		return "<space>"
	}
	if k.Text != "" {
		return k.Text
	}
	if k.Code != 0 {
		return string(k.Code)
	}
	return ""
}

// keyTokens returns the tokens for k, splitting multi-grapheme printable
// text into one token per grapheme.
func keyTokens(k Key) []string {
	tok := keyToken(k)
	if tok == "" {
		return nil
	}
	if k.Ctrl || k.Alt || k.Name != "" || tok == "<space>" {
		return []string{tok}
	}
	gs := buffer.Graphemes(tok)
	for i, g := range gs {
		if g == " " {
			gs[i] = "<space>"
		}
	}
	return gs
}

// isPrintable reports whether k carries text to insert.
func isPrintable(k Key) bool {
	return k.Text != "" && !k.Ctrl && !k.Alt
}

// tokenChar converts a token used as a character argument (f, t, r) to the
// character it stands for, or "" if the token is not usable as one.
func tokenChar(tok string) string {
	switch tok {
	case "<space>":
		return " "
	case "<tab>":
		return "\t"
	}
	if len(tok) > 1 && tok[0] == '<' && tok[len(tok)-1] == '>' {
		return ""
	}
	return tok
}
