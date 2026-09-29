package imgrender

import (
	"bytes"
	"image"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/sixel"
)

// Sixel returns img scaled to pxW x pxH pixels as a complete Sixel DCS
// sequence. It returns "" for a nil image or an empty size.
func Sixel(img image.Image, pxW, pxH int) string {
	if img == nil || pxW <= 0 || pxH <= 0 {
		return ""
	}
	var payload bytes.Buffer
	var enc sixel.Encoder
	if err := enc.Encode(&payload, Scale(img, pxW, pxH)); err != nil {
		return ""
	}
	// P2=1 keeps unset pixels transparent instead of painting a black bar.
	return ansi.SixelGraphics(0, 1, 0, payload.Bytes())
}
