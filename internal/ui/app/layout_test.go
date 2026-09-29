package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/theme"
)

func TestSidebarStartsVisible(t *testing.T) {
	tests := []struct {
		width int
		want  bool
	}{{60, false}, {70, false}, {79, false}, {80, true}, {120, true}}
	for _, tt := range tests {
		if got := SidebarStartsVisible(tt.width); got != tt.want {
			t.Errorf("SidebarStartsVisible(%d) = %v, want %v", tt.width, got, tt.want)
		}
	}
}

func TestComputeLayout(t *testing.T) {
	tests := []struct {
		name          string
		w, h          int
		sidebar       bool
		wantSidebar   Rect
		wantMain      Rect
		wantContent   Rect
		wantStatus    Rect
		wantSidebarOn bool
	}{
		{
			name: "wide with sidebar", w: 120, h: 40, sidebar: true,
			wantSidebar: Rect{0, 0, 28, 39}, wantMain: Rect{28, 0, 92, 39},
			wantContent: Rect{29, 1, 90, 37}, wantStatus: Rect{0, 39, 120, 1}, wantSidebarOn: true,
		},
		{
			name: "hidden at 70", w: 70, h: 24, sidebar: false,
			wantSidebar: Rect{}, wantMain: Rect{0, 0, 70, 23},
			wantContent: Rect{1, 1, 68, 21}, wantStatus: Rect{0, 23, 70, 1},
		},
		{
			name: "shown at 70 after ctrl+b", w: 70, h: 24, sidebar: true,
			wantSidebar: Rect{0, 0, 28, 23}, wantMain: Rect{28, 0, 42, 23},
			wantContent: Rect{29, 1, 40, 21}, wantStatus: Rect{0, 23, 70, 1}, wantSidebarOn: true,
		},
		{
			name: "zen centers content at 80", w: 140, h: 30, sidebar: false,
			wantSidebar: Rect{}, wantMain: Rect{0, 0, 140, 29},
			wantContent: Rect{30, 1, 80, 27}, wantStatus: Rect{0, 29, 140, 1},
		},
		{
			name: "no zen at exactly 100", w: 100, h: 30, sidebar: false,
			wantSidebar: Rect{}, wantMain: Rect{0, 0, 100, 29},
			wantContent: Rect{1, 1, 98, 27}, wantStatus: Rect{0, 29, 100, 1},
		},
		{
			name: "zen at 101", w: 101, h: 30, sidebar: false,
			wantSidebar: Rect{}, wantMain: Rect{0, 0, 101, 29},
			wantContent: Rect{10, 1, 80, 27}, wantStatus: Rect{0, 29, 101, 1},
		},
		{
			name: "too narrow for the sidebar", w: 30, h: 10, sidebar: true,
			wantSidebar: Rect{}, wantMain: Rect{0, 0, 30, 9},
			wantContent: Rect{1, 1, 28, 7}, wantStatus: Rect{0, 9, 30, 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := ComputeLayout(tt.w, tt.h, tt.sidebar)
			if l.SidebarVisible != tt.wantSidebarOn {
				t.Errorf("SidebarVisible = %v, want %v", l.SidebarVisible, tt.wantSidebarOn)
			}
			if l.Sidebar != tt.wantSidebar {
				t.Errorf("Sidebar = %+v, want %+v", l.Sidebar, tt.wantSidebar)
			}
			if l.Main != tt.wantMain {
				t.Errorf("Main = %+v, want %+v", l.Main, tt.wantMain)
			}
			if l.Content != tt.wantContent {
				t.Errorf("Content = %+v, want %+v", l.Content, tt.wantContent)
			}
			if l.Status != tt.wantStatus {
				t.Errorf("Status = %+v, want %+v", l.Status, tt.wantStatus)
			}
		})
	}
}

func TestComputeLayoutTiny(t *testing.T) {
	// Degenerate sizes must not panic or produce negative sizes.
	for _, sz := range [][2]int{{0, 0}, {1, 1}, {2, 2}, {5, 3}} {
		l := ComputeLayout(sz[0], sz[1], true)
		for _, r := range []Rect{l.Sidebar, l.Main, l.Content, l.Status} {
			if r.W < 0 || r.H < 0 {
				t.Errorf("ComputeLayout(%d,%d) has negative rect %+v", sz[0], sz[1], r)
			}
		}
	}
}

func testStyles(t *testing.T) theme.Styles {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	return theme.NewStyles(p)
}

func TestRenderPane(t *testing.T) {
	st := testStyles(t)
	tests := []struct {
		name, title, right string
		w, h               int
		wantTop            string
	}{
		{"title", "◆ Notty", "", 20, 4, "╭─ ◆ Notty ────────╮"},
		{"title and marker", "Work / Standup", "●", 24, 3, "╭─ Work / Standup ─ ● ─╮"},
		{"title truncated", "A very long pane title indeed", "●", 20, 3, "╭─ A very lo… ─ ● ─╮"},
		{"no title", "", "", 8, 3, "╭──────╮"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderPane(st, tt.title, tt.right, "hello\nworld", tt.w, tt.h, false)
			lines := strings.Split(ansi.Strip(out), "\n")
			if len(lines) != tt.h {
				t.Fatalf("got %d lines, want %d", len(lines), tt.h)
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != tt.w {
					t.Errorf("line %d width %d, want %d: %q", i, w, tt.w, l)
				}
			}
			if lines[0] != tt.wantTop {
				t.Errorf("top = %q, want %q", lines[0], tt.wantTop)
			}
			if !strings.HasPrefix(lines[len(lines)-1], "╰") || !strings.HasSuffix(lines[len(lines)-1], "╯") {
				t.Errorf("bottom = %q", lines[len(lines)-1])
			}
			if !strings.HasPrefix(lines[1], "│hello") {
				t.Errorf("body = %q", lines[1])
			}
		})
	}
	if renderPane(st, "x", "", "", 10, 3, true) == renderPane(st, "x", "", "", 10, 3, false) {
		t.Error("focused and unfocused panes render identically")
	}
}
