package imageviewer

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/charmbracelet/x/term"

	"github.com/mathieucroset/notty/internal/imgrender"
)

// Terminal sequences written by the viewer.
const (
	seqEnter = "\x1b[?1049h\x1b[?25l"
	seqLeave = "\x1b[0m\x1b[?25h\x1b[?1049l"
	seqClear = "\x1b[0m\x1b[H\x1b[2J"
)

// kittyID is the Kitty image id used by the viewer. The viewer runs on its
// own alternate screen, whose image store is wiped on entry, so it cannot
// collide with the preview's images.
const kittyID uint32 = 0xFFFFFE

const hints = "n next · p prev · o open · q close"

// fallbackCols and fallbackRows are used when the terminal size is unknown.
const (
	fallbackCols = 80
	fallbackRows = 24
)

// Viewer is the full-screen image viewer. It implements tea.ExecCommand, so
// the UI runs it with tea.Exec: Bubble Tea releases the terminal, calls the
// Set* methods with its own input and output, and then calls Run.
type Viewer struct {
	// Paths are the images of the note; Index is the one shown first (0
	// when out of range). Run updates Index as the user navigates.
	Paths []string
	Index int
	// Caps.Viewer picks the protocol; CellW and CellH size the image.
	Caps imgrender.Caps

	// Size returns the terminal size. Default: term.GetSize on stdout (or
	// stdin when stdout is not a terminal).
	Size func() (cols, rows int, err error)
	// Open opens path with the system image viewer. Default: xdg-open,
	// open, or cmd /c start.
	Open func(path string) error

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	// watch reports terminal resizes until stop is called (test hook).
	watch func(size func() (int, int, error)) (resized <-chan struct{}, stop func())
}

// New returns a viewer for paths starting at index.
func New(paths []string, index int, caps imgrender.Caps) *Viewer {
	return &Viewer{Paths: paths, Index: index, Caps: caps}
}

// SetStdin sets the terminal input (tea.ExecCommand).
func (v *Viewer) SetStdin(r io.Reader) { v.stdin = r }

// SetStdout sets the terminal output (tea.ExecCommand).
func (v *Viewer) SetStdout(w io.Writer) { v.stdout = w }

// SetStderr sets the error output (tea.ExecCommand). The viewer does not
// write to it; it is kept for the interface.
func (v *Viewer) SetStderr(w io.Writer) { v.stderr = w }

// fder is a file with a descriptor, such as *os.File.
type fder interface{ Fd() uintptr }

// Run takes over the terminal and shows the images until the user closes
// the viewer (q or esc) or the input ends. The terminal state (raw mode,
// alternate screen, cursor, Kitty images) is restored on every exit path,
// including panics.
func (v *Viewer) Run() (err error) {
	if len(v.Paths) == 0 {
		return errors.New("image viewer: no images")
	}
	in, out := v.stdin, v.stdout
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	s := &session{v: v, out: out, size: v.Size, index: clampIndex(v.Index, len(v.Paths))}
	if s.size == nil {
		s.size = defaultSize(out, in)
	}

	if f, ok := in.(fder); ok && term.IsTerminal(f.Fd()) {
		state, rawErr := term.MakeRaw(f.Fd())
		if rawErr != nil {
			return fmt.Errorf("image viewer: raw mode: %w", rawErr)
		}
		defer func() { _ = term.Restore(f.Fd(), state) }()
	}
	defer enableVT(out)()

	s.write(seqEnter)
	defer func() {
		s.deleteKitty()
		s.write(seqLeave)
		if err == nil && s.err != nil {
			err = fmt.Errorf("image viewer: write: %w", s.err)
		}
	}()

	keys := startKeyReader(in)
	defer keys.stop()
	watch := v.watch
	if watch == nil {
		watch = watchResize
	}
	resized, stopWatch := watch(s.size)
	defer stopWatch()

	s.draw()
	for {
		select {
		case batch := <-keys.keys:
			if s.handle(batch) {
				return nil
			}
		case <-keys.done:
			return nil // input closed
		case <-resized:
			s.draw()
		}
	}
}

func clampIndex(i, n int) int {
	if i < 0 || i >= n {
		return 0
	}
	return i
}

// session is the state of one Run.
type session struct {
	v     *Viewer
	out   io.Writer
	size  func() (int, int, error)
	err   error // first write error
	index int

	// The decoded current image (or the error), loaded lazily per index.
	loaded       int // index+1 of the loaded image; 0 when none
	img          image.Image
	imgErr       error
	imgW, imgH   int
	status       string // replaces the hints in the footer
	cols, rows   int    // size of the last draw
	kittyVisible bool
}

