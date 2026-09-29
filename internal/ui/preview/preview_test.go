package preview

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// --- helpers ---------------------------------------------------------------

// img is the default icon set's image glyph, which leads every chip.
var img = icons.Default().Image

func testPalette(t *testing.T) theme.Palette {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	return p
}

func newTest(t *testing.T, proto imgrender.Protocol, vault string) Model {
	t.Helper()
	p := testPalette(t)
	m := New(theme.NewStyles(p), p, imgrender.Caps{Inline: proto, CellW: 8, CellH: 16}, vault)
	m.debounce = time.Millisecond
	return m.SetSize(60, 30)
}

// run executes cmd and everything it leads to: preview messages are fed
// back into Update, tea.Raw payloads are collected in raws, anything else
// (messages for the app) in out.
func run(m Model, cmd tea.Cmd) (Model, []string, []tea.Msg) {
	var raws []string
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case tea.RawMsg:
			raws = append(raws, fmt.Sprint(msg.Msg))
		case renderTickMsg, renderedMsg:
			var next tea.Cmd
			m, next = m.Update(msg)
			queue = append(queue, next)
		default:
			out = append(out, msg)
		}
	}
	return m, raws, out
}

func setContent(t *testing.T, m Model, path, content string) (Model, []string) {
	t.Helper()
	m, cmd := m.SetContent(path, content)
	m, raws, _ := run(m, cmd)
	return m, raws
}

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 5), 200, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// lineOf returns the index of the first view line containing s, or -1.
func lineOf(view, s string) int {
	for i, l := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

func checkDims(t *testing.T, view string, w, h int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Fatalf("view has %d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("line %d has width %d, want %d: %q", i, got, w, l)
		}
	}
}

// --- rendering -------------------------------------------------------------

func TestRenderShowsGlamourText(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "n.md", "# Hello\n\nSome **bold** text.")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Hello") || !strings.Contains(v, "Some bold text.") {
		t.Fatalf("view misses rendered text:\n%s", v)
	}
	if strings.Contains(v, "**") {
		t.Fatalf("markdown not rendered:\n%s", v)
	}
}

func TestGlamourPanicFallsBackToRawLines(t *testing.T) {
	orig := renderMarkdown
	t.Cleanup(func() { renderMarkdown = orig })
	renderMarkdown = func(tr *glamour.TermRenderer, md string) (string, error) {
		if strings.Contains(md, "boom") {
			panic("bad input")
		}
		return orig(tr, md)
	}
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "n.md", "# Fine\n\nboom **raw**")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Fine") || !strings.Contains(v, "boom **raw**") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestSegmentCacheReuse(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "n.md", "# A\n\npara b\n\npara c")
	if got := m.sh.glamourCalls.Load(); got != 3 {
		t.Fatalf("first render: %d glamour calls, want 3", got)
	}
	m, _ = setContent(t, m, "n.md", "# A\n\npara b changed\n\npara c")
	if got := m.sh.glamourCalls.Load(); got != 4 {
		t.Fatalf("after one change: %d glamour calls, want 4", got)
	}
	m, _ = setContent(t, m, "n.md", "# A\n\npara b changed\n\npara c")
	if got := m.sh.glamourCalls.Load(); got != 4 {
		t.Fatalf("unchanged content: %d glamour calls, want 4", got)
	}
	// A new width renders everything again.
	m = m.SetSize(40, 30)
	m, cmd := m.Refresh()
	m, _, _ = run(m, cmd)
	if got := m.sh.glamourCalls.Load(); got != 7 {
		t.Fatalf("after resize: %d glamour calls, want 7", got)
	}
	// And the theme is part of the key.
	p, _ := theme.Get("catppuccin-latte")
	m, cmd = m.SetTheme(theme.NewStyles(p), p)
	m, _, _ = run(m, cmd)
	if got := m.sh.glamourCalls.Load(); got != 10 {
		t.Fatalf("after theme change: %d glamour calls, want 10", got)
	}
	// The key is the palette's identity, not its name: a rewritten user
	// theme keeps its name but gets a new ID.
	p.ID = p.Name + "@rewritten"
	m, cmd = m.SetTheme(theme.NewStyles(p), p)
	_, _, _ = run(m, cmd)
	if got := m.sh.glamourCalls.Load(); got != 13 {
		t.Fatalf("after same-name palette change: %d glamour calls, want 13", got)
	}
}

