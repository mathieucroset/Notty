package app

import (
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/wizard"
	"github.com/mathieucroset/notty/internal/vault"
)

// screenSizes are the terminal sizes every screen is captured at (Task 39):
// a roomy window, the classic 80×24, and a narrow one below the sidebar's
// 80-column threshold.
var screenSizes = [][2]int{{120, 40}, {80, 24}, {70, 24}}

// showcaseNote exercises the editor's display substitutions: a heading
// with task progress, tasks, tags, a quote, and emphasis.
const showcaseNote = "# Standup notes\n\n- shipped **auth** flow\n- [ ] follow up with #design\n- [x] review the PR #todo\n\n> Keep it short.\n\n## Next\n\nPlan the *sprint* demo.\n"

// showcaseOptions is testOptions with a pinned note, tags, tasks, a
// second folder, and a fixed clock, so the sidebar shows every section.
func showcaseOptions(t *testing.T) Options {
	t.Helper()
	opts := testOptions(t)
	opts.Now = func() time.Time { return fixedNow }
	writeFile(t, opts.Vault, "Work/Standup notes.md", showcaseNote)
	writeFile(t, opts.Vault, "Personal/Journal.md", "# Journal\n\nA quiet day. #life\n")
	opts.Local.Expanded = []string{"Work"}
	opts.Pins.Pins = []string{"Work/Standup notes.md"}
	return opts
}

// openShowcase starts the showcase vault with the standup note open and
// the editor focused.
func openShowcase(t *testing.T, w, h int) *Model {
	t.Helper()
	m := start(t, showcaseOptions(t), w, h)
	run(t, m, msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: 0})
	return m
}

// fixedGitDates makes the commits that follow carry a fixed date, so the
// history view renders the same text on every run. Call it after
// gittest.New, which clears the date variables.
func fixedGitDates(t *testing.T) {
	t.Helper()
	const date = "2026-09-20T09:30:00+00:00"
	t.Setenv("GIT_AUTHOR_DATE", date)
	t.Setenv("GIT_COMMITTER_DATE", date)
}

// fakeTrashed trashes rel by hand with a fixed host and deletion time, so
// the trash view does not show this machine's hostname or the real clock.
func fakeTrashed(t *testing.T, v *vault.Vault, rel string, at time.Time) {
	t.Helper()
	id := vault.NewTrashID(at, "laptop")
	body, err := os.ReadFile(v.Abs(rel))
	if err != nil {
		t.Fatal(err)
	}
	meta := fmt.Sprintf("{\"original_path\": %q, \"deleted_at\": %q, \"host\": \"laptop\", \"is_dir\": false}\n",
		rel, at.UTC().Format(time.RFC3339))
	writeFile(t, v, ".trash/"+id+"/meta.json", meta)
	writeFile(t, v, ".trash/"+id+"/"+path.Base(rel), string(body))
	if err := os.Remove(v.Abs(rel)); err != nil {
		t.Fatal(err)
	}
}

// screenCase builds one screen at a size.
type screenCase struct {
	name  string
	build func(t *testing.T, w, h int) *Model
}

