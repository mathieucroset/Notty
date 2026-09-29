package app

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/recovery"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// TestSnapshotFollowsTheBuffer checks that the recovery snapshot holds the
// open note's path, text and dirty state after each step.
func TestSnapshotFollowsTheBuffer(t *testing.T) {
	opts := testOptions(t)
	snap := &recovery.Snapshot{}
	opts.Snapshot = snap
	m := openNote(t, opts, "ideas.md")
	steps := []struct {
		name       string
		do         func()
		rel        string
		wantPrefix string
		dirty      bool
	}{
		{"opened", func() {}, "ideas.md", "# Ideas", false},
		{"typed", func() { insertText(t, m, "typed ") }, "ideas.md", "typed # Ideas", true},
		{"saved", func() { run(t, m, msgs.SaveRequestMsg{}) }, "ideas.md", "typed # Ideas", false},
		{"typed again", func() { insertText(t, m, "more ") }, "ideas.md", "typedmore  # Ideas", true},
		{"other note", func() { run(t, m, msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: -1}) },
			"Work/Standup notes.md", "# Standup notes", false},
	}
	for _, s := range steps {
		s.do()
		root, rel, content, dirty := snap.Get()
		if root != opts.Vault.Root || rel != s.rel || !strings.HasPrefix(content, s.wantPrefix) || dirty != s.dirty {
			t.Errorf("%s: snapshot = %q %q %.30q dirty %v; want %q %q %q dirty %v",
				s.name, root, rel, content, dirty, opts.Vault.Root, s.rel, s.wantPrefix, s.dirty)
		}
	}
}

// Without a snapshot the app runs as before.
func TestNoSnapshot(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	insertText(t, m, "typed ")
	if !m.editor.Dirty() {
		t.Error("edit lost")
	}
}

// fakeModel panics where told to, and returns cmd from Init and Update.
type fakeModel struct {
	panicIn string // "init", "update" or "view"
	cmd     tea.Cmd
	got     []tea.Msg
}

func (f *fakeModel) Init() tea.Cmd {
	if f.panicIn == "init" {
		panic("boom in init")
	}
	return f.cmd
}

func (f *fakeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	f.got = append(f.got, msg)
	if f.panicIn == "update" {
		panic("boom in update")
	}
	return f, f.cmd
}

func (f *fakeModel) View() tea.View {
	if f.panicIn == "view" {
		panic("boom in view")
	}
	return tea.NewView("fine")
}

type okMsg struct{}

func panicking() tea.Msg { panic("boom in cmd") }

