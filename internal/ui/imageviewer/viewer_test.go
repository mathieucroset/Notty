package imageviewer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/imgrender"
)

const (
	altEnter   = "\x1b[?1049h"
	altLeave   = "\x1b[?1049l"
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
)

// writePNG writes a w x h PNG with a red/blue gradient and returns its path.
func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / w), B: uint8(y * 255 / h), A: 255}) //nolint:gosec // test data
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeHugePNG writes a PNG whose header claims w x h pixels (with a valid
// IHDR CRC) so Decode rejects it before reading pixel data.
func writeHugePNG(t *testing.T, dir string, w, h uint32) string {
	t.Helper()
	path := writePNG(t, dir, "huge.png", 1, 1)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 8-byte signature, 4-byte length, "IHDR", then width and height.
	binary.BigEndian.PutUint32(data[16:], w)
	binary.BigEndian.PutUint32(data[20:], h)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestViewer(paths []string, proto imgrender.Protocol, in io.Reader) (*Viewer, *bytes.Buffer) {
	v := New(paths, 0, imgrender.Caps{Viewer: proto})
	v.Size = func() (int, int, error) { return 80, 24, nil }
	v.Open = func(string) error { return nil }
	var out bytes.Buffer
	v.SetStdin(in)
	v.SetStdout(&out)
	v.SetStderr(io.Discard)
	return v, &out
}

func runViewer(t *testing.T, v *Viewer) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- v.Run() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}

// assertOrder fails unless every needle occurs in s, each after the previous.
func assertOrder(t *testing.T, s string, needles ...string) {
	t.Helper()
	pos := 0
	for _, n := range needles {
		i := strings.Index(s[pos:], n)
		if i < 0 {
			t.Fatalf("missing %q after byte %d in output %q", n, pos, abbreviate(s))
		}
		pos += i + len(n)
	}
}

func abbreviate(s string) string {
	if len(s) > 2000 {
		return s[:1000] + " … " + s[len(s)-1000:]
	}
	return s
}

func TestRunHalfBlocksNavigates(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 40, 20)
	b := writePNG(t, dir, "b.png", 30, 30)
	v, out := newTestViewer([]string{a, b}, imgrender.ProtoHalfBlocks, strings.NewReader("nq"))
	runViewer(t, v)
	s := out.String()
	assertOrder(t, s, altEnter, hideCursor, "▀", "a.png  40x20  1/2", "n next · p prev · o open · q close",
		"▀", "b.png  30x30  2/2", showCursor, altLeave)
	// 40x20 px at 8x16 cells fills 80x20 cells of the 80x23 area: centered
	// vertically, it starts on row 2.
	if !strings.Contains(s, "\x1b[2;1H") {
		t.Errorf("image not positioned at row 2 col 1: %q", abbreviate(s))
	}
	if strings.Contains(s, "\x1b_G") {
		t.Error("half-block viewer sent kitty graphics")
	}
}

func TestRunWrapsAround(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 10, 10)
	b := writePNG(t, dir, "b.png", 10, 10)
	v, out := newTestViewer([]string{a, b}, imgrender.ProtoHalfBlocks, strings.NewReader("p\x1b[Cq"))
	runViewer(t, v)
	assertOrder(t, out.String(), "1/2", "2/2", "1/2", altLeave)
}

func TestRunKitty(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 40, 20)
	b := writePNG(t, dir, "b.png", 20, 40)
	v, out := newTestViewer([]string{a, b}, imgrender.ProtoKitty, strings.NewReader("nq"))
	runViewer(t, v)
	s := out.String()
	del := imgrender.KittyDelete(kittyID)
	assertOrder(t, s, altEnter, "\x1b_Ga=T,f=100,q=2,C=1,i=", "1/2", del, "\x1b_Ga=T", "2/2", del, altLeave)
	if strings.Contains(s, "U=1") {
		t.Error("viewer used a virtual placement instead of a direct one")
	}
	if strings.Contains(s, "▀") {
		t.Error("kitty viewer drew half-blocks")
	}
}

func TestRunKittyClampsTo297Cells(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 40, 20)
	for _, tmux := range []bool{false, true} {
		v, out := newTestViewer([]string{a}, imgrender.ProtoKitty, strings.NewReader("q"))
		v.Caps.TmuxPassthrough = tmux
		v.Size = func() (int, int, error) { return 1000, 400, nil }
		runViewer(t, v)
		s := out.String()
		// 297 columns keep the 4:1 cell aspect: 74 rows, centered.
		if !strings.Contains(s, "c=297,r=74") {
			t.Errorf("tmux=%v: placement not clamped to 297x74 cells: %q", tmux, abbreviate(s))
		}
		if !strings.Contains(s, cup((399-74)/2+1, (1000-297)/2+1)) {
			t.Errorf("tmux=%v: clamped image not centered", tmux)
		}
	}
}

func TestRunKittyTmuxUsesPlaceholders(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 40, 20)
	v, out := newTestViewer([]string{a}, imgrender.ProtoKitty, strings.NewReader("q"))
	v.Caps.TmuxPassthrough = true
	runViewer(t, v)
	s := out.String()
	assertOrder(t, s, "\x1bPtmux;\x1b\x1b_G", "\U0010EEEE", imgrender.WrapTmux(imgrender.KittyDelete(kittyID)), altLeave)
}

