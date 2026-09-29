package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/vault"
)

// newVault creates a temp vault with a folder and two notes.
func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"Work/Standup notes.md": "# Standup notes\n\n- shipped auth flow\n- follow up with design\n",
		"ideas.md":              "# Ideas\n\nA note app in the terminal.\n",
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := vault.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func testOptions(t *testing.T) Options {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	v := newVault(t)
	localPath := filepath.Join(t.TempDir(), "state.json")
	local, err := localstate.Load(localPath)
	if err != nil {
		t.Fatal(err)
	}
	return Options{
		Config:     config.Default(),
		Styles:     theme.NewStyles(p),
		Palette:    p,
		Vault:      v,
		Local:      local,
		LocalPath:  localPath,
		Pins:       &meta.State{Pins: []string{}},
		ConfigPath: filepath.Join(t.TempDir(), "config.toml"),
	}
}

// run feeds msg to m and executes every resulting command, feeding their
// messages back in, until nothing is left. It returns every message the
// commands produced (including tea.QuitMsg, which stops processing).
func run(t *testing.T, m *Model, msg tea.Msg) []tea.Msg {
	t.Helper()
	var produced []tea.Msg
	queue := []tea.Msg{msg}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 100 {
			t.Fatal("too many message steps")
		}
		cur := queue[0]
		queue = queue[1:]
		_, cmd := m.Update(cur)
		for _, res := range execCmd(cmd) {
			produced = append(produced, res)
			if _, quit := res.(tea.QuitMsg); quit {
				return produced
			}
			queue = append(queue, res)
		}
	}
	return produced
}

