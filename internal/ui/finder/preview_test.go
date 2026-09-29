package finder

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/search"
)

// --- mapLineToRendered -------------------------------------------------

func TestMapLineToRendered(t *testing.T) {
	tests := []struct {
		name                          string
		rawLine, totalRawLines, rendL int
		want                          int
	}{
		{"no rendered lines", 0, 10, 0, -1},
		{"single raw line", 0, 1, 5, 0},
		{"start maps to start", 0, 10, 20, 0},
		{"end maps to end", 9, 10, 20, 19},
		{"midpoint scales proportionally", 5, 11, 21, 10},
		{"negative raw line clamps to 0", -3, 10, 20, 0},
		{"raw line past the end clamps to the last rendered line", 100, 10, 20, 19},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapLineToRendered(tt.rawLine, tt.totalRawLines, tt.rendL); got != tt.want {
				t.Errorf("mapLineToRendered(%d, %d, %d) = %d, want %d", tt.rawLine, tt.totalRawLines, tt.rendL, got, tt.want)
			}
		})
	}
}

// --- findHitLine ---------------------------------------------------------

func TestFindHitLinePrefersOccurrenceNearestGuess(t *testing.T) {
	lines := []string{
		"intro",
		"TARGET here", // index 1
		"unrelated",
		"unrelated",
		"TARGET here", // index 4, closer to guess 5
		"outro",
	}
	got := findHitLine(lines, "TARGET here", 5)
	if got != 4 {
		t.Errorf("findHitLine(guess=5) = %d, want 4 (nearest occurrence)", got)
	}
	got = findHitLine(lines, "TARGET here", 0)
	if got != 1 {
		t.Errorf("findHitLine(guess=0) = %d, want 1 (nearest occurrence)", got)
	}
}

func TestFindHitLineFallsBackToGuessWhenTextNotFound(t *testing.T) {
	lines := []string{"one", "two", "three"}
	got := findHitLine(lines, "not present anywhere", 2)
	if got != 2 {
		t.Errorf("findHitLine with no match = %d, want the guess (2)", got)
	}
}

func TestFindHitLineIgnoresANSICodesAndWhitespace(t *testing.T) {
	styled := "\x1b[1m  Shipped the auth flow  \x1b[0m"
	lines := []string{"unrelated", styled, "unrelated"}
	got := findHitLine(lines, "  Shipped the auth flow  ", 0)
	if got != 1 {
		t.Errorf("findHitLine did not match through ANSI styling: got %d, want 1", got)
	}
}

func TestFindHitLineEmptyTextReturnsGuess(t *testing.T) {
	lines := []string{"one", "two"}
	if got := findHitLine(lines, "   ", 1); got != 1 {
		t.Errorf("findHitLine with blank hit text = %d, want the guess", got)
	}
}

// --- LRU cache -------------------------------------------------------------

func TestLRUCacheGetAndPut(t *testing.T) {
	c := newLRUCache(2)
	if _, ok := c.Get(previewKey{path: "a"}); ok {
		t.Fatal("Get on an empty cache reported a hit")
	}
	c.Put(previewKey{path: "a"}, []string{"A"})
	if lines, ok := c.Get(previewKey{path: "a"}); !ok || lines[0] != "A" {
		t.Fatalf("Get(a) = %v, %v, want [A], true", lines, ok)
	}
}

func TestLRUCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newLRUCache(2)
	c.Put(previewKey{path: "a"}, []string{"A"})
	c.Put(previewKey{path: "b"}, []string{"B"})
	// Touch "a" so "b" becomes the least recently used.
	c.Get(previewKey{path: "a"})
	c.Put(previewKey{path: "c"}, []string{"C"})

	if _, ok := c.Get(previewKey{path: "b"}); ok {
		t.Error("least-recently-used entry \"b\" was not evicted")
	}
	if _, ok := c.Get(previewKey{path: "a"}); !ok {
		t.Error("recently used entry \"a\" was evicted")
	}
	if _, ok := c.Get(previewKey{path: "c"}); !ok {
		t.Error("just-inserted entry \"c\" is missing")
	}
	if c.Len() != 2 {
		t.Errorf("Len() = %d, want 2", c.Len())
	}
}

