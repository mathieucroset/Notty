package preview

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func bigTaskList(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("- [ ] item " + strconv.Itoa(i) + "\n")
	}
	return b.String()
}

func TestBigTaskListMapsEveryTask(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir()).SetSize(100, 40)
	m, _ = setContent(t, m, "n.md", bigTaskList(3000))
	if len(m.doc.taskRows) != 3000 {
		t.Fatalf("%d tasks, want 3000", len(m.doc.taskRows))
	}
	for i, r := range m.doc.taskRows {
		if got := ansi.Strip(m.doc.lines[r].text); !strings.Contains(got, "item "+strconv.Itoa(i)+" ") {
			t.Fatalf("task %d mapped to row %d = %q", i, r, got)
		}
	}
}

// repeat builds n lines from f.
func repeat(n int, f func(i int) string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(f(i))
	}
	return b.String()
}

func TestPiecesRenderLikeTheWholeBlock(t *testing.T) {
	cases := []struct {
		name    string
		content string
		cut     bool // expect the block to be cut into pieces
	}{
		{"nested mixed lists", repeat(120, func(i int) string {
			if i < 60 {
				return fmt.Sprintf("- entry %d\n  - [ ] nested %d\n", i, i)
			}
			return fmt.Sprintf("%d. entry %d\n     wrapped **line** %d\n", i+1, i, i)
		}), true},
		{"ordered all ones", repeat(40, func(int) string { return "1. lazy\n" }), false},
		{"ordered in sequence", repeat(40, func(i int) string { return fmt.Sprintf("%d. seq\n", i+1) }), true},
		{"ordered from 5", repeat(40, func(i int) string { return fmt.Sprintf("%d. seq\n", i+5) }), true},
		{"loose early", repeat(40, func(i int) string {
			if i == 2 {
				return "- item 2\n\n  para inside\n"
			}
			return fmt.Sprintf("- item %d\n", i)
		}), false},
		{"loose late", repeat(40, func(i int) string {
			if i == 36 {
				return "- item 36\n\n  para inside\n"
			}
			return fmt.Sprintf("- item %d\n", i)
		}), false},
		{"paragraph then list", "Para intro\n" + repeat(40, func(i int) string { return fmt.Sprintf("- item %d\n", i) }), false},
		{"paragraph then ordered", "Para start\n" + repeat(34, func(i int) string { return fmt.Sprintf("  cont %d\n", i) }) + "33. not an item\n", false},
		{"lazy continuations", repeat(40, func(i int) string { return fmt.Sprintf("- item %d\nlazy continuation %d\n", i, i) }), false},
		{"quote then list", "> quote\n" + repeat(40, func(i int) string { return fmt.Sprintf("- item %d\n", i) }), false},
		{"nested tasks", repeat(40, func(i int) string { return fmt.Sprintf("- [x] item %d\n    - sub %d\n", i, i) }), true},
		{"star bullets", repeat(40, func(i int) string { return fmt.Sprintf("* item %d\n", i) }), true},
		{"bullet change", repeat(40, func(i int) string {
			if i < 33 {
				return fmt.Sprintf("- item %d\n", i)
			}
			return fmt.Sprintf("* item %d\n", i)
		}), true}, // cut inside the second list, not at the change
		{"trailing rule", repeat(40, func(i int) string { return fmt.Sprintf("- item %d\n", i) }) + "---\n", true},
		{"trailing lazy line", repeat(40, func(i int) string { return fmt.Sprintf("- item %d\n", i) }) + "Setext\n", true},
		{"html block", "<div>\n" + repeat(40, func(i int) string { return fmt.Sprintf("- item %d\n", i) }) + "</div>\n", false},
		{"table", "| a | b |\n|---|---|\n" + repeat(40, func(i int) string { return fmt.Sprintf("| x%d | y |\n", i) }), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			content := strings.TrimSuffix(c.content, "\n")
			m := newTest(t, imgrender.ProtoOff, t.TempDir())
			m, _ = setContent(t, m, "n.md", content)
			if got := len(m.doc.segs) > 1; got != c.cut {
				t.Fatalf("cut = %v (%d segments), want %v", got, len(m.doc.segs), c.cut)
			}
			tr, err := glamour.NewTermRenderer(glamour.WithStyles(theme.GlamourStyle(m.palette)), glamour.WithWordWrap(contentWidth(m.width)))
			if err != nil {
				t.Fatal(err)
			}
			whole := renderJob{sh: m.sh}.glamour(tr, content)
			for i := range max(len(whole), len(m.doc.lines)) {
				var want, got string
				if i < len(whole) {
					want = ansi.Strip(whole[i])
				}
				if i < len(m.doc.lines) {
					got = ansi.Strip(m.doc.lines[i].text)
				}
				if got != want {
					t.Fatalf("row %d = %q, want %q (rows: pieces %d, whole %d)", i, got, want, len(m.doc.lines), len(whole))
				}
			}
		})
	}
}

func TestOrderedListOfOnesKeepsNumbering(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "n.md", repeat(40, func(int) string { return "1. x\n" }))
	if got := ansi.Strip(m.doc.lines[32].text); !strings.HasPrefix(got, "33.") {
		t.Fatalf("row 32 = %q, want it numbered 33.", got)
	}
}

func TestMapTasksIsFast(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing test")
	}
	d := renderedDoc(t, bigTaskList(3000))
	start := time.Now()
	d.mapTasks()
	if el := time.Since(start); el > 50*time.Millisecond {
		t.Fatalf("mapping 3000 tasks took %v", el)
	}
}

func renderedDoc(tb testing.TB, content string) *doc {
	tb.Helper()
	p, _ := theme.Get("catppuccin-mocha")
	m := New(theme.NewStyles(p), p, imgrender.Caps{}, tb.TempDir()).SetSize(100, 40)
	m, _ = m.SetContent("n.md", content)
	return m.startRender()().(renderedMsg).doc
}

// BenchmarkRenderBigTaskList renders a 3000-task list from scratch
// (Glamour, layout and task mapping); the target is under 200ms.
func BenchmarkRenderBigTaskList(b *testing.B) {
	content := bigTaskList(3000)
	for b.Loop() {
		_ = renderedDoc(b, content)
	}
}

// BenchmarkMapTasks measures the task mapping alone.
func BenchmarkMapTasks(b *testing.B) {
	d := renderedDoc(b, bigTaskList(3000))
	for b.Loop() {
		d.mapTasks()
	}
}
