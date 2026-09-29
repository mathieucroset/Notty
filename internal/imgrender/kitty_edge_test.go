package imgrender

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi/kitty"
)

func TestKittyDirect(t *testing.T) {
	img := solidImage(4, 4, color.RGBA{255, 0, 0, 255})
	cmds := parseKitty(t, KittyDirect(img, 9, 3, 2))
	if len(cmds) != 1 {
		t.Fatalf("got %d commands, want 1", len(cmds))
	}
	c := cmds[0]
	want := map[string]string{"a": "T", "f": "100", "q": "2", "C": "1", "i": "9", "c": "3", "r": "2"}
	for k, v := range want {
		if c.opts[k] != v {
			t.Errorf("opt %s = %q, want %q (all: %v)", k, c.opts[k], v, c.opts)
		}
	}
	if _, ok := c.opts["U"]; ok {
		t.Errorf("direct placement must not be virtual (U=1): %v", c.opts)
	}
	assertPNG(t, c.payload, 4, 4)
}

func TestKittyDirectChunksAndDownscales(t *testing.T) {
	img := noiseImage(600, 300)
	cmds := parseKitty(t, KittyDirect(img, 5, 10, 4)) // budget 200x160 px
	if len(cmds) < 2 {
		t.Fatalf("got %d chunks, want several", len(cmds))
	}
	var all strings.Builder
	for i, c := range cmds {
		if i > 0 && len(c.opts) != 2 {
			t.Errorf("chunk %d: only q and m allowed after the first, got %v", i, c.opts)
		}
		all.WriteString(c.payload)
	}
	if cmds[0].opts["C"] != "1" || cmds[len(cmds)-1].opts["m"] != "0" {
		t.Errorf("first chunk %v, last chunk %v", cmds[0].opts, cmds[len(cmds)-1].opts)
	}
	assertPNG(t, all.String(), 200, 100)
}

func TestKittyDirectInvalid(t *testing.T) {
	img := solidImage(4, 4, color.RGBA{1, 2, 3, 255})
	for _, got := range []string{
		KittyDirect(nil, 1, 2, 2),
		KittyDirect(img, 0, 2, 2),
		KittyDirect(img, 1, 0, 2),
		KittyDirect(img, 1, 2, 0),
	} {
		if got != "" {
			t.Errorf("got %q, want empty", trunc(got))
		}
	}
}

func TestKittyIDZero(t *testing.T) {
	img := solidImage(4, 4, color.RGBA{1, 2, 3, 255})
	if got := KittyTransmit(img, 0, 2, 2); got != "" {
		t.Errorf("KittyTransmit id 0 = %q, want empty", trunc(got))
	}
	if got := KittyPlaceholders(0, 2, 2); got != nil {
		t.Errorf("KittyPlaceholders id 0 = %q, want nil", got)
	}
	if got := KittyDelete(0); got != "" {
		t.Errorf("KittyDelete id 0 = %q, want empty", got)
	}
}

// Ids at or above 2^31 must format correctly even where int is 32 bits.
func TestKittyLargeIDs(t *testing.T) {
	img := solidImage(4, 4, color.RGBA{1, 2, 3, 255})
	cmds := parseKitty(t, KittyTransmit(img, 0x80000001, 2, 2))
	if got := cmds[0].opts["i"]; got != "2147483649" {
		t.Errorf("transmit i=%q, want 2147483649", got)
	}
	if got, want := KittyDelete(0xFFFFFFFF), "\x1b_Gq=2,i=4294967295,d=I,a=d\x1b\\"; got != want {
		t.Errorf("KittyDelete = %q, want %q", got, want)
	}
	row := KittyPlaceholders(0xFFFFFFFF, 1, 1)[0]
	want := "\x1b[38;2;255;255;255m" + string([]rune{kitty.Placeholder, kitty.Diacritic(0), kitty.Diacritic(0), kitty.Diacritic(255)}) + "\x1b[39m"
	if row != want {
		t.Errorf("placeholder row = %q, want %q", row, want)
	}
}

func TestKittyClampsToDiacriticTable(t *testing.T) {
	if kitty.Diacritic(maxPlaceholderCells-1) == kitty.Diacritic(0) || kitty.Diacritic(maxPlaceholderCells) != kitty.Diacritic(0) {
		t.Fatalf("maxPlaceholderCells=%d does not match the kitty diacritic table", maxPlaceholderCells)
	}
	rows := KittyPlaceholders(1, 400, 350)
	if len(rows) != maxPlaceholderCells {
		t.Fatalf("got %d rows, want %d", len(rows), maxPlaceholderCells)
	}
	if n := strings.Count(rows[0], string(kitty.Placeholder)); n != maxPlaceholderCells {
		t.Errorf("row has %d cells, want %d", n, maxPlaceholderCells)
	}
	img := solidImage(8, 8, color.RGBA{1, 2, 3, 255})
	c := parseKitty(t, KittyTransmit(img, 1, 400, 350))[0]
	if c.opts["c"] != "297" || c.opts["r"] != "297" {
		t.Errorf("transmit c=%s r=%s, want 297", c.opts["c"], c.opts["r"])
	}
}

// kittyChunks never emits an empty final chunk, even when the payload is an
// exact multiple of the chunk size (x/ansi/kitty's EncodeGraphics would
// send a trailing "m=0" chunk with no data in that case).
func TestKittyChunksBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		wantLens []int
	}{
		{"exactly one chunk", 4096, []int{4096}},
		{"one byte over", 4100, []int{4096, 4}},
		{"exact multiple", 8192, []int{4096, 4096}},
		{"small", 8, []int{8}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := parseKitty(t, kittyChunks("a=T,q=2,i=1", strings.Repeat("A", tt.size)))
			if len(cmds) != len(tt.wantLens) {
				t.Fatalf("got %d chunks, want %d", len(cmds), len(tt.wantLens))
			}
			for i, c := range cmds {
				if len(c.payload) != tt.wantLens[i] {
					t.Errorf("chunk %d: %d bytes, want %d", i, len(c.payload), tt.wantLens[i])
				}
				m, hasM := c.opts["m"]
				switch {
				case len(cmds) == 1 && hasM:
					t.Errorf("single chunk carries m=%s", m)
				case len(cmds) > 1 && i < len(cmds)-1 && m != "1":
					t.Errorf("chunk %d: m=%q, want 1", i, m)
				case len(cmds) > 1 && i == len(cmds)-1 && m != "0":
					t.Errorf("last chunk: m=%q, want 0", m)
				}
			}
		})
	}
}
