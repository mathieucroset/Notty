package imgrender

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/sixel"
)

func TestFitCells(t *testing.T) {
	tests := []struct {
		name                   string
		imgW, imgH, maxC, maxR int
		cellW, cellH           int
		wantC, wantR           int
	}{
		{"landscape limited by rows", 800, 600, 80, 24, 8, 16, 64, 24},
		{"square upscales to fill", 100, 100, 80, 40, 10, 20, 80, 40},
		{"wide limited by cols", 1600, 200, 80, 24, 8, 16, 80, 5},
		{"tall limited by rows", 100, 1000, 80, 24, 8, 16, 5, 24},
		{"hairline keeps 1 col", 1, 1000, 80, 24, 8, 16, 1, 24},
		{"hairline keeps 1 row", 5000, 1, 80, 24, 8, 16, 80, 1},
		{"exact fit", 160, 160, 20, 10, 8, 16, 20, 10},
		{"zero image", 0, 0, 80, 24, 8, 16, 1, 1},
		{"zero max", 100, 100, 0, 0, 8, 16, 1, 1},
		{"zero cell size uses 8x16", 800, 600, 80, 24, 0, 0, 64, 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, r := FitCells(tt.imgW, tt.imgH, tt.maxC, tt.maxR, tt.cellW, tt.cellH)
			if c != tt.wantC || r != tt.wantR {
				t.Fatalf("FitCells = %dx%d, want %dx%d", c, r, tt.wantC, tt.wantR)
			}
		})
	}
}

func TestFitCellsAspect(t *testing.T) {
	for _, sz := range [][2]int{{640, 480}, {1920, 1080}, {300, 900}, {1234, 567}} {
		c, r := FitCells(sz[0], sz[1], 120, 40, 9, 18)
		if c > 120 || r > 40 || c < 1 || r < 1 {
			t.Fatalf("%v: %dx%d out of bounds", sz, c, r)
		}
		want := float64(sz[0]) / float64(sz[1])
		got := float64(c*9) / float64(r*18)
		// Rounding to whole cells: allow half a cell of error either way.
		tol := want * (0.5/float64(c) + 0.5/float64(r))
		if math.Abs(got-want) > tol {
			t.Errorf("%v: aspect %.3f, want %.3f (±%.3f) for %dx%d", sz, got, want, tol, c, r)
		}
	}
}

// img2x4 is two pixels wide and four tall:
//
//	red   green
//	blue  white
//	black gray
//	rgb(1,2,3) rgb(250,251,252)
func img2x4() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 2, 4))
	px := []color.RGBA{
		{255, 0, 0, 255}, {0, 255, 0, 255},
		{0, 0, 255, 255}, {255, 255, 255, 255},
		{0, 0, 0, 255}, {128, 128, 128, 255},
		{1, 2, 3, 255}, {250, 251, 252, 255},
	}
	for i, c := range px {
		img.SetRGBA(i%2, i/2, c)
	}
	return img
}

func TestHalfBlocksGolden(t *testing.T) {
	got := HalfBlocks(img2x4(), 2, 2)
	want := []string{
		"\x1b[38;2;255;0;0;48;2;0;0;255m▀\x1b[38;2;0;255;0;48;2;255;255;255m▀\x1b[0m",
		"\x1b[38;2;0;0;0;48;2;1;2;3m▀\x1b[38;2;128;128;128;48;2;250;251;252m▀\x1b[0m",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows: %q", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d:\n got %q\nwant %q", i, got[i], want[i])
		}
		if w := ansi.StringWidth(got[i]); w != 2 {
			t.Errorf("row %d: width %d, want 2", i, w)
		}
	}
}

