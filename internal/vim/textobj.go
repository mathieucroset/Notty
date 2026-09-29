package vim

import "github.com/mathieucroset/notty/internal/buffer"

func isTextObject(name string) bool { return false }

func (m *Machine) textObject(b *buffer.Buffer, c cmd, from buffer.Pos) (buffer.Range, bool, bool) {
	return buffer.Range{}, false, false
}
