package imgrender

import (
	"image"
	"image/color"
	"math"
)

// FitCells returns the largest cell size (cols x rows) that shows an imgW x
// imgH pixel image with its aspect ratio preserved, given cells of cellW x
// cellH pixels (8x16 when unknown), without exceeding maxCols x maxRows. It
// upscales small images; pass a smaller max to avoid that. The result is at
// least 1x1.
func FitCells(imgW, imgH, maxCols, maxRows, cellW, cellH int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 1, 1
	}
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 8, 16
	}
	// Image size measured in cells.
	w := float64(imgW) / float64(cellW)
	h := float64(imgH) / float64(cellH)
	scale := math.Min(float64(maxCols)/w, float64(maxRows)/h)
	cols = clamp(int(math.Round(w*scale)), 1, maxCols)
	rows = clamp(int(math.Round(h*scale)), 1, maxRows)
	return cols, rows
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// maxSamples bounds how many source pixels per axis are averaged into one
// destination pixel, so shrinking a huge photo stays cheap.
const maxSamples = 8

// scaleImage resizes img to w x h with a box filter: each destination pixel
// averages the source pixels under it (sampling at most maxSamples per axis),
// which degrades to nearest neighbour when enlarging. Averaging happens on
// premultiplied values, so transparent pixels do not darken edges. The result
// always has its origin at (0,0).
func scaleImage(img image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return dst
	}
	for y := range h {
		sy0 := y * sh / h
		sy1 := max(sy0+1, (y+1)*sh/h)
		stepY := max(1, (sy1-sy0)/maxSamples)
		for x := range w {
			sx0 := x * sw / w
			sx1 := max(sx0+1, (x+1)*sw/w)
			stepX := max(1, (sx1-sx0)/maxSamples)
			var r, g, bl, a, n uint64
			for sy := sy0; sy < sy1; sy += stepY {
				for sx := sx0; sx < sx1; sx += stepX {
					cr, cg, cb, ca := img.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r += uint64(cr >> 8)
					g += uint64(cg >> 8)
					bl += uint64(cb >> 8)
					a += uint64(ca >> 8)
					n++
				}
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8((r + n/2) / n),
				G: uint8((g + n/2) / n),
				B: uint8((bl + n/2) / n),
				A: uint8((a + n/2) / n),
			})
		}
	}
	return dst
}

// sized returns img unchanged when it already is w x h with origin (0,0),
// otherwise a scaled copy.
func sized(img image.Image, w, h int) image.Image {
	if b := img.Bounds(); b.Min == (image.Point{}) && b.Dx() == w && b.Dy() == h {
		return img
	}
	return scaleImage(img, w, h)
}
