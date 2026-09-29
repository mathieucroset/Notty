package wizard

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func init() { inspectDelay = 0 }

// fakeGH is a setup.GH with a fixed availability.
type fakeGH struct{ ok bool }

func (g fakeGH) Available(context.Context) bool                   { return g.ok }
func (g fakeGH) CreateRepo(context.Context, string, string) error { return nil }

// fakeEnv records what the wizard asked for.
type fakeEnv struct {
	mu sync.Mutex

	gh          bool
	vaultState  setup.VaultState
	vaultErr    error
	notes       int
	remote      setup.RemoteState
	remoteErr   error
	identityErr error // returned by CheckIdentity until SetIdentity is called

	inspected   []string
	remotes     []string
	identitySet [][2]string
	runs        []setup.Request
	runSteps    [][]setup.Step
	// run is the fake runner; nil runs every step and succeeds.
	run func(ctx context.Context, req setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error)
}

func (f *fakeEnv) env() Env {
	return Env{
		GH: fakeGH{ok: f.gh},
		Inspect: func(path string) (setup.VaultState, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.inspected = append(f.inspected, path)
			return f.vaultState, f.vaultErr
		},
		InspectRemote: func(_ context.Context, url string) (setup.RemoteState, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.remotes = append(f.remotes, url)
			return f.remote, f.remoteErr
		},
		CheckIdentity: func(context.Context, string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.identityErr
		},
		SetIdentity: func(_ context.Context, name, email string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.identitySet = append(f.identitySet, [2]string{name, email})
			f.identityErr = nil
			return nil
		},
		Run: func(ctx context.Context, req setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
			f.mu.Lock()
			f.runs = append(f.runs, req)
			f.runSteps = append(f.runSteps, steps)
			run := f.run
			f.mu.Unlock()
			if run != nil {
				return run(ctx, req, steps, progress)
			}
			for i, s := range steps {
				progress(i, s)
			}
			return false, nil
		},
		CountNotes: func(string) int { return f.notes },
		Host:       "testhost",
	}
}

// builtin returns the built-in palette name.
func builtin(name string) theme.Palette {
	p, _ := theme.Get(name)
	return p
}

func testStyles() theme.Styles {
	p, _ := theme.Get("catppuccin-mocha")
	return theme.NewStyles(p)
}

// outbound reports whether msg is meant for the app rather than the wizard.
func outbound(msg tea.Msg) bool {
	switch msg.(type) {
	case DoneMsg, QuitMsg, CancelMsg, ThemePreviewMsg:
		return true
	}
	return false
}

// exec runs cmd and returns the messages it produced, flattening batches and
// dropping spinner ticks (running those would only animate).
func exec(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch msg := msg.(type) {
	case nil:
		return nil
	case spinner.TickMsg:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, exec(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// drive runs cmd to completion, feeding every internal message back into the
// model, and returns the final model and the outbound messages in order.
func drive(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	queue := exec(cmd)
	for n := 0; len(queue) > 0; n++ {
		if n > 1000 {
			t.Fatal("drive: too many messages")
		}
		msg := queue[0]
		queue = queue[1:]
		if outbound(msg) {
			out = append(out, msg)
			continue
		}
		var c tea.Cmd
		m, c = m.Update(msg)
		queue = append(queue, exec(c)...)
	}
	return m, out
}

// send delivers one message and drives the result.
func send(t *testing.T, m Model, msg tea.Msg) (Model, []tea.Msg) {
	t.Helper()
	m, cmd := m.Update(msg)
	return drive(t, m, cmd)
}

func press(t *testing.T, m Model, keys ...string) (Model, []tea.Msg) {
	t.Helper()
	var all []tea.Msg
	for _, k := range keys {
		var out []tea.Msg
		m, out = send(t, m, key(k))
		all = append(all, out...)
	}
	return m, all
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = send(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// start builds a sized wizard and runs Init.
func start(t *testing.T, mode Mode, f *fakeEnv) Model {
	t.Helper()
	m := New(mode, "~/Notes", builtin("catppuccin-mocha"), theme.Catalog{}, f.env(), testStyles()).SetSize(100, 40)
	m, _ = drive(t, m, m.Init())
	return m
}

func plain(m Model) string { return ansi.Strip(m.View()) }

// flat is the view's text with the card borders removed and all whitespace
// collapsed, so wrapped sentences can be matched.
func flat(m Model) string {
	v := strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(plain(m))
	return collapse(v)
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func mustContain(t *testing.T, m Model, want ...string) {
	t.Helper()
	v := flat(m)
	for _, w := range want {
		if !strings.Contains(v, collapse(w)) {
			t.Errorf("view does not contain %q:\n%s", w, plain(m))
		}
	}
}

func mustNotContain(t *testing.T, m Model, bad ...string) {
	t.Helper()
	v := flat(m)
	for _, b := range bad {
		if strings.Contains(v, collapse(b)) {
			t.Errorf("view contains %q:\n%s", b, v)
		}
	}
}

var errBoom = errors.New("boom")
