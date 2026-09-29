package editor

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/mdstyle"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRenderSnapshots(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "attachments", "cat.png"), 64, 32)
	doc := strings.Join([]string{
		"# Title",                   // 0
		"",                          // 1
		"- [ ] open task",           // 2
		"- [x] done task",           // 3
		"1. [ ] numbered",           // 4
		"> quoted text",             // 5
		"---",                       // 6
		"![](/attachments/cat.png)", // 7
		"![](nope.png)",             // 8
		"plain **bold**",            // 9
		"> - [ ] quoted task",       // 10
		"> - [x] quoted done",       // 11
	}, "\n")
	tests := []struct {
		name string
		cur  int
		row  int
		want string
	}{
		{"heading progress", 1, 0, "# Title ▰▰▱▱▱ 1/3"},
		{"heading raw on cursor line", 0, 0, "# Title"},
		{"open task glyph", 1, 2, "☐ open task"},
		{"done task glyph", 1, 3, "☑ done task"},
		{"numbered task keeps number", 1, 4, "1. ☐ numbered"},
		{"task raw on cursor line", 2, 2, "- [ ] open task"},
		{"done raw on cursor line", 3, 3, "- [x] done task"},
		{"quote bar", 1, 5, "▎ quoted text"},
		{"quote raw", 5, 5, "> quoted text"},
		{"rule", 1, 6, strings.Repeat("─", 38)},
		{"rule raw", 6, 6, "---"},
		{"image chip", 1, 7, "🖼️ cat.png  64×32"},
		{"missing chip", 1, 8, "🖼️ nope.png  missing"},
		{"chip raw on cursor line", 7, 7, "![](/attachments/cat.png)"},
		{"markup kept", 1, 9, "plain **bold**"},
		{"task in quote", 1, 10, "▎ ☐ quoted task"},
		{"done task in quote", 1, 11, "▎ ☑ quoted done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			opts.VaultRoot = vault
			m := newModel(t, opts, doc, buffer.Pos{Line: tt.cur}, 40, 14)
			rows := plainView(m)
			// Gutter is one space of padding.
			if got := strings.TrimSpace(rows[tt.row]); got != tt.want {
				t.Errorf("row %d = %q, want %q", tt.row, got, tt.want)
			}
			checkSize(t, m, 40, 14)
		})
	}
}

func TestRenderSoftWrap(t *testing.T) {
	m := newModel(t, testOptions(t), "the quick brown fox jumps\nnext", buffer.Pos{Line: 1}, 12, 5)
	// Text width 11, wrap width 10.
	want := []string{" the quick", " brown fox", " jumps", " next", ""}
	if got := plainView(m); !slices.Equal(got, want) {
		t.Errorf("view = %q, want %q", got, want)
	}
}

func TestLineNumbers(t *testing.T) {
	opts := testOptions(t)
	opts.LineNumbers = true
	m := newModel(t, opts, "a\nb\nc\nd", buffer.Pos{Line: 1}, 20, 4)
	want := []string{" 1 a", " 2 b", " 1 c", " 2 d"}
	if got := plainView(m); !slices.Equal(got, want) {
		t.Errorf("view = %q, want %q", got, want)
	}
	checkSize(t, m, 20, 4)
}

func TestViewExactSize(t *testing.T) {
	docs := []string{
		"",
		"short",
		strings.Repeat("long line with words ", 20),
		"日本語のテキスト日本語のテキスト日本語のテキスト",
		"\ttabs\tand\tmore\ttabs\t\t\t\t\t\t\t\t\t\tx",
		"😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀😀",
		"# Heading\n- [ ] task\n- [x] done\n> quote\n---\n```go\nfunc main() {}\n```",
	}
	for _, doc := range docs {
		for _, ln := range []bool{false, true} {
			for _, size := range [][2]int{{1, 1}, {5, 3}, {13, 7}, {40, 10}} {
				opts := testOptions(t)
				opts.LineNumbers = ln
				m := newModel(t, opts, doc, buffer.Pos{}, size[0], size[1])
				checkSize(t, m, size[0], size[1])
				m = m.SetReadOnly(true, "")
				checkSize(t, m, size[0], size[1])
			}
		}
	}
}

func TestCursorPositionAfterWrap(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		cur   buffer.Pos
		w, h  int
		x, y  int
		shape tea.CursorShape
	}{
		{"start", "hello", buffer.Pos{}, 20, 5, 1, 0, tea.CursorBlock},
		{"wrapped row", "the quick brown fox", buffer.Pos{Col: 14}, 12, 5, 5, 1, tea.CursorBlock},
		{"second line after wrap", "the quick brown fox\nab", buffer.Pos{Line: 1, Col: 1}, 12, 5, 2, 2, tea.CursorBlock},
		{"cjk", "日本語", buffer.Pos{Col: 2}, 20, 5, 5, 0, tea.CursorBlock},
		{"tab", "\tx", buffer.Pos{Col: 1}, 20, 5, 5, 0, tea.CursorBlock},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, testOptions(t), tt.doc, tt.cur, tt.w, tt.h)
			c := m.CursorPosition()
			if c == nil {
				t.Fatal("nil cursor")
			}
			if c.X != tt.x || c.Y != tt.y || c.Shape != tt.shape || !c.Blink {
				t.Errorf("cursor = (%d,%d) shape %v blink %v, want (%d,%d) shape %v", c.X, c.Y, c.Shape, c.Blink, tt.x, tt.y, tt.shape)
			}
		})
	}
}

