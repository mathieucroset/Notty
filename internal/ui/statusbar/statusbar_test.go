package statusbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func styles(t *testing.T) theme.Styles {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("catppuccin-mocha palette missing")
	}
	return theme.NewStyles(p)
}

func TestSyncText(t *testing.T) {
	tests := []struct {
		st   msgs.SyncStatusMsg
		want string
	}{
		{msgs.SyncStatusMsg{State: msgs.SyncSynced}, "✓ synced"},
		{msgs.SyncStatusMsg{State: msgs.SyncSyncing}, "↻ syncing…"},
		{msgs.SyncStatusMsg{State: msgs.SyncOffline, Pending: 3}, "⊘ offline (3 pending)"},
		{msgs.SyncStatusMsg{State: msgs.SyncConflict, Conflicts: 2}, "⚠ 2 conflicts"},
		{msgs.SyncStatusMsg{State: msgs.SyncConflict, Conflicts: 1}, "⚠ 1 conflict"},
		{msgs.SyncStatusMsg{State: msgs.SyncError}, "✗ sync error"},
		{msgs.SyncStatusMsg{State: msgs.SyncLocalOnly}, "○ local only"},
		{msgs.SyncStatusMsg{}, ""},
	}
	for _, tt := range tests {
		if got := SyncText(tt.st); got != tt.want {
			t.Errorf("SyncText(%+v) = %q, want %q", tt.st, got, tt.want)
		}
	}
}

func TestViewPerSyncState(t *testing.T) {
	for _, state := range []msgs.SyncState{msgs.SyncSynced, msgs.SyncSyncing, msgs.SyncOffline, msgs.SyncConflict, msgs.SyncError, msgs.SyncLocalOnly} {
		t.Run(string(state), func(t *testing.T) {
			m := New(styles(t))
			m.SetSize(80)
			m.Mode = "NORMAL"
			m.Path = "Work/Standup notes.md"
			m.Words = 142
			m.Sync = msgs.SyncStatusMsg{State: state, Pending: 1, Conflicts: 2}
			got := ansi.Strip(m.View())
			if w := ansi.StringWidth(got); w != 80 {
				t.Errorf("width = %d, want 80: %q", w, got)
			}
			for _, want := range []string{"NORMAL", "Work/Standup notes.md", "· 142 words", SyncText(m.Sync), "F1 help"} {
				if !strings.Contains(got, want) {
					t.Errorf("view %q missing %q", got, want)
				}
			}
			if !strings.HasSuffix(strings.TrimRight(got, " "), "F1 help") {
				t.Errorf("F1 help is not right-aligned: %q", got)
			}
		})
	}
}

func TestViewModes(t *testing.T) {
	for _, mode := range []string{"NORMAL", "INSERT", "VISUAL", "V-LINE", "COMMAND", "PLAIN", "READ-ONLY", "WHATEVER"} {
		m := New(styles(t))
		m.SetSize(60)
		m.Mode = mode
		got := ansi.Strip(m.View())
		if !strings.HasPrefix(got, " "+mode+" ") {
			t.Errorf("mode %s: view %q does not start with the pill", mode, got)
		}
	}
}

func TestViewSingularWord(t *testing.T) {
	m := New(styles(t))
	m.SetSize(80)
	m.Mode = "NORMAL"
	m.Path = "a.md"
	m.Words = 1
	if got := ansi.Strip(m.View()); !strings.Contains(got, "· 1 word ") {
		t.Errorf("view %q missing singular word count", got)
	}
}

func TestViewNoPath(t *testing.T) {
	m := New(styles(t))
	m.SetSize(50)
	m.Mode = "NORMAL"
	got := ansi.Strip(m.View())
	if strings.Contains(got, "words") {
		t.Errorf("word count shown without a note: %q", got)
	}
}

func TestViewKeepsUrgentSyncText(t *testing.T) {
	long := "Projects/Clients/Acme Corporation/Quarterly planning/Meeting notes 2026.md"
	tests := []struct {
		st    msgs.SyncStatusMsg
		width int
	}{
		{msgs.SyncStatusMsg{State: msgs.SyncConflict, Conflicts: 2}, 30},
		{msgs.SyncStatusMsg{State: msgs.SyncConflict, Conflicts: 2}, 24},
		{msgs.SyncStatusMsg{State: msgs.SyncError}, 30},
		{msgs.SyncStatusMsg{State: msgs.SyncError}, 22},
	}
	for _, tt := range tests {
		m := New(styles(t))
		m.SetSize(tt.width)
		m.Mode = "NORMAL"
		m.Path = long
		m.Words = 7
		m.Sync = tt.st
		got := ansi.Strip(m.View())
		if w := ansi.StringWidth(got); w != tt.width {
			t.Errorf("width %d: rendered width %d: %q", tt.width, w, got)
		}
		for _, want := range []string{"NORMAL", SyncText(tt.st)} {
			if !strings.Contains(got, want) {
				t.Errorf("%s at width %d: %q missing %q", tt.st.State, tt.width, got, want)
			}
		}
	}
}

func TestViewTruncation(t *testing.T) {
	long := "Projects/Clients/Acme Corporation/Quarterly planning/Meeting notes 2026.md"
	tests := []struct {
		width       int
		mustHave    []string
		mustNotHave []string
	}{
		{120, []string{"NORMAL", long, "· 7 words", "✓ synced", "F1 help"}, nil},
		// Words drop before the path is shortened.
		{90, []string{"NORMAL", "…", "Meeting notes 2026.md", "✓ synced", "F1 help"}, []string{"words"}},
		{50, []string{"NORMAL", "…", "2026.md", "✓ synced", "F1 help"}, []string{"words"}},
		// Very narrow: help and then sync text are dropped, the pill stays.
		{24, []string{"NORMAL"}, []string{"F1 help"}},
		{8, []string{"NORMAL"}, nil},
		{3, nil, nil},
	}
	for _, tt := range tests {
		m := New(styles(t))
		m.SetSize(tt.width)
		m.Mode = "NORMAL"
		m.Path = long
		m.Words = 7
		m.Sync = msgs.SyncStatusMsg{State: msgs.SyncSynced}
		got := ansi.Strip(m.View())
		if w := ansi.StringWidth(got); w != tt.width {
			t.Errorf("width %d: rendered width %d: %q", tt.width, w, got)
		}
		for _, s := range tt.mustHave {
			if !strings.Contains(got, s) {
				t.Errorf("width %d: %q missing %q", tt.width, got, s)
			}
		}
		for _, s := range tt.mustNotHave {
			if strings.Contains(got, s) {
				t.Errorf("width %d: %q should not contain %q", tt.width, got, s)
			}
		}
	}
}

func TestViewBusy(t *testing.T) {
	m := New(styles(t))
	m.SetSize(80)
	m.Busy = "indexing…"
	if got := ansi.Strip(m.View()); !strings.Contains(got, "indexing…   F1 help") {
		t.Errorf("busy label missing: %q", got)
	}
	m.Sync = msgs.SyncStatusMsg{State: msgs.SyncSynced}
	if got := ansi.Strip(m.View()); !strings.Contains(got, "indexing…   ✓ synced") {
		t.Errorf("busy label not before the sync state: %q", got)
	}
}