func TestTabsAndCarriageReturnsNeverReachTheFrame(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	content := "```go\r\nfunc a() {\r\n\treturn\r\n}\r\n```\r\n\r\ntext\twith tab\r\n\r\n| a\tb | c |\n|---|---|\n| x | y |"
	m, _ = setContent(t, m, "n.md", content)
	v := m.View()
	if strings.ContainsAny(v, "\t\r") {
		t.Fatalf("view contains a tab or CR: %q", v)
	}
	checkDims(t, v, 60, 30)
	if !strings.Contains(ansi.Strip(v), "    return") {
		t.Fatalf("tab not expanded to 4 columns:\n%s", ansi.Strip(v))
	}
}

func TestExpandTabs(t *testing.T) {
	cases := map[string]string{
		"\tx":     "    x",
		"ab\tx":   "ab  x",
		"abcd\tx": "abcd    x",
		"é\tx":    "é   x",
		"no tabs": "no tabs",
	}
	for in, want := range cases {
		if got := expandTabs(in); got != want {
			t.Errorf("expandTabs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetContentIsDebouncedAndStaleRendersDropped(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, first := m.SetContent("n.md", "old text")
	m, second := m.SetContent("n.md", "new text")
	m, _, _ = run(m, first) // stale tick: nothing renders
	if m.doc != nil {
		t.Fatal("stale tick rendered")
	}
	m, _, _ = run(m, second)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "new text") || strings.Contains(v, "old text") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestStaleRenderResultDropped(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = m.SetContent("n.md", "first")
	stale := m.startRender()().(renderedMsg)
	m, _ = m.SetContent("n.md", "second")
	m, _ = m.Update(stale)
	if m.doc != nil {
		t.Fatal("stale render applied")
	}
}

func TestOnlyOneRenderInFlight(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, tick := m.SetContent("n.md", "first")
	m, render1 := m.Update(tick())
	if render1 == nil {
		t.Fatal("tick did not start a render")
	}
	m, tick = m.SetContent("n.md", "second")
	m, c := m.Update(tick())
	if c != nil {
		t.Fatal("second render started while the first runs")
	}
	m, tick = m.SetContent("n.md", "third")
	m, c = m.Update(tick())
	if c != nil {
		t.Fatal("third render started while the first runs")
	}
	// The stale first render returns: not applied, and exactly one new
	// render starts for the latest content.
	m, render2 := m.Update(render1())
	if m.doc != nil {
		t.Fatal("stale render applied")
	}
	if render2 == nil {
		t.Fatal("no follow-up render after the dirty one returned")
	}
	msg := render2()
	if _, ok := msg.(renderedMsg); !ok {
		t.Fatalf("follow-up is %T, want a single render", msg)
	}
	m, c = m.Update(msg)
	if c != nil {
		t.Fatal("another render started after the latest one")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "third") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestSizeChangeRerendersOnNextUpdate(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "n.md", strings.Repeat("word ", 30))
	m = m.SetSize(30, 10)
	m, cmd := m.Update(struct{}{})
	if cmd == nil {
		t.Fatal("no render scheduled after resize")
	}
	m, _, _ = run(m, cmd)
	if m.doc.width != 30 {
		t.Fatalf("doc width %d, want 30", m.doc.width)
	}
}

func TestMissingImageRendersWarningChip(t *testing.T) {
	for _, proto := range []imgrender.Protocol{imgrender.ProtoKitty, imgrender.ProtoHalfBlocks, imgrender.ProtoOff} {
		m := newTest(t, proto, t.TempDir())
		m, _ = setContent(t, m, "notes/n.md", "![x](nope.png)\n\n![y](https://example.com/y.png)")
		v := ansi.Strip(m.View())
		if !strings.Contains(v, img+" nope.png  (missing)") {
			t.Fatalf("%v: no missing chip:\n%s", proto, v)
		}
		if !strings.Contains(v, img+" y.png  (external)") {
			t.Fatalf("%v: no external chip:\n%s", proto, v)
		}
	}
}

func TestUndecodableImageRendersWarningChip(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "bad.png"), []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTest(t, imgrender.ProtoHalfBlocks, vault)
	m, _ = setContent(t, m, "n.md", "![](/bad.png)")
	if v := ansi.Strip(m.View()); !strings.Contains(v, img+" bad.png  (unreadable)") {
		t.Fatalf("no unreadable chip:\n%s", v)
	}
}

func TestHalfBlockImageAndOverlayChips(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "img", "pic.png"), 160, 96)
	m := newTest(t, imgrender.ProtoHalfBlocks, vault)
	m, raws := setContent(t, m, "notes/n.md", "Intro\n\n![pic](../img/pic.png)\n\nOutro")
	if len(raws) != 0 {
		t.Fatalf("half-blocks sent raw output: %q", raws)
	}
	v := m.View()
	if !strings.Contains(v, "▀") {
		t.Fatalf("no half-block image:\n%s", ansi.Strip(v))
	}
	// 160x96 px (20x6 cells at 8x16 px) grows to the 58-column content
	// width: 58x17 cells, within 60% of the 30-row pane and the 160x48
	// half-block sample resolution.
	if got := m.doc.imgRows[0]; got != 17 {
		t.Fatalf("image rows = %d, want 17", got)
	}
	m = m.SetOverlayOpen(true)
	ov := m.View()
	if strings.Contains(ov, "▀") {
		t.Fatal("image still drawn while overlay open")
	}
	if !strings.Contains(ansi.Strip(ov), img+" pic.png") {
		t.Fatalf("no chip while overlay open:\n%s", ansi.Strip(ov))
	}
	if lineOf(ov, "Outro") != lineOf(v, "Outro") || lineOf(v, "Outro") < 0 {
		t.Fatal("overlay swap moved the layout")
	}
	m = m.SetOverlayOpen(false)
	if !strings.Contains(m.View(), "▀") {
		t.Fatal("image not back after overlay closed")
	}
}

