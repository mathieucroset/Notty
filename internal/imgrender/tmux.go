package imgrender

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// WrapTmux wraps seq in tmux passthrough (DCS tmux; … ST with every ESC
// doubled) so it reaches the outer terminal. Each ST-terminated sequence in
// seq (for example every chunk of a Kitty transmission) is wrapped on its
// own, keeping each passthrough small. Requires tmux allow-passthrough, see
// [Caps.TmuxPassthrough].
func WrapTmux(seq string) string {
	var b strings.Builder
	for seq != "" {
		i := strings.Index(seq, "\x1b\\")
		if i < 0 {
			b.WriteString(ansi.TmuxPassthrough(seq))
			break
		}
		b.WriteString(ansi.TmuxPassthrough(seq[:i+2]))
		seq = seq[i+2:]
	}
	return b.String()
}
