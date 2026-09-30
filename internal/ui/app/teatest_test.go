package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// Real-program tests.
//
// The renderer writes only the cells that changed since the last frame, so
// the program's output stream is a sequence of partial redraws: when
// "Trash · 0 items" becomes "Trash · 1 item", the stream gets a cursor move
// and "1 item", never the new title in one piece. Checking the stream for
// text only works for text that appears all at once, and races with the
// order in which asynchronous results arrive. waitScreen instead asks the
// running program for its current screen, through a probe message that
// the wrapped model answers from its event loop.

// screenProbe asks a running program for its current screen, stripped of
// ANSI sequences.
type screenProbe chan string

// probedModel is the app as run by newProgram: it answers screen probes
// and passes every other message on.
type probedModel struct{ *Model }

func (p probedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if probe, ok := msg.(screenProbe); ok {
		probe <- ansi.Strip(p.View().Content)
		return p, nil
	}
	_, cmd := p.Model.Update(msg)
	return p, cmd
}

// newProgram runs the app on opts as a real Bubble Tea program in a
// 120x30 terminal.
func newProgram(t *testing.T, opts Options) *teatest.TestModel {
	t.Helper()
	return teatest.NewTestModel(t, probedModel{New(opts)}, teatest.WithInitialTermSize(120, 30))
}

// finalModel is the app's model once the program has finished.
func finalModel(t *testing.T, tm *teatest.TestModel) *Model {
	t.Helper()
	p, ok := tm.FinalModel(t).(probedModel)
	if !ok {
		t.Fatalf("final model is %T, want a program started by newProgram", tm.FinalModel(t))
	}
	return p.Model
}

// waitScreen waits up to 5s until the program's current screen contains
// every want. The program must have been started by newProgram.
func waitScreen(t *testing.T, tm *teatest.TestModel, want ...string) {
	t.Helper()
	deadline := time.Now().Add(cmdLimit)
	var last string
	for {
		probe := make(screenProbe, 1)
		tm.Send(probe)
		select {
		case last = <-probe:
		case <-time.After(time.Until(deadline)):
			t.Fatalf("waitScreen %q: the program did not answer within %v. Last screen:\n%s", want, cmdLimit, last)
		}
		if containsAll(last, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitScreen: %q not on the screen after %v. Last screen:\n%s", want, cmdLimit, last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func containsAll(s string, want []string) bool {
	for _, w := range want {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}

// TestWaitScreenSeesPartialRedraws checks that waitScreen looks at the
// screen, not at the output stream: when the trash count goes from 0 to 1,
// the stream holds "Trash · 0 items" followed by a lone "1 item", never
// "Trash · 1 item" in one piece.
func TestWaitScreenSeesPartialRedraws(t *testing.T) {
	opts := testOptions(t)
	tm := newProgram(t, opts)
	tm.Send(msgs.ActivateEntryMsg{Entry: msgs.EntryTrash})
	waitScreen(t, tm, "Trash · 0 items", "Trash is empty")
	if _, err := opts.Vault.Trash("ideas.md"); err != nil {
		t.Fatal(err)
	}
	tm.Send(loadTrashCmd(opts.Vault)())
	waitScreen(t, tm, "Trash · 1 item", "Trash (1)")
	tm.Send(keyMsg("ctrl+q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// TestProgramStartsAndQuits runs the app as a real Bubble Tea program on a
// temp vault with two notes, checks the rendered screen, and quits with
// ctrl+q.
func TestProgramStartsAndQuits(t *testing.T) {
	opts := testOptions(t)
	opts.Local.Expanded = []string{"Work"}
	tm := newProgram(t, opts)
	waitScreen(t, tm, "Notty", "N O T E S", "Standup", "ideas", "NORMAL", "F1", "help")

	tm.Send(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))

	screen := ansi.Strip(finalModel(t, tm).View().Content)
	for _, want := range []string{"◆ Notty", "Standup notes", "ideas", "F1 help"} {
		if !strings.Contains(screen, want) {
			t.Errorf("final screen missing %q:\n%s", want, screen)
		}
	}
}