// handle applies a batch of keys and reports whether the viewer should close.
func (s *session) handle(batch []Key) bool {
	n := len(s.v.Paths)
	for _, k := range batch {
		switch k {
		case Quit:
			return true
		case Next, Prev:
			if k == Next {
				s.index = (s.index + 1) % n
			} else {
				s.index = (s.index + n - 1) % n
			}
			s.v.Index = s.index
			s.status = ""
			s.draw()
		case Open:
			s.status = ""
			if err := s.open(s.v.Paths[s.index]); err != nil {
				s.status = "open failed: " + err.Error()
			}
			var b strings.Builder
			s.footer(&b)
			s.write(b.String())
		case Unknown:
		}
	}
	return false
}

func (s *session) open(path string) error {
	if s.v.Open != nil {
		return s.v.Open(path)
	}
	return openWithSystem(path)
}

func (s *session) write(str string) {
	if s.err != nil || str == "" {
		return
	}
	_, s.err = io.WriteString(s.out, str)
}

// wrap wraps a graphics sequence for tmux passthrough when needed.
func (s *session) wrap(seq string) string {
	if s.v.Caps.TmuxPassthrough && seq != "" {
		return imgrender.WrapTmux(seq)
	}
	return seq
}

func (s *session) deleteKitty() {
	if s.kittyVisible {
		s.write(s.wrap(imgrender.KittyDelete(kittyID)))
		s.kittyVisible = false
	}
}

func (s *session) cellSize() (w, h int) {
	w, h = s.v.Caps.CellW, s.v.Caps.CellH
	if w <= 0 || h <= 0 {
		return 8, 16
	}
	return w, h
}

// load decodes the current image once per index.
func (s *session) load() {
	if s.loaded == s.index+1 {
		return
	}
	path := s.v.Paths[s.index]
	s.loaded = s.index + 1
	s.img, s.imgErr = imgrender.Decode(path)
	if s.imgErr == nil {
		b := s.img.Bounds()
		s.imgW, s.imgH = b.Dx(), b.Dy()
		return
	}
	s.img = nil
	// Dimensions still reports the size of an oversized image.
	s.imgW, s.imgH, _ = imgrender.Dimensions(path)
}

// draw clears the screen and draws the current image and the footer.
func (s *session) draw() {
	cols, rows, err := s.size()
	if err != nil || cols <= 0 || rows <= 0 {
		cols, rows = fallbackCols, fallbackRows
	}
	s.cols, s.rows = cols, rows
	s.deleteKitty()
	s.load()

	var b strings.Builder
	b.WriteString(seqClear)
	area := max(rows-1, 1) // the last row is the footer
	if s.imgErr != nil {
		msg := ansi.Truncate(errorMessage(s.imgErr), cols, "…")
		w := ansi.StringWidth(msg)
		b.WriteString(cup(area/2+1, (cols-w)/2+1) + msg)
	} else {
		s.image(&b, cols, area)
	}
	s.footer(&b)
	s.write(b.String())
}

// image draws the current image centered in cols x area cells.
func (s *session) image(b *strings.Builder, cols, area int) {
	cw, ch := s.cellSize()
	c, r := imgrender.FitCells(s.imgW, s.imgH, cols, area, cw, ch)
	top, left := (area-r)/2+1, (cols-c)/2+1
	switch s.v.Caps.Viewer {
	case imgrender.ProtoKitty:
		if s.v.Caps.TmuxPassthrough {
			// tmux does not track where a direct placement lands; unicode
			// placeholders are plain text that tmux positions itself.
			b.WriteString(s.wrap(imgrender.KittyTransmit(s.img, kittyID, c, r)))
			for i, line := range imgrender.KittyPlaceholders(kittyID, c, r) {
				b.WriteString(cup(top+i, left) + line)
			}
		} else {
			b.WriteString(cup(top, left) + kittyDirect(s.img, kittyID, c, r, c*cw, r*ch))
		}
		s.kittyVisible = true
	case imgrender.ProtoSixel:
		b.WriteString(cup(top, left) + s.wrap(imgrender.Sixel(s.img, c*cw, r*ch)))
	case imgrender.ProtoITerm:
		b.WriteString(cup(top, left) + s.wrap(imgrender.ITerm(s.img, c*cw, r*ch)))
	default: // ProtoHalfBlocks, ProtoOff
		for i, line := range imgrender.HalfBlocks(s.img, c, r) {
			b.WriteString(cup(top+i, left) + line)
		}
	}
}