func TestHalfBlocksCases(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	clear := color.RGBA{}
	tests := []struct {
		name       string
		img        image.Image
		cols, rows int
		want       []string
	}{
		{
			name: "same colors are not repeated",
			img:  solidImage(3, 2, red), cols: 3, rows: 1,
			want: []string{"\x1b[38;2;255;0;0;48;2;255;0;0m▀▀▀\x1b[0m"},
		},
		{
			name: "downscale blends pixels (bilinear)",
			img:  checker(4, 4), cols: 2, rows: 1,
			want: []string{"\x1b[38;2;127;127;127;48;2;127;127;127m▀▀\x1b[0m"},
		},
		{
			name: "transparency uses default colors",
			img:  column(red, clear, clear, red, clear, clear), cols: 1, rows: 3,
			want: []string{
				"\x1b[38;2;255;0;0;49m▀\x1b[0m",
				"\x1b[38;2;255;0;0;49m▄\x1b[0m",
				"\x1b[39;49m \x1b[0m",
			},
		},
		{"zero size", solidImage(2, 2, red), 0, 1, nil},
		{"nil image", nil, 2, 2, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HalfBlocks(tt.img, tt.cols, tt.rows)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d rows %q, want %d", len(got), got, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("row %d:\n got %q\nwant %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// checker is a black and white 1-pixel checkerboard.
func checker(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if (x+y)%2 == 0 {
				img.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}
	return img
}

// column is a 1-pixel-wide image with the given pixels top to bottom.
func column(px ...color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1, len(px)))
	for y, c := range px {
		img.SetRGBA(0, y, c)
	}
	return img
}

func TestScale(t *testing.T) {
	// Offset bounds must be handled; output always starts at (0,0).
	src := img2x4().SubImage(image.Rect(0, 1, 2, 3)) // blue white / black gray
	got := Scale(src, 4, 2)
	if b := got.Bounds(); b != image.Rect(0, 0, 4, 2) {
		t.Fatalf("bounds %v", b)
	}
	corners := map[image.Point]color.RGBA{
		{0, 0}: {0, 0, 255, 255},
		{3, 0}: {255, 255, 255, 255},
		{0, 1}: {0, 0, 0, 255},
		{3, 1}: {128, 128, 128, 255},
	}
	for p, want := range corners {
		if c := color.RGBAModel.Convert(got.At(p.X, p.Y)); c != want {
			t.Errorf("%v = %v, want %v", p, c, want)
		}
	}

	same := img2x4()
	if Scale(same, 2, 4) != image.Image(same) {
		t.Error("same size must return the source unchanged")
	}
	if b := Scale(same, 0, 3).Bounds(); !b.Empty() {
		t.Errorf("zero width gave %v", b)
	}

	// A large noisy photo shrinks to the requested size.
	big := noiseImage(1200, 800)
	if b := Scale(big, 30, 20).Bounds(); b != image.Rect(0, 0, 30, 20) {
		t.Errorf("downscale bounds %v", b)
	}
}

func TestFitWithin(t *testing.T) {
	tests := []struct{ w, h, maxW, maxH, wantW, wantH int }{
		{2000, 1000, 200, 200, 200, 100},
		{1000, 2000, 200, 200, 100, 200},
		{100, 50, 200, 200, 100, 50}, // never enlarged
		{5000, 1, 100, 100, 100, 1},  // never below 1
	}
	for _, tt := range tests {
		w, h := fitWithin(tt.w, tt.h, tt.maxW, tt.maxH)
		if w != tt.wantW || h != tt.wantH {
			t.Errorf("fitWithin(%d,%d,%d,%d) = %dx%d, want %dx%d", tt.w, tt.h, tt.maxW, tt.maxH, w, h, tt.wantW, tt.wantH)
		}
	}
}

func TestSixel(t *testing.T) {
	got := Sixel(img2x4(), 6, 12)
	if !strings.HasPrefix(got, "\x1bP0;1q") || !strings.HasSuffix(got, "\x1b\\") {
		t.Fatalf("not a sixel DCS: %q", trunc(got))
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(got, "\x1bP0;1q"), "\x1b\\")
	if !strings.HasPrefix(payload, "\"1;1;6;12") {
		t.Errorf("raster attributes missing: %q", trunc(payload))
	}
	var d sixel.Decoder
	img, err := d.Decode(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 6 || b.Dy() != 12 {
		t.Errorf("decoded %v, want 6x12", b)
	}
	if Sixel(nil, 6, 12) != "" || Sixel(img2x4(), 0, 12) != "" {
		t.Error("nil image or zero size must give empty string")
	}
}

func TestITerm(t *testing.T) {
	got := ITerm(img2x4(), 10, 20)
	const prefix = "\x1b]1337;File="
	if !strings.HasPrefix(got, prefix) || !strings.HasSuffix(got, "\x07") {
		t.Fatalf("not an iTerm2 OSC: %q", trunc(got))
	}
	args, content, ok := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(got, prefix), "\x07"), ":")
	if !ok {
		t.Fatalf("no content in %q", trunc(got))
	}
	for _, want := range []string{"inline=1", "width=10px", "height=20px"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}
	raw, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		t.Fatalf("content not base64: %v", err)
	}
	if !strings.Contains(args, "size="+strconv.Itoa(len(raw))) {
		t.Errorf("args %q missing size=%d", args, len(raw))
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("content not png: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 10 || b.Dy() != 20 {
		t.Errorf("png %v, want 10x20", b)
	}
	if ITerm(nil, 1, 1) != "" {
		t.Error("nil image must give empty string")
	}
}

func TestWrapTmux(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"single sequence", "\x1b_Ga=d\x1b\\", "\x1bPtmux;\x1b\x1b_Ga=d\x1b\x1b\\\x1b\\"},
		{"each ST sequence wrapped separately", "\x1b_Gm=1;AA\x1b\\\x1b_Gm=0;BB\x1b\\",
			"\x1bPtmux;\x1b\x1b_Gm=1;AA\x1b\x1b\\\x1b\\" + "\x1bPtmux;\x1b\x1b_Gm=0;BB\x1b\x1b\\\x1b\\"},
		{"BEL terminated", "\x1b]1337;File=:AA\x07", "\x1bPtmux;\x1b\x1b]1337;File=:AA\x07\x1b\\"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WrapTmux(tt.in); got != tt.want {
				t.Errorf("got %q\nwant %q", got, tt.want)
			}
		})
	}
}