// execCmd runs cmd, flattening batches and sequences.
func execCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	res := cmd()
	switch r := res.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range r {
			out = append(out, execCmd(c)...)
		}
		return out
	}
	return []tea.Msg{res}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "f1":
		return tea.KeyPressMsg{Code: tea.KeyF1}
	}
	if strings.HasPrefix(s, "ctrl+") {
		return tea.KeyPressMsg{Code: rune(s[5]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// start creates the app, sizes it, and runs Init.
func start(t *testing.T, opts Options, w, h int) *Model {
	t.Helper()
	m := New(opts)
	run(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	for _, msg := range execCmd(m.Init()) {
		run(t, m, msg)
	}
	return m
}

func screen(m *Model) string { return ansi.Strip(m.View().Content) }

func hasQuit(ms []tea.Msg) bool {
	for _, m := range ms {
		if _, ok := m.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

func TestViewShowsShell(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	v := m.View()
	if !v.AltScreen {
		t.Error("View().AltScreen = false")
	}
	s := screen(m)
	for _, want := range []string{"◆ Notty", "NOTES", "Work", "ideas", "No note open · tab to browse notes", "NORMAL", "F1 help"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen missing %q:\n%s", want, s)
		}
	}
	lines := strings.Split(s, "\n")
	if len(lines) != 30 {
		t.Errorf("screen has %d lines, want 30", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 120 {
			t.Errorf("line %d has width %d, want 120", i, w)
		}
	}
}

func TestEmptyMainPane(t *testing.T) {
	tests := []struct {
		name string
		w, h int
		hint string
	}{
		{"sidebar shown", 120, 20, "No note open · tab to browse notes"},
		{"sidebar can be shown", 60, 12, "ctrl+b to show notes"},
		{"too narrow for the sidebar", 36, 12, "No note open"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			m := start(t, opts, tt.w, tt.h)
			s := screen(m)
			// The hint may wrap: compare with whitespace collapsed.
			flat := strings.Join(strings.Fields(strings.ReplaceAll(s, "│", " ")), " ")
			if !strings.Contains(flat, tt.hint) {
				t.Errorf("hint %q missing:\n%s", tt.hint, s)
			}
			if tt.w < 48 && strings.Contains(flat, "ctrl+b") {
				t.Errorf("hint offers ctrl+b although the sidebar cannot fit:\n%s", s)
			}
			for i, l := range strings.Split(s, "\n") {
				if w := ansi.StringWidth(l); w != tt.w {
					t.Errorf("line %d width %d, want %d", i, w, tt.w)
				}
			}
			if title := filepath.Base(opts.Vault.Root); !strings.Contains(s, "─ "+title+" ─") {
				t.Errorf("main pane title %q missing:\n%s", title, s)
			}
		})
	}
}

func TestViewDoesNotMutate(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.OpenNoteMsg{Path: "ideas.md", Line: -1})
	run(t, m, msgs.SyncStatusMsg{State: msgs.SyncSynced})
	before := *m
	_ = m.View()
	if !reflect.DeepEqual(before, *m) {
		t.Error("View() changed the model")
	}
}

func TestSidebarHiddenWhenNarrowAndToggle(t *testing.T) {
	m := start(t, testOptions(t), 70, 24)
	if m.SidebarVisible() {
		t.Fatal("sidebar visible at 70 columns")
	}
	if m.Focus() != FocusMain {
		t.Errorf("focus = %v, want FocusMain when the sidebar is hidden", m.Focus())
	}
	if strings.Contains(screen(m), "◆ Notty") {
		t.Error("sidebar drawn while hidden")
	}
	run(t, m, keyMsg("ctrl+b"))
	if !m.SidebarVisible() || !strings.Contains(screen(m), "◆ Notty") {
		t.Error("ctrl+b did not show the sidebar")
	}
	// A later resize keeps the user's choice.
	run(t, m, tea.WindowSizeMsg{Width: 72, Height: 24})
	if !m.SidebarVisible() {
		t.Error("resize overrode the ctrl+b choice")
	}
	run(t, m, keyMsg("ctrl+b"))
	if m.SidebarVisible() {
		t.Error("second ctrl+b did not hide the sidebar")
	}
}

func TestFocusNeverOnUndrawnSidebar(t *testing.T) {
	m := start(t, testOptions(t), 40, 12)
	if m.Focus() != FocusMain {
		t.Fatalf("initial focus = %v, want FocusMain", m.Focus())
	}
	run(t, m, keyMsg("tab"))
	if m.Focus() != FocusMain {
		t.Errorf("tab at 40x12: focus = %v, want FocusMain", m.Focus())
	}
	run(t, m, keyMsg("ctrl+b")) // too narrow: the sidebar still cannot be drawn
	run(t, m, msgs.FocusSidebarMsg{})
	if m.Focus() != FocusMain {
		t.Errorf("FocusSidebarMsg at 40x12: focus = %v, want FocusMain", m.Focus())
	}
	if strings.Contains(screen(m), "◆ Notty") {
		t.Errorf("sidebar drawn at 40 columns:\n%s", screen(m))
	}
}

func TestCycleNoteView(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	want := []NoteView{ViewSplit, ViewPreview, ViewEditor}
	for _, w := range want {
		run(t, m, keyMsg("ctrl+g"))
		if m.NoteView() != w {
			t.Errorf("NoteView = %v, want %v", m.NoteView(), w)
		}
	}
	run(t, m, msgs.CycleNoteViewMsg{})
	if m.NoteView() != ViewSplit {
		t.Errorf("CycleNoteViewMsg: NoteView = %v", m.NoteView())
	}
}

func TestQuit(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{"ctrl+q", keyMsg("ctrl+q")},
		{"sidebar q", keyMsg("q")},
		{"QuitMsg", msgs.QuitMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := start(t, testOptions(t), 120, 30)
			if !hasQuit(run(t, m, tt.msg)) {
				t.Errorf("%s did not quit", tt.name)
			}
		})
	}
}

func TestQuitSavesLocalState(t *testing.T) {
	for _, quitMsg := range []tea.Msg{keyMsg("ctrl+q"), msgs.QuitMsg{}} {
		opts := testOptions(t)
		m := start(t, opts, 120, 30)
		opts.Local.Touch("ideas.md") // changed but not saved yet
		if !hasQuit(run(t, m, quitMsg)) {
			t.Fatalf("%#v did not quit", quitMsg)
		}
		saved, err := localstate.Load(opts.LocalPath)
		if err != nil {
			t.Fatal(err)
		}
		if saved.LastNote != "ideas.md" {
			t.Errorf("%#v: saved LastNote = %q, want the state saved on quit", quitMsg, saved.LastNote)
		}
	}
}

func TestGlobalActionsEmitMessages(t *testing.T) {
	tests := []struct {
		key  string
		want tea.Msg
	}{
		{"f1", msgs.OpenHelpMsg{}},
		{"ctrl+p", msgs.OpenFinderMsg{}},
		{"ctrl+f", msgs.OpenFinderMsg{FullText: true}},
		{"ctrl+k", msgs.OpenPaletteMsg{}},
		{"ctrl+s", msgs.SaveRequestMsg{}},
	}
	for _, tt := range tests {
		m := start(t, testOptions(t), 120, 30)
		got := run(t, m, keyMsg(tt.key))
		if len(got) != 1 || !reflect.DeepEqual(got[0], tt.want) {
			t.Errorf("%s produced %#v, want [%#v]", tt.key, got, tt.want)
		}
	}
}

func TestFocusSwitching(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	if m.Focus() != FocusSidebar {
		t.Fatalf("initial focus = %v, want FocusSidebar", m.Focus())
	}
	run(t, m, keyMsg("tab"))
	if m.Focus() != FocusMain {
		t.Errorf("tab in sidebar: focus = %v", m.Focus())
	}
	run(t, m, keyMsg("tab"))
	if m.Focus() != FocusSidebar {
		t.Errorf("tab in main: focus = %v", m.Focus())
	}
	run(t, m, msgs.FocusMainMsg{})
	if m.Focus() != FocusMain {
		t.Errorf("FocusMainMsg: focus = %v", m.Focus())
	}
	run(t, m, msgs.FocusSidebarMsg{})
	if m.Focus() != FocusSidebar {
		t.Errorf("FocusSidebarMsg: focus = %v", m.Focus())
	}
	// Hiding the sidebar moves focus to the main pane.
	run(t, m, keyMsg("ctrl+b"))
	if m.Focus() != FocusMain {
		t.Errorf("after hiding the sidebar: focus = %v", m.Focus())
	}
	// Asking for sidebar focus while hidden shows it.
	run(t, m, msgs.FocusSidebarMsg{})
	if !m.SidebarVisible() || m.Focus() != FocusSidebar {
		t.Errorf("FocusSidebarMsg while hidden: visible=%v focus=%v", m.SidebarVisible(), m.Focus())
	}
}

func TestOpenNote(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: -1})

	if m.NotePath() != "Work/Standup notes.md" {
		t.Errorf("NotePath = %q", m.NotePath())
	}
	if m.Focus() != FocusMain {
		t.Errorf("focus = %v, want FocusMain after opening a note", m.Focus())
	}
	s := screen(m)
	for _, want := range []string{"Work / Standup notes", "Work/Standup notes.md", "words", "shipped auth flow"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen missing %q:\n%s", want, s)
		}
	}
	if got := opts.Local.Recents; len(got) == 0 || got[0] != "Work/Standup notes.md" {
		t.Errorf("Recents = %v", got)
	}
	saved, err := localstate.Load(opts.LocalPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.LastNote != "Work/Standup notes.md" {
		t.Errorf("saved LastNote = %q", saved.LastNote)
	}
	// Opening reveals the note in the sidebar, expanding its folder.
	if !reflect.DeepEqual(saved.Expanded, []string{"Work"}) {
		t.Errorf("saved Expanded = %v, want [Work]", saved.Expanded)
	}
}

