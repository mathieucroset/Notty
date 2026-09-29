package vim

import (
	"strconv"
	"strings"
)

// cmd is one parsed normal- or visual-mode command:
//
//	["x] [count1] name [arg]
//	["x] [count1] op [count2] (motion [arg] | textobject | op)
type cmd struct {
	reg            string // "+" for the clipboard register, "" for unnamed
	count1, count2 int    // 0 means not given
	op             string // operator ("d", "c", "y", ">", "<") or ""
	name           string // motion, text object ("iw"), action, or op when doubled
	arg            string // character argument (f, t, r) or <c-w> target
}

// count returns the effective count: count1 * count2, each defaulting to 1.
func (c cmd) count() int {
	n := max(1, c.count1)
	if c.count2 > 0 {
		n *= c.count2
	}
	return n
}

func (c cmd) hasCount() bool { return c.count1 > 0 || c.count2 > 0 }

// String renders the command back into keys (used for Pending and tests).
func (c cmd) String() string {
	var sb strings.Builder
	if c.reg != "" {
		sb.WriteString(`"` + c.reg)
	}
	if c.count1 > 0 {
		sb.WriteString(strconv.Itoa(c.count1))
	}
	sb.WriteString(c.op)
	if c.count2 > 0 {
		sb.WriteString(strconv.Itoa(c.count2))
	}
	sb.WriteString(c.name)
	sb.WriteString(c.arg)
	return sb.String()
}

type parseStatus int

const (
	parseMore parseStatus = iota // a valid prefix; wait for more keys
	parseBad                     // not a command; discard the keys
	parseOK                      // a complete command
)

// maxCount caps counts so arithmetic on them cannot overflow.
const maxCount = 99999

func isDigitTok(t string) bool { return len(t) == 1 && t[0] >= '0' && t[0] <= '9' }

// parseCount reads a count starting at toks[i]. A leading "0" is not a count
// (it is the 0 motion).
func parseCount(toks []string, i int) (n, next int) {
	for i < len(toks) && isDigitTok(toks[i]) && (n > 0 || toks[i] != "0") {
		n = min(maxCount, n*10+int(toks[i][0]-'0'))
		i++
	}
	return n, i
}

// singleMotions are motions of one key without an argument.
var singleMotions = map[string]bool{
	"h": true, "j": true, "k": true, "l": true, "0": true, "^": true, "$": true,
	"w": true, "b": true, "e": true, "W": true, "B": true, "E": true,
	"{": true, "}": true, "%": true, "G": true, ";": true, ",": true,
	"n": true, "N": true,
	"+": true, "-": true, "_": true,
	"<left>": true, "<right>": true, "<up>": true, "<down>": true,
	"<home>": true, "<end>": true, "<bs>": true, "<cr>": true,
}

// gMotions are the motions spelled "g" + key.
var gMotions = map[string]bool{"g": true, "j": true, "k": true, "e": true, "E": true, "_": true}

// charMotions take a character argument.
var charMotions = map[string]bool{"f": true, "F": true, "t": true, "T": true}

func isMotion(name string) bool {
	return singleMotions[name] || charMotions[name] ||
		(len(name) == 2 && name[0] == 'g' && gMotions[name[1:]])
}

// parseMotion parses a motion starting at toks[i].
func parseMotion(toks []string, i int) (name, arg string, st parseStatus) {
	t := toks[i]
	switch {
	case singleMotions[t]:
		return t, "", parseOK
	case charMotions[t]:
		if i+1 >= len(toks) {
			return "", "", parseMore
		}
		c := tokenChar(toks[i+1])
		if c == "" {
			return "", "", parseBad
		}
		return t, c, parseOK
	case t == "g":
		if i+1 >= len(toks) {
			return "", "", parseMore
		}
		if gMotions[toks[i+1]] {
			return "g" + toks[i+1], "", parseOK
		}
	}
	return "", "", parseBad
}

// normalActions are the non-motion normal-mode commands of one key.
var normalActions = map[string]bool{
	"i": true, "a": true, "I": true, "A": true, "o": true, "O": true,
	"x": true, "X": true, "<del>": true, "s": true, "S": true, "J": true,
	"p": true, "P": true, "D": true, "C": true, "Y": true, "~": true,
	"u": true, "<c-r>": true, "v": true, "V": true, ".": true,
	"/": true, "?": true, ":": true, "<tab>": true,
}

// argActions take one more key as argument.
var argActions = map[string]bool{"r": true, "<c-w>": true}

// operators take a motion or text object; doubled they act on lines.
var operators = map[string]bool{"d": true, "c": true, "y": true, ">": true, "<": true}

// parse parses toks as one command. visual selects the visual-mode grammar,
// where operators apply to the selection and take no motion.
func parse(toks []string, visual bool) (cmd, parseStatus) {
	var c cmd
	i := 0
	if toks[0] == `"` {
		if len(toks) < 2 {
			return c, parseMore
		}
		switch toks[1] {
		case "+", "*":
			c.reg = "+"
		case `"`:
		default:
			return c, parseBad
		}
		i = 2
	}
	c.count1, i = parseCount(toks, i)
	if i >= len(toks) {
		return c, parseMore
	}
	t := toks[i]
	if !visual && operators[t] {
		return parseOperator(toks, i, c)
	}
	if visual && (t == "i" || t == "a") {
		return parseTextObject(toks, i, c)
	}
	if visual && visualActions[t] {
		c.name = t
		return c, parseOK
	}
	if argActions[t] {
		if i+1 >= len(toks) {
			return c, parseMore
		}
		c.name, c.arg = t, toks[i+1]
		return c, parseOK
	}
	if !visual && normalActions[t] {
		c.name = t
		return c, parseOK
	}
	name, arg, st := parseMotion(toks, i)
	if st != parseOK {
		return c, st
	}
	c.name, c.arg = name, arg
	return c, parseOK
}

// Filled in by later features.
var visualActions = map[string]bool{}

// parseOperator parses op [count2] (op | motion | textobject) at toks[i].
func parseOperator(toks []string, i int, c cmd) (cmd, parseStatus) {
	c.op = toks[i]
	c.count2, i = parseCount(toks, i+1)
	if i >= len(toks) {
		return c, parseMore
	}
	switch toks[i] {
	case c.op:
		c.name = c.op
		return c, parseOK
	case "i", "a":
		return parseTextObject(toks, i, c)
	}
	name, arg, st := parseMotion(toks, i)
	c.name, c.arg = name, arg
	return c, st
}
