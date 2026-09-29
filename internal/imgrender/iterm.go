package imgrender

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/iterm2"
)

// ITerm returns img scaled to pxW x pxH pixels as an iTerm2 inline image
// sequence (OSC 1337 File=…, PNG payload), also understood by WezTerm. It
// returns "" for a nil image, an empty size or an encoding failure.
func ITerm(img image.Image, pxW, pxH int) string {
	if img == nil || pxW <= 0 || pxH <= 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, sized(img, pxW, pxH)); err != nil {
		return ""
	}
	return ansi.ITerm2(iterm2.File{
		Inline:  true,
		Size:    int64(buf.Len()),
		Width:   iterm2.Pixels(pxW),
		Height:  iterm2.Pixels(pxH),
		Content: []byte(base64.StdEncoding.EncodeToString(buf.Bytes())),
	})
}