func TestLastNoteReopenedOnStart(t *testing.T) {
	opts := testOptions(t)
	opts.Local.LastNote = "ideas.md"
	m := start(t, opts, 120, 30)
	if m.NotePath() != "ideas.md" {
		t.Errorf("NotePath = %q, want the last note reopened", m.NotePath())
	}

	opts = testOptions(t)
	opts.Local.LastNote = "gone.md"
	m = start(t, opts, 120, 30)
	if m.NotePath() != "" {
		t.Errorf("NotePath = %q, want nothing for a missing last note", m.NotePath())
	}
	if len(m.toast.Log()) != 0 {
		t.Errorf("toasts = %v, want none for a missing last note", toastTexts(m))
	}
}

func TestOpenNoteFromSidebar(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	// Rows: Work, ideas.md. Move to ideas and press enter.
	run(t, m, keyMsg("j"))
	run(t, m, keyMsg("enter"))
	if m.NotePath() != "ideas.md" {
		t.Errorf("NotePath = %q, want ideas.md", m.NotePath())
	}
	if !strings.Contains(screen(m), "─ Ideas ─") {
		t.Errorf("pane title missing:\n%s", screen(m))
	}
}

func TestStaleNoteLoadDropped(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	_, cmdA := m.Update(msgs.OpenNoteMsg{Path: "ideas.md", Line: -1})
	_, cmdB := m.Update(msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: -1})
	loadedA, loadedB := cmdA(), cmdB()
	// B finishes first, then the stale A arrives.
	run(t, m, loadedB)
	run(t, m, loadedA)
	if m.NotePath() != "Work/Standup notes.md" {
		t.Errorf("NotePath = %q, want the most recently requested note", m.NotePath())
	}
	// A stale failure does not toast either.
	_, cmdC := m.Update(msgs.OpenNoteMsg{Path: "missing.md", Line: -1})
	_, cmdD := m.Update(msgs.OpenNoteMsg{Path: "ideas.md", Line: -1})
	loadedC, loadedD := cmdC(), cmdD()
	run(t, m, loadedD)
	if got := run(t, m, loadedC); len(got) != 0 {
		t.Errorf("stale failed load produced %#v", got)
	}
	if m.NotePath() != "ideas.md" {
		t.Errorf("NotePath = %q, want ideas.md", m.NotePath())
	}
}

