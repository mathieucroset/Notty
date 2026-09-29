// Package imgrender turns images into terminal output: Kitty unicode
// placeholders, half-blocks, Sixel and iTerm2 sequences. It also detects which
// of those the terminal supports and caches rendered results (spec §6.3, §6.4).
//
// The package never writes to the terminal itself; callers send the returned
// strings (for example with tea.Raw) or embed them in a frame.
package imgrender

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi/kitty"
)

// Pixel budget per cell for images sent by [KittyTransmit].
const (
	kittyMaxCellW = 20
	kittyMaxCellH = 40
)

// maxPlaceholderCells is the length of the Kitty row/column diacritic table:
// placements are clamped to this many rows and columns.
const maxPlaceholderCells = 297

// KittyTransmit returns the APC sequence(s) that transmit img to a Kitty
// terminal as PNG and create a virtual placement of cols x rows cells for the
// given image id (a=T,U=1). Responses are suppressed (q=2) so nothing leaks
// into the input stream. Payloads larger than 4096 base64 bytes are split into
// chunks: every chunk but the last carries m=1, the last m=0, and no chunk is
// empty. Sources larger than cols*20 x rows*40 pixels are downscaled first
// (aspect preserved). cols and rows are clamped to 297, the size of the
// protocol's diacritic table.
//
// The image becomes visible wherever the frame contains the cells returned by
// [KittyPlaceholders] for the same id. It returns "" for a nil image, id 0
// (invalid in the protocol), an empty placement, or when encoding fails.
func KittyTransmit(img image.Image, id uint32, cols, rows int) string {
	if img == nil || id == 0 || cols <= 0 || rows <= 0 {
		return ""
	}
	cols, rows = min(cols, maxPlaceholderCells), min(rows, maxPlaceholderCells)
	// Never ship more pixels than the placement can show (20x40 px per
	// cell covers dense HiDPI cells); the terminal scales the rest.
	bounds := img.Bounds()
	w, h := fitWithin(bounds.Dx(), bounds.Dy(), cols*kittyMaxCellW, rows*kittyMaxCellH)
	var data bytes.Buffer
	if err := png.Encode(&data, Scale(img, w, h)); err != nil {
		return ""
	}
	control := "a=T,f=100,q=2,i=" + strconv.FormatUint(uint64(id), 10) +
		",U=1,c=" + strconv.Itoa(cols) + ",r=" + strconv.Itoa(rows)
	return kittyChunks(control, base64.StdEncoding.EncodeToString(data.Bytes()))
}

// kittyChunks frames a base64 payload as one APC G command, or as several of
// at most kitty.MaxChunkSize bytes each when it is longer: the first chunk
// carries control plus m=1, later ones only q=2 and m=1, the last m=0. It
// splits by length, so an exact multiple of the chunk size does not produce
// an empty trailing chunk.
func kittyChunks(control, payload string) string {
	if len(payload) <= kitty.MaxChunkSize {
		return "\x1b_G" + control + ";" + payload + "\x1b\\"
	}
	var b strings.Builder
	for i := 0; i < len(payload); i += kitty.MaxChunkSize {
		end := min(i+kitty.MaxChunkSize, len(payload))
		ctrl := "q=2"
		if i == 0 {
			ctrl = control
		}
		m := ",m=1;"
		if end == len(payload) {
			m = ",m=0;"
		}
		b.WriteString("\x1b_G" + ctrl + m + payload[i:end] + "\x1b\\")
	}
	return b.String()
}

// KittyPlaceholders returns one string per row of Kitty unicode placeholder
// cells for the virtual placement created by [KittyTransmit]. Each cell is
// U+10EEEE followed by the row and column diacritics. The image id is carried
// in a 24-bit foreground color (38;2;R;G;B) around the whole row, so these
// strings must never be restyled with a foreground color. Ids above 0xFFFFFF
// add their most significant byte as a third diacritic on every cell, as the
// Kitty protocol specifies.
//
// It returns nil for id 0 (invalid in the protocol) or an empty size. Rows
// and columns are clamped to 297, the size of the diacritic table.
func KittyPlaceholders(id uint32, cols, rows int) []string {
	if id == 0 || cols <= 0 || rows <= 0 {
		return nil
	}
	cols, rows = min(cols, maxPlaceholderCells), min(rows, maxPlaceholderCells)
	fg := "\x1b[38;2;" + strconv.Itoa(int(id>>16&0xff)) + ";" +
		strconv.Itoa(int(id>>8&0xff)) + ";" + strconv.Itoa(int(id&0xff)) + "m"
	var high rune
	if id > 0xFFFFFF {
		high = kitty.Diacritic(int(id >> 24))
	}
	out := make([]string, rows)
	var b strings.Builder
	for r := range rows {
		b.Reset()
		b.WriteString(fg)
		for c := range cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(r))
			b.WriteRune(kitty.Diacritic(c))
			if high != 0 {
				b.WriteRune(high)
			}
		}
		b.WriteString("\x1b[39m")
		out[r] = b.String()
	}
	return out
}

// KittyDelete returns the sequence that deletes the image with the given id
// together with its data (a=d,d=I) from a Kitty terminal, or "" for id 0.
func KittyDelete(id uint32) string {
	if id == 0 {
		return ""
	}
	return "\x1b_Gq=2,i=" + strconv.FormatUint(uint64(id), 10) + ",d=I,a=d\x1b\\"
}