func TestImagesCappedAtPaneWidthAndSixtyPercentHeight(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "wide.png"), 1600, 200)
	writePNG(t, filepath.Join(vault, "tall.png"), 200, 1600)
	m := newTest(t, imgrender.ProtoHalfBlocks, vault)
	m, _ = setContent(t, m, "n.md", "![](wide.png)\n\n![](tall.png)")
	cols := ansi.StringWidth(m.doc.images[0].rows[0])
	if cols != 58 {
		t.Fatalf("wide image %d cols, want 58 (pane 60 - 2)", cols)
	}
	if got := m.doc.imgRows[1]; got != 18 {
		t.Fatalf("tall image %d rows, want 18 (60%% of 30)", got)
	}
}

func TestImagesOffRenderPlainChips(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "pic.png"), 32, 32)
	m := newTest(t, imgrender.ProtoOff, vault)
	m, _ = setContent(t, m, "n.md", "![](pic.png)")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, img+" pic.png") || strings.Contains(v, "(missing)") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestInlineImageBecomesChipAndBlock(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "notes", "pic.png"), 32, 32)
	m := newTest(t, imgrender.ProtoHalfBlocks, vault)
	m, _ = setContent(t, m, "notes/n.md", "look ![alt](pic.png) and ![](pic.png) here")
	v := m.View()
	text := strings.Split(ansi.Strip(v), "\n")[0]
	for _, want := range []string{"look", img + " alt", img + " pic.png", "here"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text row %q misses %q", text, want)
		}
	}
	if strings.Contains(text, "Image:") || strings.Contains(text, "/pic.png") {
		t.Fatalf("text row still shows the image link: %q", text)
	}
	if !strings.Contains(v, "▀") {
		t.Fatalf("no image block:\n%s", ansi.Strip(v))
	}
}