func TestLRUCacheCapacityIs32(t *testing.T) {
	c := newLRUCache(previewCacheCapacity)
	for i := 0; i < previewCacheCapacity+10; i++ {
		c.Put(previewKey{width: i}, []string{"x"})
	}
	if c.Len() != previewCacheCapacity {
		t.Errorf("Len() = %d, want the configured capacity %d", c.Len(), previewCacheCapacity)
	}
	// The oldest entries should have been evicted.
	if _, ok := c.Get(previewKey{width: 0}); ok {
		t.Error("oldest entry should have been evicted past capacity")
	}
	// The most recent entries should still be present.
	if _, ok := c.Get(previewKey{width: previewCacheCapacity + 9}); !ok {
		t.Error("most recently inserted entry should still be cached")
	}
}

func TestLRUCachePeekDoesNotAffectRecency(t *testing.T) {
	c := newLRUCache(2)
	c.Put(previewKey{path: "a"}, []string{"A"})
	c.Put(previewKey{path: "b"}, []string{"B"})
	// Peek "a" repeatedly: since Peek must not promote it, "a" stays the
	// least-recently-used and is evicted once "c" is inserted.
	c.Peek(previewKey{path: "a"})
	c.Peek(previewKey{path: "a"})
	c.Put(previewKey{path: "c"}, []string{"C"})

	if _, ok := c.Peek(previewKey{path: "a"}); ok {
		t.Error("Peek must not protect an entry from eviction the way Get does")
	}
}

// --- scroll clamping ---------------------------------------------------

func fullTextModelWithNote(t *testing.T, content string) (Model, *index.Note) {
	t.Helper()
	n := note("note.md", content)
	m := New(FullText, []*index.Note{n}, nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 30)
	return m, n
}

// primeCache directly seeds the model's preview cache and current key, as if
// an async render for key had already completed, without going through
// Glamour.
func primeCache(m Model, key previewKey, lines []string) Model {
	m.previewCurrentKey = key
	m.previewCache.Put(key, lines)
	return m
}

func TestPreviewScrollClampsToTop(t *testing.T) {
	m, _ := fullTextModelWithNote(t, "irrelevant")
	m.hits = []search.Hit{{Path: "note.md", Line: 0, Text: "line0"}}
	m.cursor = 0

	full := make([]string, 50)
	for i := range full {
		full[i] = "line"
	}
	key := previewKey{path: "note.md", width: m.previewWidth(), windowStart: 0, windowLen: 50}
	m = primeCache(m, key, full)

	height := m.bodyHeight()
	out := m.previewPaneLines(m.previewWidth(), height)
	if len(out) != height {
		t.Fatalf("previewPaneLines returned %d lines, want %d", len(out), height)
	}
	// A hit near the very start must not scroll past the top: the first
	// rendered line must still be full[0]-derived content, not blank.
	if strings.TrimSpace(ansi.Strip(out[0])) == "" {
		t.Errorf("scrolled past the top for an early hit: first line is blank")
	}
}

func TestPreviewScrollClampsToBottom(t *testing.T) {
	m, _ := fullTextModelWithNote(t, "irrelevant")
	m.hits = []search.Hit{{Path: "note.md", Line: 49, Text: "line49"}}
	m.cursor = 0

	full := make([]string, 10)
	for i := range full {
		full[i] = "line" + string(rune('0'+i))
	}
	key := previewKey{path: "note.md", width: m.previewWidth(), windowStart: 0, windowLen: 10}
	m = primeCache(m, key, full)

	height := m.bodyHeight() // typically larger than len(full)
	out := m.previewPaneLines(m.previewWidth(), height)
	if len(out) != height {
		t.Fatalf("previewPaneLines returned %d lines, want %d", len(out), height)
	}
	// The window must never start past what keeps the last content line
	// visible: with only 10 rendered lines and a much taller pane, start
	// must clamp to 0.
	if !strings.Contains(ansi.Strip(out[0]), "line0") {
		t.Errorf("did not clamp scroll to the top when content is shorter than the pane: %q", ansi.Strip(out[0]))
	}
}

