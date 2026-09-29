package imgrender

import (
	"image"
	"math"

	"golang.org/x/image/draw"
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

// Scale returns img resized to w x h pixels with its origin at (0,0), using
// x/image/draw's ApproxBiLinear interpolator (which has fast paths for the
// common RGBA, NRGBA, YCbCr and Gray sources). img is returned unchanged when
// it already has that size and origin. A non-positive size yields an empty
// image.
func Scale(img image.Image, w, h int) image.Image {
	if w <= 0 || h <= 0 || img == nil {
		return image.NewRGBA(image.Rect(0, 0, max(w, 0), max(h, 0)))
	}
	b := img.Bounds()
	if b.Min == (image.Point{}) && b.Dx() == w && b.Dy() == h {
		return img
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// scaleRGBA is [Scale] returning an *image.RGBA, converting when Scale
// returned the source unchanged.
func scaleRGBA(img image.Image, w, h int) *image.RGBA {
	s := Scale(img, w, h)
	if rgba, ok := s.(*image.RGBA); ok {
		return rgba
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), s, s.Bounds().Min, draw.Src)
	return dst
}

// fitWithin returns w x h shrunk, aspect preserved, to at most maxW x maxH
// (never enlarged, never below 1x1).
func fitWithin(w, h, maxW, maxH int) (int, int) {
	if w <= maxW && h <= maxH {
		return w, h
	}
	s := math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	return max(1, int(math.Round(float64(w)*s))), max(1, int(math.Round(float64(h)*s)))
}
