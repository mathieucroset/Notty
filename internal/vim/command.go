package vim

import (
	"strconv"
	"strings"

	"github.com/mathieucroset/notty/internal/buffer"
)

// runEx executes a ":" command line (without the colon).
func (m *Machine) runEx(b *buffer.Buffer, line string) {
	s := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), ":"))
	if s == "" {
		return
	}
	name, arg, _ := strings.Cut(s, " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "w", "write", "w!":
		m.eff.Save = true
	case "q", "quit", "q!", "quit!", "qa", "qa!", "qall":
		m.eff.Quit = true
	case "wq", "wq!", "x", "x!", "xit", "exit", "wqa", "xa":
		m.eff.Save, m.eff.Quit = true, true
	case "e", "edit", "e!":
		if arg == "" {
			m.eff.Message = "E32: No file name"
			return
		}
		m.eff.OpenNote, m.eff.NoteArg = true, arg
	case "img", "image":
		if arg == "" {
			m.eff.Message = "E471: Argument required"
			return
		}
		m.eff.ImgArg = arg
	case "h", "help":
		m.eff.Help = true
	case "noh", "nohlsearch":
	default:
		if n, err := strconv.Atoi(s); err == nil {
			l := max(0, min(n-1, b.LineCount()-1))
			b.SetCursor(pos(l, firstNonBlank(b, l)))
			return
		}
		if s == "$" {
			l := b.LineCount() - 1
			b.SetCursor(pos(l, firstNonBlank(b, l)))
			return
		}
		m.eff.Message = "Not an editor command: " + s
	}
}