// footer writes the footer on the last row, padded to one column less than
// the width so the terminal never scrolls.
func (s *session) footer(b *strings.Builder) {
	name := sanitize(filepath.Base(s.v.Paths[s.index]))
	left := name
	if s.imgW > 0 && s.imgH > 0 {
		left += "  " + strconv.Itoa(s.imgW) + "x" + strconv.Itoa(s.imgH)
	}
	left += "  " + strconv.Itoa(s.index+1) + "/" + strconv.Itoa(len(s.v.Paths))
	right := hints
	if s.status != "" {
		right = sanitize(s.status)
	}
	width := max(s.cols-1, 1)
	b.WriteString(cup(s.rows, 1) + "\x1b[7m" + padRight(left+"    "+right, width) + "\x1b[0m")
}

// padRight truncates or pads text to exactly width columns.
func padRight(text string, width int) string {
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(width-ansi.StringWidth(text), 0))
}

// sanitize replaces control characters so names cannot inject sequences.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return '?'
		}
		return r
	}, s)
}

func errorMessage(err error) string {
	switch {
	case errors.Is(err, imgrender.ErrImageTooLarge):
		return "image too large"
	case errors.Is(err, fs.ErrNotExist):
		return "image not found"
	}
	return sanitize("cannot display image: " + err.Error())
}

// cup moves the cursor to the 1-based row and column.
func cup(row, col int) string {
	return "\x1b[" + strconv.Itoa(row) + ";" + strconv.Itoa(col) + "H"
}

// kittyDirect transmits img as PNG and places it at the cursor over cols x
// rows cells without moving the cursor (a=T,C=1). The image is downscaled to
// at most pxW x pxH pixels; Kitty scales it to the cells.
func kittyDirect(img image.Image, id uint32, cols, rows, pxW, pxH int) string {
	if img == nil || cols <= 0 || rows <= 0 {
		return ""
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > pxW || h > pxH {
		r := math.Min(float64(pxW)/float64(w), float64(pxH)/float64(h))
		w, h = max(int(math.Round(float64(w)*r)), 1), max(int(math.Round(float64(h)*r)), 1)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, imgrender.Scale(img, w, h)); err != nil {
		return ""
	}
	control := "a=T,f=100,q=2,C=1,i=" + strconv.FormatUint(uint64(id), 10) +
		",c=" + strconv.Itoa(cols) + ",r=" + strconv.Itoa(rows)
	payload := base64.StdEncoding.EncodeToString(data.Bytes())
	if len(payload) <= kitty.MaxChunkSize {
		return "\x1b_G" + control + ";" + payload + "\x1b\\"
	}
	var out strings.Builder
	for i := 0; i < len(payload); i += kitty.MaxChunkSize {
		end := min(i+kitty.MaxChunkSize, len(payload))
		ctrl, m := "q=2", ",m=1;"
		if i == 0 {
			ctrl = control
		}
		if end == len(payload) {
			m = ",m=0;"
		}
		out.WriteString("\x1b_G" + ctrl + m + payload[i:end] + "\x1b\\")
	}
	return out.String()
}

// defaultSize returns a Size function reading the size of the first of
// files that is a terminal.
func defaultSize(files ...any) func() (int, int, error) {
	return func() (int, int, error) {
		for _, f := range files {
			if f, ok := f.(fder); ok && term.IsTerminal(f.Fd()) {
				w, h, err := term.GetSize(f.Fd())
				if err != nil {
					return 0, 0, fmt.Errorf("terminal size: %w", err)
				}
				return w, h, nil
			}
		}
		return 0, 0, errors.New("terminal size: not a terminal")
	}
}

// openWithSystem opens path with the platform's default application
// without waiting for it.
func openWithSystem(path string) error {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// keyReader reads and decodes keys in a goroutine.
type keyReader struct {
	keys chan []Key
	done chan struct{} // closed when the goroutine exits
	quit chan struct{}
	cr   interface {
		Cancel() bool
		Close() error
	}
}

// startKeyReader starts reading in. A terminal (or any file the platform
// can poll) is read through a cancelable reader, so stop leaves no
// goroutine reading bytes that belong to Bubble Tea afterwards. Other
// readers cannot be interrupted; their goroutine ends at EOF.
func startKeyReader(in io.Reader) *keyReader {
	k := &keyReader{keys: make(chan []Key), done: make(chan struct{}), quit: make(chan struct{})}
	src := in
	if cr, err := uv.NewCancelReader(in); err == nil {
		k.cr, src = cr, cr
	}
	go func() {
		defer close(k.done)
		buf := make([]byte, 256)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				if keys := DecodeKeys(buf[:n]); len(keys) > 0 {
					select {
					case k.keys <- keys:
					case <-k.quit:
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return k
}

// stop ends the reader. For a cancelable reader it waits until the
// goroutine has exited.
func (k *keyReader) stop() {
	close(k.quit)
	if k.cr == nil {
		return
	}
	if k.cr.Cancel() {
		select {
		case <-k.done:
		case <-time.After(time.Second):
		}
	}
	_ = k.cr.Close()
}
