package imgrender

import (
	"image"
	"image/color"
	"strconv"
	"strings"
)

// HalfBlocks renders img as rows strings of cols cells each, pure text that
// any truecolor terminal shows. The image is scaled to cols x rows*2 pixels;
// each cell is '▀' with the top pixel as 24-bit foreground and the bottom
// pixel as background. Mostly transparent pixels (alpha < 50%) use the
// terminal's default colors ('▄' when only the top is transparent, a space
// when both are). An SGR is emitted only when the colors change, and every
// row ends with a full reset.
func HalfBlocks(img image.Image, cols, rows int) []string {
	if img == nil || cols <= 0 || rows <= 0 {
		return nil
	}
	px := scaleRGBA(img, cols, rows*2)
	out := make([]string, rows)
	var b strings.Builder
	for r := range rows {
		b.Reset()
		prev := ""
		for c := range cols {
			top, topOK := opaque(px.RGBAAt(c, 2*r))
			bot, botOK := opaque(px.RGBAAt(c, 2*r+1))
			var glyph, fg, bg string
			switch {
			case topOK:
				glyph, fg = "▀", fgSGR(top)
				bg = "49"
				if botOK {
					bg = bgSGR(bot)
				}
			case botOK:
				glyph, fg, bg = "▄", fgSGR(bot), "49"
			default:
				glyph, fg, bg = " ", "39", "49"
			}
			if sgr := fg + ";" + bg; sgr != prev {
				b.WriteString("\x1b[" + sgr + "m")
				prev = sgr
			}
			b.WriteString(glyph)
		}
		b.WriteString("\x1b[0m")
		out[r] = b.String()
	}
	return out
}

// opaque converts a premultiplied pixel to straight RGB and reports whether
// it is at least half opaque.
func opaque(c color.RGBA) (color.RGBA, bool) {
	if c.A < 0x80 {
		return c, false
	}
	if c.A == 0xff {
		return c, true
	}
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return color.RGBA{n.R, n.G, n.B, 0xff}, true
}

func fgSGR(c color.RGBA) string { return "38;2;" + rgb(c) }
func bgSGR(c color.RGBA) string { return "48;2;" + rgb(c) }

func rgb(c color.RGBA) string {
	return strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))
}