func TestRunSixelAndITerm(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 40, 20)
	for _, tt := range []struct {
		proto imgrender.Protocol
		tmux  bool
		want  string
	}{
		{imgrender.ProtoSixel, false, "\x1b[2;1H\x1bP"},
		{imgrender.ProtoITerm, false, "\x1b[2;1H\x1b]1337;File="},
		{imgrender.ProtoSixel, true, "\x1b[2;1H\x1bPtmux;\x1b\x1bP"},
		{imgrender.ProtoITerm, true, "\x1b[2;1H\x1bPtmux;\x1b\x1b]1337;File="},
	} {
		t.Run(tt.proto.String(), func(t *testing.T) {
			v, out := newTestViewer([]string{a}, tt.proto, strings.NewReader("q"))
			v.Caps.TmuxPassthrough = tt.tmux
			// Tiny cells keep the encoded image small (same layout as 8x16).
			v.Caps.CellW, v.Caps.CellH = 2, 4
			runViewer(t, v)
			assertOrder(t, out.String(), altEnter, tt.want, "1/1", altLeave)
		})
	}
}

func TestRunErrorImages(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.png")
	if err := os.WriteFile(bad, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, want string
	}{
		{"missing", filepath.Join(dir, "missing.png"), "image not found"},
		{"undecodable", bad, "cannot display image"},
		{"too large", writeHugePNG(t, dir, 20000, 20000), "image too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, out := newTestViewer([]string{tt.path}, imgrender.ProtoKitty, strings.NewReader("q"))
			runViewer(t, v)
			s := out.String()
			assertOrder(t, s, altEnter, tt.want, filepath.Base(tt.path), "1/1", altLeave)
			if strings.Contains(s, "\x1b_G") {
				t.Error("error image sent kitty graphics")
			}
		})
	}
}

func TestRunOpen(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 10, 10)
	b := writePNG(t, dir, "b.png", 10, 10)
	v, out := newTestViewer([]string{a, b}, imgrender.ProtoHalfBlocks, strings.NewReader("noq"))
	var opened []string
	v.Open = func(p string) error {
		opened = append(opened, p)
		return errors.New("no viewer installed")
	}
	runViewer(t, v)
	if len(opened) != 1 || opened[0] != b {
		t.Fatalf("opened %v, want [%s]", opened, b)
	}
	assertOrder(t, out.String(), "2/2", "open failed: no viewer installed", altLeave)
}

func TestRunResizeRedraws(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 40, 20)
	inR, inW := io.Pipe()
	defer inR.Close()
	v, out := newTestViewer([]string{a}, imgrender.ProtoHalfBlocks, inR)
	resize := make(chan struct{})
	stopped := make(chan struct{})
	v.watch = func(func() (int, int, error)) (<-chan struct{}, func()) {
		return resize, func() { close(stopped) }
	}
	widths := make(chan int)
	v.Size = func() (int, int, error) { return <-widths, 24, nil }
	done := make(chan error, 1)
	go func() { done <- v.Run() }()
	widths <- 80         // initial draw
	resize <- struct{}{} // received only by the loop, which then redraws
	widths <- 40         // redraw
	if _, err := inW.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	inW.Close()
	select {
	case <-stopped:
	default:
		t.Error("resize watcher not stopped")
	}
	// The footer is padded to width-1 columns: 79 then 39.
	s := out.String()
	footer := func(w int) string {
		return "\x1b[7m" + padRight("a.png  40x20  1/1    n next · p prev · o open · q close", w-1) + "\x1b[0m"
	}
	assertOrder(t, s, footer(80), footer(40), altLeave)
}

func TestRunOSPipeLeavesLaterInputUnread(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 10, 10)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	v, _ := newTestViewer([]string{a}, imgrender.ProtoHalfBlocks, r)
	if _, err := w.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	runViewer(t, v)
	// Bytes typed after the viewer closed belong to the TUI.
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Skipf("pipe deadlines unsupported: %v", err)
	}
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil || string(buf[:n]) != "x" {
		t.Fatalf("read after Run = %q, %v; want \"x\" (the viewer kept reading)", buf[:n], err)
	}
}

func TestKeyReaderStopEndsGoroutine(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	k := startKeyReader(r)
	if _, err := w.Write([]byte("n")); err != nil {
		t.Fatal(err)
	}
	if got := <-k.keys; len(got) != 1 || got[0] != Next {
		t.Fatalf("keys = %v, want [next]", got)
	}
	// The goroutine is now blocked reading an open pipe; stop must end it.
	k.stop()
	select {
	case <-k.done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader goroutine still running after stop")
	}
}

func TestRunEOFCloses(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 10, 10)
	v, out := newTestViewer([]string{a}, imgrender.ProtoHalfBlocks, strings.NewReader(""))
	runViewer(t, v)
	assertOrder(t, out.String(), altEnter, "1/1", showCursor, altLeave)
}

func TestRunSmallAndUnknownSize(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 10, 10)
	for _, size := range []func() (int, int, error){
		func() (int, int, error) { return 3, 1, nil },
		func() (int, int, error) { return 0, 0, errors.New("not a terminal") },
	} {
		v, out := newTestViewer([]string{a}, imgrender.ProtoHalfBlocks, strings.NewReader("q"))
		v.Size = size
		runViewer(t, v)
		assertOrder(t, out.String(), altEnter, altLeave)
	}
}

func TestRunNoPaths(t *testing.T) {
	v, out := newTestViewer(nil, imgrender.ProtoHalfBlocks, strings.NewReader("q"))
	if err := v.Run(); err == nil {
		t.Fatal("Run with no paths: want error")
	}
	if out.Len() != 0 {
		t.Errorf("Run with no paths wrote %q", out.String())
	}
}

func TestRunClampsIndexAndSanitizesName(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a\x1b[31m.png", 10, 10)
	v, out := newTestViewer([]string{a}, imgrender.ProtoHalfBlocks, strings.NewReader("q"))
	v.Index = 7
	runViewer(t, v)
	s := out.String()
	assertOrder(t, s, "a?[31m.png", "1/1")
	if strings.Contains(s, "a\x1b[31m") {
		t.Error("file name control characters reached the terminal")
	}
}