// flatten runs cmd and every command inside batches and sequences, and
// returns the messages they produced.
func flatten(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		var out []tea.Msg
		for i := range v.Len() {
			out = append(out, flatten(v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// TestGuardRecordsPanics checks that a panic in Init, Update or a command
// (alone, batched or sequenced) is recorded in the snapshot with its
// stack, and quits the program instead of crashing it.
func TestGuardRecordsPanics(t *testing.T) {
	tests := []struct {
		name    string
		model   *fakeModel
		call    func(g tea.Model) tea.Cmd
		value   string
		inStack string
	}{
		{"init", &fakeModel{panicIn: "init"}, func(g tea.Model) tea.Cmd { return g.Init() },
			"boom in init", "(*fakeModel).Init"},
		{"update", &fakeModel{panicIn: "update"}, func(g tea.Model) tea.Cmd { _, c := g.Update(okMsg{}); return c },
			"boom in update", "(*fakeModel).Update"},
		{"command", &fakeModel{cmd: panicking}, func(g tea.Model) tea.Cmd { return feedBack(g, g.Init()) },
			"boom in cmd", "app.panicking"},
		{"batch", &fakeModel{cmd: tea.Batch(func() tea.Msg { return okMsg{} }, panicking)},
			func(g tea.Model) tea.Cmd { return feedBack(g, g.Init()) }, "boom in cmd", "app.panicking"},
		{"sequence", &fakeModel{cmd: tea.Sequence(func() tea.Msg { return okMsg{} }, panicking)},
			func(g tea.Model) tea.Cmd { return feedBack(g, g.Init()) }, "boom in cmd", "app.panicking"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := &recovery.Snapshot{}
			g := Guard(tt.model, snap)
			if cmd := tt.call(g); !isQuit(cmd) {
				t.Error("the panic did not quit the program")
			}
			v, stack, ok := snap.Panic()
			if !ok || v != tt.value || !strings.Contains(string(stack), tt.inStack) {
				t.Errorf("recorded %v, %v, stack has %q: %v", v, ok, tt.inStack, strings.Contains(string(stack), tt.inStack))
			}
			// Once crashed, the model sees nothing more and draws nothing.
			before := len(tt.model.got)
			if _, cmd := g.Update(okMsg{}); cmd != nil || len(tt.model.got) != before {
				t.Error("the crashed model still gets messages")
			}
			if c := g.View().Content; c != "" {
				t.Errorf("crashed view = %q, want empty", c)
			}
		})
	}
}

// feedBack runs cmd's commands and feeds their messages to g, returning
// the last command g produced.
func feedBack(g tea.Model, cmd tea.Cmd) tea.Cmd {
	var last tea.Cmd
	for _, msg := range flatten(cmd) {
		_, last = g.Update(msg)
		if isQuit(last) {
			return last
		}
	}
	return last
}

// A panic while drawing is recorded, then left to Bubble Tea, which
// restores the terminal and makes Run fail with ErrProgramPanic.
func TestGuardRecordsViewPanics(t *testing.T) {
	snap := &recovery.Snapshot{}
	g := Guard(&fakeModel{panicIn: "view"}, snap)
	func() {
		defer func() {
			if r := recover(); r != "boom in view" {
				t.Errorf("recovered %v, want the view panic passed on", r)
			}
		}()
		g.View()
	}()
	if v, _, ok := snap.Panic(); !ok || v != "boom in view" {
		t.Errorf("recorded %v, %v", v, ok)
	}
}

// Without a panic the guard is transparent.
func TestGuardPassesThrough(t *testing.T) {
	f := &fakeModel{cmd: tea.Batch(func() tea.Msg { return okMsg{} }, func() tea.Msg { return msgs.QuitMsg{} })}
	snap := &recovery.Snapshot{}
	g := Guard(f, snap)
	got := flatten(g.Init())
	if len(got) != 2 || got[0] != (okMsg{}) || got[1] != (msgs.QuitMsg{}) {
		t.Errorf("Init commands produced %v", got)
	}
	if _, cmd := g.Update(okMsg{}); cmd == nil || len(f.got) != 1 {
		t.Errorf("Update not passed on: got %v, cmd %v", f.got, cmd != nil)
	}
	if c := g.View().Content; c != "fine" {
		t.Errorf("View = %q", c)
	}
	if _, _, ok := snap.Panic(); ok {
		t.Error("a panic was recorded")
	}
}

// TestGuardInAProgram runs guarded models that panic in a real Bubble Tea
// program: it ends cleanly, with the panic recorded.
func TestGuardInAProgram(t *testing.T) {
	tests := []struct {
		name  string
		model *fakeModel
		value string
	}{
		{"update", &fakeModel{panicIn: "update"}, "boom in update"},
		{"command", &fakeModel{cmd: panicking}, "boom in cmd"},
		{"batch", &fakeModel{cmd: tea.Batch(func() tea.Msg { return okMsg{} }, panicking)}, "boom in cmd"},
		{"sequence", &fakeModel{cmd: tea.Sequence(func() tea.Msg { return okMsg{} }, panicking)}, "boom in cmd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			snap := &recovery.Snapshot{}
			p := tea.NewProgram(Guard(tt.model, snap), tea.WithContext(ctx), tea.WithInput(nil),
				tea.WithOutput(io.Discard), tea.WithWindowSize(80, 24), tea.WithoutSignalHandler())
			if _, err := p.Run(); err != nil {
				t.Fatalf("Run = %v, want a clean quit", err)
			}
			if v, _, ok := snap.Panic(); !ok || v != tt.value {
				t.Errorf("recorded %v, %v; want %q", v, ok, tt.value)
			}
		})
	}
}
