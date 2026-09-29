package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// TestProgramStartsAndQuits runs the app as a real Bubble Tea program on a
// temp vault with two notes, checks the rendered screen, and quits with
// ctrl+q.
func TestProgramStartsAndQuits(t *testing.T) {
	opts := testOptions(t)
	opts.Local.Expanded = []string{"Work"}
	tm := teatest.NewTestModel(t, New(opts), teatest.WithInitialTermSize(120, 30))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		s := ansi.Strip(string(b))
		for _, want := range []string{"Notty", "N O T E S", "Standup", "ideas", "NORMAL", "F1", "help"} {
			if !strings.Contains(s, want) {
				return false
			}
		}
		return true
	}, teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))

	final, ok := tm.FinalModel(t).(*Model)
	if !ok {
		t.Fatalf("final model is %T", tm.FinalModel(t))
	}
	screen := ansi.Strip(final.View().Content)
	for _, want := range []string{"◆ Notty", "Standup notes", "ideas", "F1 help"} {
		if !strings.Contains(screen, want) {
			t.Errorf("final screen missing %q:\n%s", want, screen)
		}
	}
}
