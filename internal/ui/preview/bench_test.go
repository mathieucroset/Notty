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

func TestLongListPiecesRenderLikeTheWholeList(t *testing.T) {
	var b strings.Builder
	for i := range 120 {
		if i < 60 {
			fmt.Fprintf(&b, "- entry %d\n  - [ ] nested %d\n", i, i)
		} else {
			fmt.Fprintf(&b, "%d. entry %d\n     wrapped **line** %d\n", i+1, i, i)
		}
	}
	content := strings.TrimSuffix(b.String(), "\n")
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "n.md", content)
	if len(m.doc.segs) < 2 {
		t.Fatal("list not cut into pieces")
	}
	tr, err := glamour.NewTermRenderer(glamour.WithStyles(theme.GlamourStyle(m.palette)), glamour.WithWordWrap(58))
	if err != nil {
		t.Fatal(err)
	}
	whole := renderJob{sh: m.sh}.glamour(tr, content)
	if len(whole) != len(m.doc.lines) {
		t.Fatalf("pieces give %d rows, whole list %d", len(m.doc.lines), len(whole))
	}
	for i := range whole {
		if got, want := ansi.Strip(m.doc.lines[i].text), ansi.Strip(whole[i]); got != want {
			t.Fatalf("row %d = %q, want %q", i, got, want)
		}
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
