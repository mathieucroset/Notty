package imgrender

import (
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
)

func key(path string) CacheKey {
	return CacheKey{Path: path, ModTime: 1, Cols: 10, Rows: 5, Proto: ProtoKitty}
}

func ids(rs []Rendered) []uint32 {
	var out []uint32
	for _, r := range rs {
		out = append(out, r.KittyID)
	}
	return out
}

func TestCacheLRU(t *testing.T) {
	c := NewCache(3)
	for i, p := range []string{"a", "b", "c"} {
		if ev := c.Put(key(p), Rendered{KittyID: uint32(i + 1)}); len(ev) != 0 {
			t.Fatalf("put %s evicted %v", p, ids(ev))
		}
	}
	// Touch "a" so "b" becomes the least recently used.
	if r, ok := c.Get(key("a")); !ok || r.KittyID != 1 {
		t.Fatalf("get a = %v, %v", r, ok)
	}
	ev := c.Put(key("d"), Rendered{KittyID: 4})
	if !slices.Equal(ids(ev), []uint32{2}) {
		t.Fatalf("evicted %v, want [2] (b)", ids(ev))
	}
	if _, ok := c.Get(key("b")); ok {
		t.Error("b still cached")
	}
	ev = c.Put(key("e"), Rendered{KittyID: 5})
	if !slices.Equal(ids(ev), []uint32{3}) {
		t.Fatalf("evicted %v, want [3] (c)", ids(ev))
	}
	if c.Len() != 3 {
		t.Errorf("Len = %d, want 3", c.Len())
	}
}

func TestCacheKeyFields(t *testing.T) {
	c := NewCache(8)
	base := key("a")
	c.Put(base, Rendered{KittyID: 1})
	variants := []CacheKey{base, base, base, base, base}
	variants[0].ModTime = 2
	variants[1].Cols = 11
	variants[2].Rows = 6
	variants[3].Proto = ProtoHalfBlocks
	variants[4].Path = "b"
	for _, k := range variants {
		if _, ok := c.Get(k); ok {
			t.Errorf("key %+v must miss", k)
		}
	}
}

func TestCacheReplace(t *testing.T) {
	c := NewCache(2)
	c.Put(key("a"), Rendered{KittyID: 1})
	if ev := c.Put(key("a"), Rendered{KittyID: 1, Rows: []string{"x"}}); len(ev) != 0 {
		t.Errorf("same id replaced: evicted %v", ids(ev))
	}
	if ev := c.Put(key("a"), Rendered{KittyID: 7}); !slices.Equal(ids(ev), []uint32{1}) {
		t.Errorf("new id replaced: evicted %v, want [1]", ids(ev))
	}
	if c.Len() != 1 {
		t.Errorf("Len = %d", c.Len())
	}
	c.Put(key("b"), Rendered{KittyID: 2})
	got := ids(c.Clear())
	slices.Sort(got)
	if !slices.Equal(got, []uint32{2, 7}) || c.Len() != 0 {
		t.Errorf("Clear returned %v, Len %d; want [2 7], 0", got, c.Len())
	}
}

// TestCacheConcurrent hammers one cache from many goroutines; run with -race.
func TestCacheConcurrent(t *testing.T) {
	const workers, ops, capacity = 8, 500, 16
	c := NewCache(capacity)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range ops {
				k := CacheKey{Path: strconv.Itoa((w*ops + i) % 40), Cols: 1, Rows: 1, Proto: ProtoKitty}
				if i%3 == 0 {
					c.Put(k, Rendered{KittyID: uint32(i + 1)})
				} else if r, ok := c.Get(k); ok && r.KittyID == 0 {
					t.Errorf("got zero id for %v", k)
				}
				if i%97 == 0 {
					_ = c.Len()
				}
			}
		})
	}
	wg.Wait()
	if n := c.Len(); n > capacity {
		t.Errorf("Len = %d, exceeds capacity %d", n, capacity)
	}
}

func TestCacheDefaultSize(t *testing.T) {
	c := NewCache(0)
	for i := range 65 {
		ev := c.Put(CacheKey{Path: string(rune('a' + i))}, Rendered{KittyID: uint32(i + 1)})
		if i < 64 && len(ev) != 0 {
			t.Fatalf("evicted at %d", i)
		}
		if i == 64 && !slices.Equal(ids(ev), []uint32{1}) {
			t.Fatalf("65th put evicted %v, want [1]", ids(ev))
		}
	}
}

func TestDecode(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 7, 3))
	for i := range src.Pix {
		src.Pix[i] = 200
	}
	dir := t.TempDir()
	encoders := map[string]func(*os.File) error{
		"a.png": func(f *os.File) error { return png.Encode(f, src) },
		"a.jpg": func(f *os.File) error { return jpeg.Encode(f, src, nil) },
		"a.gif": func(f *os.File) error { return gif.Encode(f, src, nil) },
		"noext": func(f *os.File) error { return png.Encode(f, src) },
	}
	for name, enc := range encoders {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, name)
			f, err := os.Create(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := enc(f); err != nil {
				t.Fatal(err)
			}
			f.Close()
			img, err := Decode(p)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if b := img.Bounds(); b.Dx() != 7 || b.Dy() != 3 {
				t.Errorf("bounds %v", b)
			}
			w, h, err := Dimensions(p)
			if err != nil || w != 7 || h != 3 {
				t.Errorf("Dimensions = %d, %d, %v", w, h, err)
			}
		})
	}
}

func TestDecodeErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := Decode(filepath.Join(dir, "missing.png")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := Decode(write("text.png", "not an image")); !errors.Is(err, image.ErrFormat) {
		t.Errorf("garbage: %v, want image.ErrFormat", err)
	}
	// A truncated WebP must reach the WebP decoder (registered), not fail
	// with "unknown format".
	webp := write("x.webp", "RIFF\x20\x00\x00\x00WEBPVP8L\x10\x00\x00\x00\x2f")
	if _, err := Decode(webp); err == nil || errors.Is(err, image.ErrFormat) {
		t.Errorf("webp: %v, want a webp decoding error", err)
	}
	if _, _, err := Dimensions(webp); err == nil || errors.Is(err, image.ErrFormat) {
		t.Errorf("webp dimensions: %v", err)
	}
}
