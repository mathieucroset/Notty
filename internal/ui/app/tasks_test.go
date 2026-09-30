package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
)

var fixedNow = time.Date(2026, 9, 29, 10, 0, 0, 0, time.Local)

const todoNote = "# Todo\n\n- [ ] write report @2026-09-20\n- [ ] buy milk\n- [x] done thing\n"

// tasksOptions is testOptions with a note holding two open tasks (one
// overdue) and a done one, and a fixed clock.
func tasksOptions(t *testing.T) Options {
	t.Helper()
	opts := testOptions(t)
	opts.Now = func() time.Time { return fixedNow }
	writeFile(t, opts.Vault, "Work/Todo.md", todoNote)
	return opts
}

var spaceKey = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}

func TestTasksView(t *testing.T) {
	m := start(t, tasksOptions(t), 120, 30)
	if !strings.Contains(screen(m), "Tasks (2)") {
		t.Errorf("sidebar tasks count missing:\n%s", screen(m))
	}
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTasks})
	if m.MainView() != ViewTasks || m.Focus() != FocusMain {
		t.Fatalf("MainView = %v, focus = %v", m.MainView(), m.Focus())
	}
	s := screen(m)
	for _, want := range []string{"Tasks · 2 open", "O V E R D U E", "write report", "buy milk"} {
		if !strings.Contains(s, want) {
			t.Errorf("tasks view missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "done thing") {
		t.Errorf("done task shown:\n%s", s)
	}
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "write report") && (!strings.HasSuffix(l, " │") || !strings.Contains(l, "││ ")) {
			t.Errorf("no one-column gutter around the tasks view: %q", l)
		}
	}
	assertSize(t, m, 120, 30)
}

func TestTasksViewToggleWritesFile(t *testing.T) {
	opts := tasksOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTasks})
	run(t, m, spaceKey) // the overdue task is selected first
	want := "# Todo\n\n- [x] write report @2026-09-20\n- [ ] buy milk\n- [x] done thing\n"
	if got := readFile(t, opts.Vault, "Work/Todo.md"); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	if s := screen(m); !strings.Contains(s, "Tasks · 1 open") || !strings.Contains(s, "Tasks (1)") {
		t.Errorf("tasks not refreshed after the toggle:\n%s", s)
	}
}

func TestToggleTaskFindsDriftedLine(t *testing.T) {
	opts := tasksOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.ToggleTaskMsg{Path: "Work/Todo.md", Line: 0, Text: "- [ ] buy milk"})
	if got := readFile(t, opts.Vault, "Work/Todo.md"); !strings.Contains(got, "- [x] buy milk") {
		t.Errorf("file = %q", got)
	}
}

func TestToggleTaskKeepsCRLF(t *testing.T) {
	opts := tasksOptions(t)
	writeFile(t, opts.Vault, "win.md", "# Win\r\n\r\n- [ ] one\r\n")
	m := start(t, opts, 120, 30)
	run(t, m, msgs.ToggleTaskMsg{Path: "win.md", Line: 2, Text: "- [ ] one"})
	if got := readFile(t, opts.Vault, "win.md"); got != "# Win\r\n\r\n- [x] one\r\n" {
		t.Errorf("file = %q", got)
	}
}

func TestToggleMissingTaskWarns(t *testing.T) {
	opts := tasksOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.ToggleTaskMsg{Path: "Work/Todo.md", Line: 2, Text: "- [ ] gone"})
	if !hasToast(m, msgs.ToastWarn, "not toggled") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if got := readFile(t, opts.Vault, "Work/Todo.md"); got != todoNote {
		t.Errorf("file changed: %q", got)
	}
}

func TestTasksViewOpenAndBack(t *testing.T) {
	m := start(t, tasksOptions(t), 120, 30)
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTasks})
	run(t, m, keyMsg("enter"))
	if m.MainView() != ViewNote || m.NotePath() != "Work/Todo.md" {
		t.Errorf("enter: MainView = %v, note = %q", m.MainView(), m.NotePath())
	}
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTasks})
	run(t, m, keyMsg("esc"))
	if m.MainView() != ViewNote {
		t.Errorf("esc: MainView = %v", m.MainView())
	}
}

// TestTasksToggleFlow toggles a task from the Tasks view in a real program.
func TestTasksToggleFlow(t *testing.T) {
	opts := tasksOptions(t)
	tm := newProgram(t, opts)
	waitScreen(t, tm, "N O T E S")
	tm.Send(msgs.ActivateEntryMsg{Entry: msgs.EntryTasks})
	waitScreen(t, tm, "O V E R D U E", "write report")
	tm.Send(spaceKey)
	eventually(t, func() bool {
		return strings.Contains(readFile(t, opts.Vault, "Work/Todo.md"), "- [x] write report")
	})
	tm.Send(keyMsg("ctrl+q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	if got := finalModel(t, tm).tasks.Title(); got != "Tasks · 1 open" {
		t.Errorf("tasks title = %q", got)
	}
}

// eventually polls cond for up to 5s.
func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met after 5s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