func TestCursorHiddenWhenUnfocused(t *testing.T) {
	m := newModel(t, testOptions(t), "x", buffer.Pos{}, 10, 3).SetFocused(false)
	if m.CursorPosition() != nil {
		t.Error("unfocused editor shows a cursor")
	}
}

func TestScrollFollowsCursor(t *testing.T) {
	var lines []string
	for range 50 {
		lines = append(lines, "line")
	}
	m := newModel(t, testOptions(t), strings.Join(lines, "\n"), buffer.Pos{Line: 30}, 20, 10)
	c := m.CursorPosition()
	if c == nil || c.Y != 6 {
		t.Fatalf("cursor after load at row %v, want 6 (bottom margin)", c)
	}
}

func TestGutterGrowsWithLineCount(t *testing.T) {
	opts := testOptions(t)
	opts.LineNumbers = true
	doc := strings.TrimSuffix(strings.Repeat("x\n", 99), "\n")
	m := newModel(t, opts, doc, buffer.Pos{Line: 98}, 12, 5)
	m, _ = typeKeys(m, "o", "a", "b", "c", "d", "e", "f", "g", "h", "esc") // line 100
	rows := plainView(m)
	// Gutter 4, text 8, wrap 7.
	if got := rows[len(rows)-2:]; got[0] != "100 abcdefg" || got[1] != "    h" {
		t.Errorf("last rows = %q, want the 3-digit gutter and a wrap at 7", got)
	}
	checkSize(t, m, 12, 5)
}

func TestControlCharsArePlaceholders(t *testing.T) {
	doc := "bad \x1b]52;c;aGk=\x07 and \x1b[2J x\nnext\x08\x08\r\u009b\x7f"
	for _, ro := range []bool{false, true} {
		m := newModel(t, testOptions(t), doc, buffer.Pos{Line: 1, Col: 5}, 30, 4)
		if ro {
			m = m.SetReadOnly(true, "")
		}
		v := m.View()
		for _, bad := range []string{"\x1b]", "\x1b[2J", "\x07", "\x08", "\r", "\u009b", "\x7f"} {
			if strings.Contains(v, bad) {
				t.Errorf("view contains %q", bad)
			}
		}
		checkSize(t, m, 30, 4)
		rows := plainView(m)
		if !slices.Contains(rows, " next␈␈␍\ufffd␡") {
			t.Errorf("placeholders missing: %q", rows)
		}
		if !ro {
			// The cursor after "next" and one ␈ sits on the second ␈.
			c := m.CursorPosition()
			if c == nil || c.X != 6 {
				t.Errorf("cursor = %+v, want x=6", c)
			}
		}
	}
}

func TestLongLineLaidOutOncePerKey(t *testing.T) {
	long := strings.Repeat("word ", 20000) // 100KB on one line
	m := newModel(t, testOptions(t), long, buffer.Pos{Col: 50000}, 100, 40)
	m, _ = typeKeys(m, "i")
	for range 3 {
		before := m.aux.builds
		m, _ = typeKeys(m, "x")
		_ = m.View()
		_ = m.CursorPosition()
		if n := m.aux.builds - before; n != 1 {
			t.Fatalf("the long line was laid out %d times for one key", n)
		}
	}
}

func TestRuleSelection(t *testing.T) {
	// A selection starting in the middle of a rule still covers the drawn
	// line, whose units stand for the whole source line.
	m := newModel(t, testOptions(t), "abc\n---\nxyz", buffer.Pos{Line: 1, Col: 2}, 20, 3)
	m, _ = typeKeys(m, "v", "j")
	selected := m.sty.style(sty{role: roleRule, sel: true}).Render(strings.Repeat("─", 18))
	if !strings.Contains(m.View(), selected) {
		t.Errorf("rule inside the selection is not highlighted:\n%q", m.View())
	}
}

func TestDoneTaskInQuoteStyled(t *testing.T) {
	m := newModel(t, testOptions(t), "x\n> - [x] done", buffer.Pos{}, 30, 3)
	done := m.sty.style(sty{kind: mdstyle.TaskDoneText}).Render("done")
	if !strings.Contains(m.View(), done) {
		t.Errorf("done task text in a quote is not struck through:\n%q", m.View())
	}
}