func TestLinksShowVaultRootTargets(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	content := "[sib](b.md) [up](../top.md) [root](/r.md) [web](https://x.org/p) [anchor](#h) `[code](c.md)`"
	m, _ = setContent(t, m, "notes/n.md", content)
	v := ansi.Strip(m.View())
	for _, want := range []string{"/notes/b.md", "/top.md", "/r.md", "https://x.org/p", "[code](c.md)"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view misses %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, " /b.md") {
		t.Fatalf("note-relative link shown as root-relative:\n%s", v)
	}
}

func TestReferenceLinksRender(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	content := "See [ref][R] and [other].\n\n[r]: http://example.com\n[other]: sub/o.md \"title\"\n\n```\n[x]: nope\n```"
	m, _ = setContent(t, m, "notes/n.md", content)
	v := ansi.Strip(m.View())
	first := strings.Split(v, "\n")[0]
	if strings.Contains(first, "[ref]") || !strings.Contains(first, "http://example.com") || !strings.Contains(v, "/notes/sub/o.md") {
		t.Fatalf("reference links not resolved:\n%s", v)
	}
}

func TestReferenceDefsNotAppendedIntoOpenFence(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "n.md", "[a]: x.md\n\n```\ncode [a]\n")
	if v := ansi.Strip(m.View()); strings.Contains(v, "x.md") {
		t.Fatalf("definition leaked into the open code block:\n%s", v)
	}
}

func TestCodeSpans(t *testing.T) {
	cases := map[string][][2]int{
		"a `b` c":       {{2, 5}},
		"``x ` y`` z":   {{0, 9}},
		"`open only":    nil,
		"`a` and `b`":   {{0, 3}, {8, 11}},
		"``` not ``` x": {{0, 11}},
	}
	for in, want := range cases {
		got := codeSpans(in)
		if len(got) != len(want) {
			t.Errorf("codeSpans(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("codeSpans(%q) = %v, want %v", in, got, want)
			}
		}
	}
}

// --- kitty -----------------------------------------------------------------

func kittyNote(t *testing.T) (Model, string) {
	t.Helper()
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "attachments", "a.png"), 64, 64)
	m := newTest(t, imgrender.ProtoKitty, vault)
	return m, "text\n\n![](/attachments/a.png)"
}

func TestKittyTransmitHeldUntilTerminalReady(t *testing.T) {
	m, content := kittyNote(t)
	m, raws := setContent(t, m, "n.md", content)
	if len(raws) != 0 {
		t.Fatalf("transmitted before the terminal was ready: %d raws", len(raws))
	}
	if !strings.ContainsRune(m.View(), '\U0010EEEE') {
		t.Fatal("no placeholder cells in the view")
	}
	id := m.doc.images[0].kittyID
	if id == 0 || id > imgrender.MaxDynamicKittyID {
		t.Fatalf("kitty id %d outside the dynamic range", id)
	}

	m, cmd := m.SetTerminalReady()
	m, raws, _ = run(m, cmd)
	want := fmt.Sprintf("a=T,f=100,q=2,i=%d,", id)
	if len(raws) != 1 || !strings.Contains(raws[0], want) {
		t.Fatalf("after ready: raws %d, want one transmit containing %q", len(raws), want)
	}
	if _, cmd := m.SetTerminalReady(); cmd != nil {
		t.Fatal("second SetTerminalReady transmitted again")
	}

	// Re-rendering the same note does not re-send.
	m, raws = setContent(t, m, "n.md", content+"\n\nmore")
	if len(raws) != 0 {
		t.Fatalf("re-render re-sent %d raws", len(raws))
	}

	if got, want := m.KittyCleanup(), imgrender.KittyDelete(id); got != want {
		t.Fatalf("KittyCleanup = %q, want %q", got, want)
	}

	// For tea.Exec: forget everything and hold transmissions again.
	m, cmd = m.ResetKittyState()
	if cmd != nil {
		t.Fatal("ResetKittyState returned a command")
	}
	if m.KittyCleanup() != "" {
		t.Fatal("ids still recorded as sent after ResetKittyState")
	}
	m, raws = setContent(t, m, "n.md", content)
	if len(raws) != 0 {
		t.Fatal("transmitted after ResetKittyState before the terminal was ready")
	}
	// Ready again: the image is re-encoded and re-transmitted, same id.
	m, cmd = m.SetTerminalReady()
	m, raws, _ = run(m, cmd)
	if len(raws) != 1 || !strings.Contains(raws[0], want) {
		t.Fatalf("after reset + ready: raws %d, want one re-transmit containing %q", len(raws), want)
	}
	if got := m.doc.images[0].kittyID; got != id {
		t.Fatalf("id changed from %d to %d", id, got)
	}
}