var screenCases = []screenCase{
	{"main", func(t *testing.T, w, h int) *Model {
		m := openShowcase(t, w, h)
		run(t, m, msgs.FocusSidebarMsg{})
		return m
	}},
	{"editor", func(t *testing.T, w, h int) *Model {
		return openShowcase(t, w, h)
	}},
	{"split", func(t *testing.T, w, h int) *Model {
		m := openShowcase(t, w, h)
		run(t, m, keyMsg("ctrl+g"))
		return m
	}},
	{"preview", func(t *testing.T, w, h int) *Model {
		m := openShowcase(t, w, h)
		run(t, m, keyMsg("ctrl+g"))
		run(t, m, keyMsg("ctrl+g"))
		return m
	}},
	{"finder", func(t *testing.T, w, h int) *Model {
		m := openShowcase(t, w, h)
		run(t, m, keyMsg("ctrl+p"))
		return m
	}},
	{"search", func(t *testing.T, w, h int) *Model {
		m := openShowcase(t, w, h)
		run(t, m, keyMsg("ctrl+f"))
		typeText(t, m, "sprint")
		waitFor(t, m, func() bool { return strings.Contains(screen(m), "demo") && !strings.Contains(screen(m), "searching…") })
		return m
	}},
	{"palette", func(t *testing.T, w, h int) *Model {
		m := openShowcase(t, w, h)
		run(t, m, keyMsg("ctrl+k"))
		return m
	}},
	{"help", func(t *testing.T, w, h int) *Model {
		m := openShowcase(t, w, h)
		run(t, m, keyMsg("f1"))
		return m
	}},
	{"tasks", func(t *testing.T, w, h int) *Model {
		opts := showcaseOptions(t)
		writeFile(t, opts.Vault, "Work/Todo.md", todoNote)
		m := start(t, opts, w, h)
		run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTasks})
		return m
	}},
	{"trash", func(t *testing.T, w, h int) *Model {
		opts := showcaseOptions(t)
		fakeTrashed(t, opts.Vault, "ideas.md", fixedNow.Add(-50*time.Hour))
		m := start(t, opts, w, h)
		run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryTrash})
		return m
	}},
	{"history", func(t *testing.T, w, h int) *Model {
		env := gittest.New(t)
		fixedGitDates(t)
		for _, body := range []string{"first", "second", "third"} {
			gittest.Write(t, env.Laptop, "ideas.md", "# Ideas\n\n"+body+" version\n")
			gittest.CommitAll(t, env.Laptop, "Update ideas.md · laptop")
		}
		v, err := vault.Open(env.Laptop.Dir)
		if err != nil {
			t.Fatal(err)
		}
		opts := testOptions(t)
		opts.Vault = v
		m := start(t, opts, w, h)
		run(t, m, msgs.OpenHistoryMsg{Path: "ideas.md"})
		if m.history == nil {
			t.Fatalf("history not open; toasts %v", toastTexts(m))
		}
		return m
	}},
	{"resolver", func(t *testing.T, w, h int) *Model {
		_, m, _ := conflictEnv(t)
		run(t, m, tea.WindowSizeMsg{Width: w, Height: h})
		run(t, m, msgs.OpenResolverMsg{Path: "README.md"})
		if m.resolver == nil {
			t.Fatalf("resolver not open; toasts %v", toastTexts(m))
		}
		return m
	}},
	{"wizard", func(t *testing.T, w, h int) *Model {
		gittest.Isolate(t)
		opts := wizardOptions(t)
		opts.Config.Vault = "/home/me/Notes"
		m := start(t, opts, w, h)
		t.Cleanup(m.Shutdown)
		return m
	}},
	{"wizard-sync", func(t *testing.T, w, h int) *Model {
		gittest.Isolate(t)
		opts := wizardOptions(t)
		m := start(t, opts, w, h)
		t.Cleanup(m.Shutdown)
		run(t, m, keyMsg("enter"))
		waitFor(t, m, func() bool { return m.wizard != nil && m.wizard.Stage() != wizard.StageVault })
		return m
	}},
	{"toasts", func(t *testing.T, w, h int) *Model {
		m := openShowcase(t, w, h)
		run(t, m, msgs.ToastMsg{Level: msgs.ToastInfo, Text: "Saved Standup notes"})
		run(t, m, msgs.ToastMsg{Level: msgs.ToastWarn, Text: "Could not reach GitHub, will retry"})
		run(t, m, msgs.ToastMsg{Level: msgs.ToastError, Text: "Disk full: the note was not saved"})
		return m
	}},
}

// TestScreens captures every screen at every size as a golden file of the
// plain text (colours are checked by the component tests), and checks that
// the screen fills the terminal exactly: no line is wider than the
// terminal, so nothing overflows or wraps.
func TestScreens(t *testing.T) {
	for _, sc := range screenCases {
		for _, size := range screenSizes {
			w, h := size[0], size[1]
			t.Run(fmt.Sprintf("%s-%dx%d", sc.name, w, h), func(t *testing.T) {
				m := sc.build(t, w, h)
				view := m.View().Content
				for i, l := range strings.Split(view, "\n") {
					if got := ansi.StringWidth(l); got > w {
						t.Errorf("line %d is %d columns wide, wider than the %d-column terminal: %q", i, got, w, ansi.Strip(l))
					}
				}
				assertSize(t, m, w, h)
				teatest.RequireEqualOutput(t, []byte(ansi.Strip(view)))
			})
		}
	}
}