// --- async render flow (seq-guarded) ------------------------------------

func TestPreviewAsyncRenderFlow(t *testing.T) {
	m := New(Fuzzy, testNotes(), []string{"readme.md"}, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	// Nothing rendered yet: the preview pane must show the loading
	// placeholder, never call Glamour synchronously.
	before := strings.Join(m.previewPaneLines(m.previewWidth(), m.bodyHeight()), "\n")
	if !strings.Contains(ansi.Strip(before), "loading") {
		t.Fatalf("expected a loading placeholder before any render completed, got:\n%s", ansi.Strip(before))
	}

	m, cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned no command for a valid initial selection")
	}
	msg := cmd()
	rendered, ok := msg.(previewRenderedMsg)
	if !ok {
		t.Fatalf("Init's command produced %#v, want previewRenderedMsg", msg)
	}
	if rendered.key.path != "readme.md" {
		t.Errorf("rendered.key.path = %q, want readme.md", rendered.key.path)
	}

	m, _ = m.Update(rendered)
	after := strings.Join(m.previewPaneLines(m.previewWidth(), m.bodyHeight()), "\n")
	if strings.Contains(ansi.Strip(after), "loading") {
		t.Errorf("still showing the loading placeholder after the render landed:\n%s", ansi.Strip(after))
	}
}

func TestPreviewStaleRenderDiscarded(t *testing.T) {
	m := New(Fuzzy, testNotes(), []string{"readme.md", "Work/ideas.md"}, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	// Request #1: selection is "readme.md".
	m, cmd1 := m.checkPreview()
	if cmd1 == nil {
		t.Fatal("expected a render command for the initial selection")
	}

	// The selection moves on before request #1's render lands, which bumps
	// the seq and supersedes it with request #2.
	m, _ = m.Update(key("down"))
	if m.previewSeq < 2 {
		t.Fatalf("previewSeq = %d, want at least 2 after a second request", m.previewSeq)
	}

	// Request #1's (now-stale) result arrives.
	msg1 := cmd1().(previewRenderedMsg)
	before := m.previewCache.Len()
	m, _ = m.Update(msg1)
	if m.previewCache.Len() != before {
		t.Error("a stale preview render was still cached")
	}
	if _, ok := m.previewCache.Peek(msg1.key); ok {
		t.Error("the stale render's key must not appear in the cache")
	}
}

func TestPreviewNoSelectionShowsBlank(t *testing.T) {
	m := New(Fuzzy, nil, nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)
	out := m.previewPaneLines(m.previewWidth(), m.bodyHeight())
	for i, l := range out {
		if strings.TrimSpace(ansi.Strip(l)) != "" {
			t.Errorf("line %d not blank with no selection: %q", i, ansi.Strip(l))
		}
	}
}

func TestCheckPreviewNoOpWhenSelectionUnchanged(t *testing.T) {
	m := New(Fuzzy, testNotes(), []string{"readme.md"}, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	m, cmd := m.checkPreview()
	if cmd == nil {
		t.Fatal("expected a render command for the initial selection")
	}
	seqAfterFirst := m.previewSeq

	m, cmd = m.checkPreview()
	if cmd != nil {
		t.Error("checkPreview issued a second command for an unchanged selection")
	}
	if m.previewSeq != seqAfterFirst {
		t.Errorf("previewSeq changed (%d -> %d) although the selection did not", seqAfterFirst, m.previewSeq)
	}
}
