package imgrender

import (
	"bufio"
	"fmt"
	"image"
	"os"

	// Register the formats Notty accepts (spec §6.2).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// Decode reads a PNG, JPEG, GIF (first frame) or WebP image from path. The
// format is sniffed from the content, not the extension.
func Decode(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	img, _, err := image.Decode(bufio.NewReader(f))
	if err != nil {
		return nil, fmt.Errorf("decode image %s: %w", path, err)
	}
	return img, nil
}

// Dimensions returns the pixel size of the image at path without decoding
// the pixel data.
func Dimensions(path string) (w, h int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("image dimensions: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	cfg, _, err := image.DecodeConfig(bufio.NewReader(f))
	if err != nil {
		return 0, 0, fmt.Errorf("image dimensions %s: %w", path, err)
	}
	return cfg.Width, cfg.Height, nil
}
