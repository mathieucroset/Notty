package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/imageviewer"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// writePNG writes a w×h gradient PNG to a temp file and returns its path.
func writePNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 128, A: 255})
		}
	}
	p := filepath.Join(t.TempDir(), "shot.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return p
}

var linkRE = regexp.MustCompile(`!\[\]\(/(attachments/[^)]+)\)`)

// imgCommand types ":img <path>" and runs it.
func imgCommand(t *testing.T, m *Model, path string) {
	t.Helper()
	run(t, m, keyMsg(":"))
	run(t, m, tea.PasteMsg{Content: "img " + path})
	run(t, m, keyMsg("enter"))
}

func TestImgCommandImportsAndInsertsLink(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	src := writePNG(t, 64, 32)
	imgCommand(t, m, src)
	match := linkRE.FindStringSubmatch(m.editor.Content())
	if match == nil {
		t.Fatalf("no image link in the buffer: %q; toasts %v", m.editor.Content(), toastTexts(m))
	}
	if !strings.HasPrefix(match[1], "attachments/ideas-") || !exists(opts.Vault, match[1]) {
		t.Errorf("attachment %q missing", match[1])
	}
	lines := strings.Split(m.editor.Content(), "\n")
	if lines[1] != match[0] {
		t.Errorf("link not on its own line: %q", lines)
	}
	if !m.editor.Dirty() {
		t.Error("inserting the link did not dirty the buffer")
	}
}

func TestImportClipboardBytes(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	data, err := os.ReadFile(writePNG(t, 8, 8))
	if err != nil {
		t.Fatal(err)
	}
	run(t, m, msgs.ImportImageMsg{Data: data, Ext: "png"})
	match := linkRE.FindStringSubmatch(m.editor.Content())
	if match == nil || !exists(opts.Vault, match[1]) {
		t.Fatalf("clipboard image not imported: %q", m.editor.Content())
	}
}

func TestImportErrorsToast(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	imgCommand(t, m, filepath.Join(t.TempDir(), "missing.png"))
	if !hasToast(m, msgs.ToastError, "Could not add the image") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	notImage := filepath.Join(t.TempDir(), "notes.txt")
	writeAbs(t, notImage, []byte("hello"))
	run(t, m, msgs.ImportImageMsg{Path: notImage})
	if !hasToast(m, msgs.ToastError, "not a supported image") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if strings.Contains(m.editor.Content(), "attachments") {
		t.Error("a failed import inserted a link")
	}
}

func writeAbs(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLargeImportAsksFirst(t *testing.T) {
	opts := testOptions(t)
	opts.Config.Images.MaxImportMB = 1
	m := openNote(t, opts, "ideas.md")
	big := filepath.Join(t.TempDir(), "big.png")
	data, err := os.ReadFile(writePNG(t, 8, 8))
	if err != nil {
		t.Fatal(err)
	}
	writeAbs(t, big, append(data, make([]byte, 1536*1024)...))

	run(t, m, msgs.ImportImageMsg{Path: big})
	o := m.topOverlay()
	if o == nil || o.dialog.ID() != dlgImportLarge || !strings.Contains(screen(m), "1.5 MB") {
		t.Fatalf("no size confirmation:\n%s", screen(m))
	}
	run(t, m, keyMsg("esc"))
	if strings.Contains(m.editor.Content(), "attachments") {
		t.Fatal("cancelled import inserted a link")
	}

	run(t, m, msgs.ImportImageMsg{Path: big})
	run(t, m, keyMsg("enter"))
	if linkRE.FindString(m.editor.Content()) == "" {
		t.Errorf("confirmed import not inserted: %q, toasts %v", m.editor.Content(), toastTexts(m))
	}
}

func TestImportWithoutOpenNote(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.ImportImageMsg{Path: writePNG(t, 4, 4)})
	if !hasToast(m, msgs.ToastInfo, "Open a note") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}

// halfBlockOptions renders preview images as half-blocks.
func halfBlockOptions(t *testing.T) Options {
	opts := testOptions(t)
	opts.Caps = imgrender.Caps{Inline: imgrender.ProtoHalfBlocks, Viewer: imgrender.ProtoHalfBlocks, CellW: 8, CellH: 16}
	return opts
}

func TestPreviewShowsImportedImageAndOpensViewer(t *testing.T) {
	var got *imageviewer.Viewer
	oldExec := execCommand
	execCommand = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		got, _ = c.(*imageviewer.Viewer)
		return func() tea.Msg { return fn(nil) }
	}
	t.Cleanup(func() { execCommand = oldExec })

	opts := halfBlockOptions(t)
	m := openNote(t, opts, "ideas.md")
	imgCommand(t, m, writePNG(t, 64, 32))
	run(t, m, keyMsg("ctrl+g"))
	run(t, m, keyMsg("ctrl+g"))
	if !strings.Contains(screen(m), "▀") {
		t.Fatalf("preview does not draw the image with half-blocks:\n%s", screen(m))
	}
	// An overlay swaps the image for a chip.
	run(t, m, msgs.OpenHelpMsg{})
	if strings.Contains(screen(m), "▀") {
		t.Error("image still drawn under an overlay")
	}
	run(t, m, keyMsg("esc"))
	if !strings.Contains(screen(m), "▀") {
		t.Error("image not back after the overlay closed")
	}

	gen := m.kittyGen
	pressKeys(t, m, "]", "i")
	out := run(t, m, keyMsg("enter"))
	if got == nil || len(got.Paths) != 1 || !strings.Contains(got.Paths[0], "attachments") {
		t.Fatalf("viewer not opened on the attachment: %+v", got)
	}
	if m.kittyGen != gen+1 || !hasMsg[readyTickMsg](out) {
		t.Error("no ready tick after the viewer closed")
	}
	if hasMsg[tea.QuitMsg](out) {
		t.Error("closing the viewer quit the app")
	}
}

func TestImageViewerCtrlQQuits(t *testing.T) {
	oldExec := execCommand
	execCommand = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		c.(*imageviewer.Viewer).QuitRequested = true
		return func() tea.Msg { return fn(nil) }
	}
	t.Cleanup(func() { execCommand = oldExec })
	m := openNote(t, testOptions(t), "ideas.md")
	out := run(t, m, msgs.OpenImageViewerMsg{Paths: []string{writePNG(t, 4, 4)}})
	if !hasMsg[tea.QuitMsg](out) {
		t.Error("ctrl+q in the viewer did not quit")
	}
}
