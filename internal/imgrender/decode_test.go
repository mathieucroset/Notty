package imgrender

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// pngHeader returns a PNG consisting of the signature and an IHDR chunk
// claiming w x h pixels (8-bit RGBA), with a valid CRC but no image data.
// DecodeConfig accepts it; a full decode would try to allocate w*h*4 bytes.
func pngHeader(w, h uint32) []byte {
	var ihdr bytes.Buffer
	ihdr.WriteString("IHDR")
	binary.Write(&ihdr, binary.BigEndian, w) //nolint:errcheck
	binary.Write(&ihdr, binary.BigEndian, h) //nolint:errcheck
	ihdr.Write([]byte{8, 6, 0, 0, 0})        // depth, RGBA, deflate, filter, no interlace

	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&b, binary.BigEndian, uint32(ihdr.Len()-4))             //nolint:errcheck
	b.Write(ihdr.Bytes())                                                //nolint:errcheck
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes())) //nolint:errcheck
	return b.Bytes()
}

func TestDecodeRejectsHugeImages(t *testing.T) {
	tests := []struct {
		name string
		w, h uint32
		huge bool
	}{
		{"both sides huge", 100000, 100000, true},
		{"too many pixels", 8000, 8000, true},   // 64e6 > 50e6
		{"one side too long", 16385, 10, true},  // side > 16384
		{"tall side too long", 10, 20000, true}, //
		{"at the limits", 16384, 3000, false},   // 49.2e6 pixels, sides ok
		{"small", 10, 10, false},
	}
	dir := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(dir, tt.name+".png")
			if err := os.WriteFile(p, pngHeader(tt.w, tt.h), 0o600); err != nil {
				t.Fatal(err)
			}
			w, h, err := Dimensions(p)
			if got := errors.Is(err, ErrImageTooLarge); got != tt.huge {
				t.Fatalf("Dimensions err = %v, want ErrImageTooLarge=%v", err, tt.huge)
			}
			if w != int(tt.w) || h != int(tt.h) {
				t.Errorf("Dimensions = %dx%d, want %dx%d even when too large", w, h, tt.w, tt.h)
			}
			_, err = Decode(p)
			if tt.huge {
				if !errors.Is(err, ErrImageTooLarge) {
					t.Fatalf("Decode err = %v, want ErrImageTooLarge", err)
				}
			} else if err == nil || errors.Is(err, ErrImageTooLarge) {
				// Allowed sizes get past the check and then fail on the
				// missing pixel data.
				t.Fatalf("Decode err = %v, want a data error", err)
			}
		})
	}
}
