package preview

import (
	"fmt"
	"testing"

	"github.com/mathieucroset/notty/internal/imgrender"
)

// TestImageCells checks image sizing (spec §6.4) in an 88×38 pane: at most
// 88 columns and 22 rows (60%), aspect ratio preserved.
func TestImageCells(t *testing.T) {
	const paneCols, paneRows = 88, 38
	tests := []struct {
		proto      imgrender.Protocol
		w, h       int
		cw, ch     int
		cols, rows int
	}{
		// Half-blocks: a small image grows to its sample resolution
		// (64 columns, 16 rows), never capped at its physical size.
		{imgrender.ProtoHalfBlocks, 64, 32, 8, 16, 64, 16},
		{imgrender.ProtoHalfBlocks, 64, 32, 16, 32, 64, 16},
		// Larger images are capped by 60% of the pane height.
		{imgrender.ProtoHalfBlocks, 800, 600, 8, 16, 59, 22},
		{imgrender.ProtoHalfBlocks, 800, 600, 16, 32, 59, 22},
		{imgrender.ProtoHalfBlocks, 3000, 2000, 8, 16, 66, 22},
		{imgrender.ProtoHalfBlocks, 3000, 2000, 16, 32, 66, 22},
		// Kitty: a small image is upscaled to a quarter of the pane width
		// (22 columns), which it reaches before 8 rows.
		{imgrender.ProtoKitty, 64, 32, 8, 16, 22, 6},
		{imgrender.ProtoKitty, 64, 32, 16, 32, 22, 6},
		// Kitty keeps a mid-size image at its physical size when it fits.
		{imgrender.ProtoKitty, 800, 600, 8, 16, 59, 22},
		{imgrender.ProtoKitty, 800, 600, 16, 32, 50, 19},
		{imgrender.ProtoKitty, 3000, 2000, 8, 16, 66, 22},
		{imgrender.ProtoKitty, 3000, 2000, 16, 32, 66, 22},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%v %dx%d at %dx%d", tt.proto, tt.w, tt.h, tt.cw, tt.ch)
		t.Run(name, func(t *testing.T) {
			cols, rows := imageCells(tt.proto, tt.w, tt.h, paneCols, paneRows, tt.cw, tt.ch)
			if cols != tt.cols || rows != tt.rows {
				t.Errorf("got %dx%d cells, want %dx%d", cols, rows, tt.cols, tt.rows)
			}
			if cols > paneCols || rows > paneRows*60/100 {
				t.Errorf("%dx%d exceeds the pane caps", cols, rows)
			}
		})
	}
}

func TestKittyTallSmallImageStopsAtEightRows(t *testing.T) {
	// 16x64 px at 8x16 is 2x4 cells: it reaches 8 rows (4 columns) long
	// before a quarter of the pane width.
	cols, rows := imageCells(imgrender.ProtoKitty, 16, 64, 88, 38, 8, 16)
	if cols != 4 || rows != 8 {
		t.Errorf("got %dx%d, want 4x8", cols, rows)
	}
}