func TestKittyReencodesAfterResetWithoutNewContent(t *testing.T) {
	m, content := kittyNote(t)
	m, _ = m.SetTerminalReady()
	m, raws := setContent(t, m, "n.md", content)
	if len(raws) != 1 {
		t.Fatalf("raws = %d, want 1", len(raws))
	}
	if m.doc.images[0].transmit != "" {
		t.Fatal("transmission kept after it was written")
	}
	m, _ = m.ResetKittyState()
	m, cmd := m.SetTerminalReady()
	if cmd == nil {
		t.Fatal("no re-encode requested")
	}
	_, raws, _ = run(m, cmd)
	if len(raws) != 1 || !strings.Contains(raws[0], "\x1b_Ga=T") {
		t.Fatalf("raws = %d, want one re-transmit", len(raws))
	}
}

func TestKittyTransmitAfterReadyGoesOutWithRender(t *testing.T) {
	m, content := kittyNote(t)
	m, _ = m.SetTerminalReady()
	_, raws := setContent(t, m, "n.md", content)
	if len(raws) != 1 || !strings.Contains(raws[0], "\x1b_Ga=T") {
		t.Fatalf("raws = %d, want one transmit", len(raws))
	}
}

func TestKittyImagesNoLongerShownAreDeleted(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "a.png"), 32, 32)
	writePNG(t, filepath.Join(vault, "b.png"), 32, 32)
	m := newTest(t, imgrender.ProtoKitty, vault)
	m, _ = m.SetTerminalReady()
	m, _ = setContent(t, m, "n.md", "![](a.png)")
	idA := m.doc.images[0].kittyID
	m, raws := setContent(t, m, "n.md", "![](b.png)")
	idB := m.doc.images[0].kittyID
	all := strings.Join(raws, "")
	if !strings.Contains(all, imgrender.KittyDelete(idA)) {
		t.Fatal("image no longer shown not deleted")
	}
	if !strings.Contains(all, fmt.Sprintf("i=%d,", idB)) {
		t.Fatal("new image not transmitted")
	}
	if got := m.KittyCleanup(); got != imgrender.KittyDelete(idB) {
		t.Fatalf("KittyCleanup = %q, want only b", got)
	}
}

func TestKittyOldSizeDeletedOnResize(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "p.png"), 400, 64) // 50x4 cells
	m := newTest(t, imgrender.ProtoKitty, vault)
	m, _ = m.SetTerminalReady()
	m, _ = setContent(t, m, "n.md", "![](p.png)")
	for w := 45; w > 35; w-- {
		old := m.doc.images[0].kittyID
		m = m.SetSize(w, 30)
		var cmd tea.Cmd
		m, cmd = m.Refresh()
		var raws []string
		m, raws, _ = run(m, cmd)
		if !strings.Contains(strings.Join(raws, ""), imgrender.KittyDelete(old)) {
			t.Fatalf("width %d: old size %d not deleted", w, old)
		}
		if len(m.sh.sent) != 1 || !m.sh.sent[m.doc.images[0].kittyID] {
			t.Fatalf("width %d: sent = %v, want only the shown id", w, m.sh.sent)
		}
	}
}