func TestOpenMissingNoteToasts(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	got := run(t, m, msgs.OpenNoteMsg{Path: "nope.md", Line: -1})
	var toast bool
	for _, g := range got {
		if tm, ok := g.(msgs.ToastMsg); ok && tm.Level == msgs.ToastError {
			toast = true
		}
	}
	if !toast {
		t.Errorf("no error toast for a missing note: %#v", got)
	}
	if m.NotePath() != "" {
		t.Errorf("NotePath = %q after a failed open", m.NotePath())
	}
}

func TestExpandedStateSaved(t *testing.T) {
	opts := testOptions(t)
	opts.Local.Expanded = []string{}
	m := start(t, opts, 120, 30)
	run(t, m, keyMsg("l")) // expand Work (first row)
	if !reflect.DeepEqual(opts.Local.Expanded, []string{"Work"}) {
		t.Errorf("Local.Expanded = %v", opts.Local.Expanded)
	}
	saved, err := localstate.Load(opts.LocalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Expanded, []string{"Work"}) {
		t.Errorf("saved Expanded = %v", saved.Expanded)
	}
	if !strings.Contains(screen(m), "Standup notes") {
		t.Errorf("expanded folder children not shown:\n%s", screen(m))
	}
}

func TestExpandedRestored(t *testing.T) {
	opts := testOptions(t)
	opts.Local.Expanded = []string{"Work"}
	m := start(t, opts, 120, 30)
	if !strings.Contains(screen(m), "Standup notes") {
		t.Errorf("saved expanded folder not restored:\n%s", screen(m))
	}
}

func TestPinsShown(t *testing.T) {
	opts := testOptions(t)
	opts.Pins = &meta.State{Pins: []string{"ideas.md"}}
	m := start(t, opts, 120, 30)
	if !strings.Contains(screen(m), "PINNED") {
		t.Errorf("PINNED section missing:\n%s", screen(m))
	}
}

func TestEntriesSwitchMainView(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTasks})
	if m.MainView() != ViewTasks {
		t.Errorf("MainView = %v, want ViewTasks", m.MainView())
	}
	run(t, m, keyMsg("esc"))
	if m.MainView() != ViewNote {
		t.Errorf("esc: MainView = %v, want ViewNote", m.MainView())
	}
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTrash})
	if m.MainView() != ViewTrash {
		t.Errorf("MainView = %v, want ViewTrash", m.MainView())
	}
}

func TestSyncStatusShown(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.SyncStatusMsg{State: msgs.SyncOffline, Pending: 2})
	if !strings.Contains(screen(m), "⊘ offline (2 pending)") {
		t.Errorf("sync status missing:\n%s", screen(m))
	}
}

func TestZenLayoutCentersPlaceholder(t *testing.T) {
	m := start(t, testOptions(t), 160, 20)
	run(t, m, keyMsg("ctrl+b"))
	if m.SidebarVisible() {
		t.Fatal("sidebar still visible")
	}
	s := screen(m)
	if !strings.Contains(s, "No note open · ctrl+b to show notes") {
		t.Fatalf("placeholder missing:\n%s", s)
	}
}

func TestWizardPlaceholder(t *testing.T) {
	opts := testOptions(t)
	opts.WizardNeeded = true
	opts.Vault = nil
	m := start(t, opts, 100, 30)
	s := screen(m)
	if !strings.Contains(s, "Setup wizard coming soon") {
		t.Errorf("wizard placeholder missing:\n%s", s)
	}
	if strings.Contains(s, "◆ Notty ─") {
		t.Errorf("main screen drawn in wizard mode:\n%s", s)
	}
	for _, k := range []string{"q", "ctrl+g", "f1", "ctrl+b"} {
		if got := run(t, m, keyMsg(k)); len(got) != 0 {
			t.Errorf("wizard: %s produced %#v, want nothing", k, got)
		}
	}
	if !hasQuit(run(t, m, keyMsg("ctrl+q"))) {
		t.Error("wizard: ctrl+q did not quit")
	}
}
