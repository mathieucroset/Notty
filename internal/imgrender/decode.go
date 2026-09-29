package imgrender

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"io"
	"os"

	// Register the formats Notty accepts (spec §6.2).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// Limits checked before any pixel data is decoded, so a small file claiming
// enormous dimensions (a decompression bomb) cannot exhaust memory.
const (
	MaxImagePixels = 50_000_000
	MaxImageSide   = 16384
)

// ErrImageTooLarge is returned (wrapped) by [Decode] and [Dimensions] for
// images over [MaxImagePixels] pixels or with a side over [MaxImageSide].
var ErrImageTooLarge = errors.New("image too large")

// Decode reads a PNG, JPEG, GIF (first frame) or WebP image from path. The
// format is sniffed from the content, not the extension. The header is read
// first and oversized images are rejected with [ErrImageTooLarge].
func Decode(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	if _, _, err := checkedConfig(f, path); err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("decode image %s: %w", path, err)
	}
	img, _, err := image.Decode(bufio.NewReader(f))
	if err != nil {
		return nil, fmt.Errorf("decode image %s: %w", path, err)
	}
	return img, nil
}

// Dimensions returns the pixel size of the image at path without decoding
// the pixel data. For an oversized image it returns the size together with
// an error wrapping [ErrImageTooLarge].
func Dimensions(path string) (w, h int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("image dimensions: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	w, h, err = checkedConfig(f, path)
	if err != nil {
		return w, h, fmt.Errorf("image dimensions: %w", err)
	}
	return w, h, nil
}

func checkedConfig(r io.Reader, path string) (w, h int, err error) {
	cfg, _, err := image.DecodeConfig(bufio.NewReader(r))
	if err != nil {
		return 0, 0, fmt.Errorf("%s: %w", path, err)
	}
	w, h = cfg.Width, cfg.Height
	if w > MaxImageSide || h > MaxImageSide || int64(w)*int64(h) > MaxImagePixels {
		return w, h, fmt.Errorf("%s is %dx%d: %w", path, w, h, ErrImageTooLarge)
	}
	return w, h, nil
}