func TestKittyManyImagesStayStable(t *testing.T) {
	vault := t.TempDir()
	var b strings.Builder
	for i := range 70 {
		writePNG(t, filepath.Join(vault, fmt.Sprintf("p%d.png", i)), 16, 16)
		fmt.Fprintf(&b, "![](p%d.png)\n\n", i)
	}
	m := newTest(t, imgrender.ProtoKitty, vault)
	m, _ = m.SetTerminalReady()
	m, raws := setContent(t, m, "n.md", b.String())
	if got := strings.Count(strings.Join(raws, ""), "a=T"); got != 70 {
		t.Fatalf("first render transmitted %d images, want 70", got)
	}
	for r := range 3 {
		m, raws = setContent(t, m, "n.md", b.String()+strings.Repeat("x", r+1))
		if len(raws) != 0 {
			t.Fatalf("re-render %d sent %d raws, want none", r, len(raws))
		}
	}
	if len(m.sh.sent) != 70 {
		t.Fatalf("sent = %d ids, want 70", len(m.sh.sent))
	}
}

func TestKittyStaleRenderNeverDeletesShownImages(t *testing.T) {
	m, content := kittyNote(t)
	m, _ = m.SetTerminalReady()
	m, _ = setContent(t, m, "n.md", content)
	id := m.doc.images[0].kittyID
	stale := m.startRender()().(renderedMsg)
	m.sh.imgCache.Clear()
	m, cmd := m.SetContent("n.md", "no images")
	m, c := m.Update(stale)
	if _, raws, _ := run(m, c); len(raws) != 0 {
		t.Fatalf("stale render wrote %q", raws)
	}
	if !m.sh.sent[id] {
		t.Fatal("stale render forgot a shown id")
	}
	_, raws, _ := run(m, cmd)
	if strings.Join(raws, "") != imgrender.KittyDelete(id) {
		t.Fatalf("current render wrote %q, want the delete of %d", raws, id)
	}
}

func TestKittyIDsUniqueAcrossPreviews(t *testing.T) {
	m1, content := kittyNote(t)
	m2 := New(m1.styles, m1.palette, m1.caps, m1.vaultRoot).SetSize(60, 30)
	m2.debounce = time.Millisecond
	m1, _ = setContent(t, m1, "n.md", content)
	m2, _ = setContent(t, m2, "n.md", content)
	if m1.doc.images[0].kittyID == m2.doc.images[0].kittyID {
		t.Fatal("two previews share a kitty id")
	}
}

func TestKittyTmuxWrapping(t *testing.T) {
	m, content := kittyNote(t)
	m.caps.TmuxPassthrough = true
	m, _ = m.SetTerminalReady()
	m, raws := setContent(t, m, "n.md", content)
	if len(raws) != 1 || !strings.HasPrefix(raws[0], "\x1bPtmux;") {
		t.Fatalf("transmit not wrapped for tmux")
	}
	if !strings.HasPrefix(m.KittyCleanup(), "\x1bPtmux;") {
		t.Fatal("delete not wrapped for tmux")
	}
}

// --- view geometry ---------------------------------------------------------

func TestViewAlwaysExactSize(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "pic.png"), 400, 300)
	content := "# Title that is rather long and will need wrapping somewhere\n\n" +
		"```\n" + strings.Repeat("x", 200) + "\n```\n\n" +
		"| a | b |\n|---|---|\n| " + strings.Repeat("c", 90) + " | d |\n\n" +
		"![](pic.png)\n\n![](missing-with-a-very-long-file-name-that-overflows-the-pane.png)\n\n- [ ] task"
	sizes := [][2]int{{60, 30}, {20, 5}, {3, 4}, {2, 2}, {1, 3}, {80, 100}}
	for _, proto := range []imgrender.Protocol{imgrender.ProtoKitty, imgrender.ProtoHalfBlocks, imgrender.ProtoOff} {
		m := newTest(t, proto, vault)
		checkDims(t, m.View(), 60, 30) // before any render
		for _, sz := range sizes {
			m = m.SetSize(sz[0], sz[1])
			checkDims(t, m.View(), sz[0], sz[1]) // stale render cropped
			m, _ = setContent(t, m, "n.md", content)
			checkDims(t, m.View(), sz[0], sz[1])
			m = m.SetOverlayOpen(true)
			checkDims(t, m.View(), sz[0], sz[1])
			m = m.SetOverlayOpen(false)
		}
	}
	m := newTest(t, imgrender.ProtoOff, vault).SetSize(10, 0)
	if m.View() != "" {
		t.Fatal("zero height view not empty")
	}
}
