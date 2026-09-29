package imgrender

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"
)

// kittyCmd is one parsed APC G sequence.
type kittyCmd struct {
	opts    map[string]string
	order   []string
	payload string
}

// parseKitty splits s into its APC G sequences. It fails the test when s
// contains anything other than well-formed kitty graphics commands.
func parseKitty(t *testing.T, s string) []kittyCmd {
	t.Helper()
	var cmds []kittyCmd
	for s != "" {
		if !strings.HasPrefix(s, "\x1b_G") {
			t.Fatalf("expected APC G at %q", trunc(s))
		}
		end := strings.Index(s, "\x1b\\")
		if end < 0 {
			t.Fatalf("unterminated APC in %q", trunc(s))
		}
		body := s[len("\x1b_G"):end]
		s = s[end+2:]
		ctrl, payload, _ := strings.Cut(body, ";")
		c := kittyCmd{opts: map[string]string{}, payload: payload}
		for kv := range strings.SplitSeq(ctrl, ",") {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				t.Fatalf("malformed key %q", kv)
			}
			c.opts[k] = v
			c.order = append(c.order, k)
		}
		cmds = append(cmds, c)
	}
	return cmds
}

func trunc(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

func solidImage(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

func noiseImage(w, h int) *image.RGBA {
	r := rand.New(rand.NewPCG(1, 2))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = byte(r.IntN(256))
	}
	return img
}

func TestKittyPlaceholdersGolden(t *testing.T) {
	tests := []struct {
		name       string
		id         uint32
		cols, rows int
		want       []string
	}{
		{
			name: "2x2 id 42",
			id:   42, cols: 2, rows: 2,
			want: []string{
				"\x1b[38;2;0;0;42m\U0010EEEE\u0305\u0305\U0010EEEE\u0305\u030D\x1b[39m",
				"\x1b[38;2;0;0;42m\U0010EEEE\u030D\u0305\U0010EEEE\u030D\u030D\x1b[39m",
			},
		},
		{
			name: "1x1 id spans three bytes",
			id:   0x123456, cols: 1, rows: 1,
			want: []string{
				"\x1b[38;2;18;52;86m\U0010EEEE\u0305\u0305\x1b[39m",
			},
		},
		{
			name: "id above 24 bits adds third diacritic",
			id:   0x02000001, cols: 1, rows: 1,
			want: []string{
				"\x1b[38;2;0;0;1m\U0010EEEE\u0305\u0305\u030E\x1b[39m",
			},
		},
		{
			name: "zero size",
			id:   1, cols: 0, rows: 3,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KittyPlaceholders(tt.id, tt.cols, tt.rows)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d rows, want %d: %q", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("row %d:\n got %q\nwant %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestKittyDelete(t *testing.T) {
	tests := []struct {
		id   uint32
		want string
	}{
		{7, "\x1b_Gq=2,i=7,d=I,a=d\x1b\\"},
		{0xFFFFFF, "\x1b_Gq=2,i=16777215,d=I,a=d\x1b\\"},
	}
	for _, tt := range tests {
		if got := KittyDelete(tt.id); got != tt.want {
			t.Errorf("KittyDelete(%d) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestKittyTransmitSingleChunk(t *testing.T) {
	img := solidImage(4, 4, color.RGBA{255, 0, 0, 255})
	cmds := parseKitty(t, KittyTransmit(img, 9, 3, 2))
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	c := cmds[0]
	want := map[string]string{"f": "100", "q": "2", "i": "9", "U": "1", "c": "3", "r": "2", "a": "T"}
	for k, v := range want {
		if c.opts[k] != v {
			t.Errorf("opt %s = %q, want %q (all: %v)", k, c.opts[k], v, c.opts)
		}
	}
	if _, ok := c.opts["m"]; ok {
		t.Errorf("single chunk must not carry m=: %v", c.opts)
	}
	assertPNG(t, c.payload, 4, 4)
}

func TestKittyTransmitChunking(t *testing.T) {
	img := noiseImage(120, 80) // ~38 KB of incompressible PNG
	cmds := parseKitty(t, KittyTransmit(img, 3, 10, 5))
	if len(cmds) < 3 {
		t.Fatalf("got %d chunks, want >= 3", len(cmds))
	}
	var all strings.Builder
	for i, c := range cmds {
		first, last := i == 0, i == len(cmds)-1
		wantM := "1"
		if last {
			wantM = "0"
		}
		if c.opts["m"] != wantM {
			t.Errorf("chunk %d: m=%q, want %q", i, c.opts["m"], wantM)
		}
		if c.opts["q"] != "2" {
			t.Errorf("chunk %d: q=%q, want 2", i, c.opts["q"])
		}
		if first {
			for _, k := range []string{"a", "f", "i", "U", "c", "r"} {
				if _, ok := c.opts[k]; !ok {
					t.Errorf("first chunk missing %s: %v", k, c.opts)
				}
			}
		} else if len(c.opts) != 2 {
			t.Errorf("chunk %d: only q and m allowed after the first, got %v", i, c.opts)
		}
		if len(c.payload) > 4096 {
			t.Errorf("chunk %d: payload %d bytes > 4096", i, len(c.payload))
		}
		if !last && len(c.payload)%4 != 0 {
			t.Errorf("chunk %d: payload %d bytes not a multiple of 4", i, len(c.payload))
		}
		all.WriteString(c.payload)
	}
	assertPNG(t, all.String(), 120, 80)
}

func TestKittyTransmitDownscales(t *testing.T) {
	tests := []struct {
		name       string
		w, h       int
		cols, rows int
		wantW      int
		wantH      int
	}{
		{"wide photo limited by width", 2000, 1000, 10, 5, 200, 100},
		{"tall photo limited by height", 1000, 4000, 10, 5, 50, 200},
		{"small image sent as is", 30, 20, 10, 5, 30, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img := solidImage(tt.w, tt.h, color.RGBA{0, 128, 255, 255})
			var payload strings.Builder
			for _, c := range parseKitty(t, KittyTransmit(img, 5, tt.cols, tt.rows)) {
				payload.WriteString(c.payload)
			}
			assertPNG(t, payload.String(), tt.wantW, tt.wantH)
		})
	}
}

func TestKittyTransmitNil(t *testing.T) {
	if got := KittyTransmit(nil, 1, 1, 1); got != "" {
		t.Errorf("nil image: got %q, want empty", got)
	}
}

func assertPNG(t *testing.T, b64 string, w, h int) {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("payload not base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("payload not PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Errorf("png is %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
}
