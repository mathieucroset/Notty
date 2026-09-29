package wizard

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/setup"
)

// runPhase is where the setup run is.
type runPhase int

const (
	phaseChecking  runPhase = iota // checking the git identity
	phasePreparing                 // inspecting the vault and remote, planning
	phaseRunning                   // executing the steps
	phaseFailed                    // stopped with runErr
	phaseDone                      // finished
)

const textIdentity = "Git needs a name and email to save your notes' history."

// Run messages. gen ties a message to one run so results of an abandoned run
// are ignored.
type (
	identityCheckedMsg struct {
		gen int
		err error
	}
	identitySetMsg struct{ err error }
	planMsg        struct {
		gen   int
		req   setup.Request
		steps []setup.Step
		err   error
	}
	progressMsg struct {
		gen int
		i   int
		ch  chan tea.Msg
	}
	runDoneMsg struct {
		gen        int
		conflicted bool
		err        error
	}
)

// runState is the identity form and the run's progress.
type runState struct {
	// Identity form.
	nameInput, emailInput textinput.Model
	identityField         int // 0 name, 1 email
	identityErr           string
	settingIdentity       bool

	// Run.
	gen        int
	phase      runPhase
	steps      []setup.Step
	current    int // index of the running step, -1 before the first
	failed     int // index of the failed step, -1 when none
	runErr     error
	conflicted bool
}

func newRunState() runState {
	r := runState{current: -1, failed: -1}
	r.nameInput = newInput("Ada Lovelace")
	r.emailInput = newInput("ada@example.com")
	return r
}

// request builds the setup request from the collected choices.
func (m Model) request() setup.Request {
	req := setup.Request{
		Vault:       m.expandedVault(),
		Choice:      m.choice,
		Host:        m.env.Host,
		GHAvailable: m.ghOK,
	}
	switch m.choice {
	case setup.CreateGitHub:
		req.RepoName = strings.TrimSpace(m.repoInput.Value())
	case setup.ExistingURL:
		req.URL = strings.TrimSpace(m.urlInput.Value())
	}
	return req
}

// startSetup leaves the sync step and runs the setup, checking the git
// identity first. Retry calls it again: it re-inspects and re-plans.
func (m Model) startSetup() (Model, tea.Cmd) {
	m.stage = StageRun
	m.run.gen++
	m.run.steps, m.run.current, m.run.failed, m.run.runErr = nil, -1, -1, nil
	m.focus()
	check := m.env.CheckIdentity
	if check == nil {
		return m.plan()
	}
	m.run.phase = phaseChecking
	gen, dir := m.run.gen, m.expandedVault()
	cmd := func() tea.Msg { return identityCheckedMsg{gen: gen, err: check(context.Background(), dir)} }
	return m, tea.Batch(cmd, m.spin())
}

// plan inspects the vault (and the remote) and plans the steps.
func (m Model) plan() (Model, tea.Cmd) {
	m.stage = StageRun
	m.run.phase = phasePreparing
	m.focus()
	gen, req := m.run.gen, m.request()
	inspect, inspectRemote := m.env.Inspect, m.env.InspectRemote
	cmd := func() tea.Msg {
		msg := planMsg{gen: gen, req: req}
		if inspect == nil {
			msg.err = errors.New("setup: no vault inspector")
			return msg
		}
		vs, err := inspect(req.Vault)
		if err != nil {
			msg.err = err
			return msg
		}
		var rs setup.RemoteState
		if req.Choice == setup.ExistingURL {
			if inspectRemote == nil {
				msg.err = errors.New("setup: no remote inspector")
				return msg
			}
			if rs, err = inspectRemote(context.Background(), req.URL); err != nil {
				msg.err = err
				return msg
			}
		}
		msg.steps, msg.err = setup.Plan(req, vs, rs)
		return msg
	}
	return m, tea.Batch(cmd, m.spin())
}

// execCmd runs the steps on a goroutine. Progress and the final result are
// delivered through a channel, one message per Cmd (progress may be called
// from another goroutine, such as the syncer's worker).
func (m Model) execCmd(gen int, req setup.Request, steps []setup.Step) tea.Cmd {
	run := m.env.Run
	return func() tea.Msg {
		if run == nil {
			return runDoneMsg{gen: gen, err: errors.New("setup: no runner")}
		}
		ch := make(chan tea.Msg, len(steps)+1)
		go func() {
			conflicted, err := run(context.Background(), req, steps, func(i int, _ setup.Step) {
				select {
				case ch <- progressMsg{gen: gen, i: i, ch: ch}:
				default: // progress is advisory; never block the runner
				}
			})
			ch <- runDoneMsg{gen: gen, conflicted: conflicted, err: err}
		}()
		return <-ch
	}
}

func waitCmd(ch chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

// updateRun handles the run and identity messages. handled is false for
// other messages.
func (m Model) updateRun(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case identityCheckedMsg:
		if msg.gen != m.run.gen || m.stage != StageRun {
			return m, nil, true
		}
		if errors.Is(msg.err, setup.ErrNoIdentity) {
			return m.showIdentity(""), nil, true
		}
		// Other failures surface from the run itself.
		m, cmd := m.plan()
		return m, cmd, true

	case identitySetMsg:
		m.run.settingIdentity = false
		m.focus()
		if msg.err != nil {
			m.run.identityErr = "Couldn't save it: " + msg.err.Error()
			return m, nil, true
		}
		m.run.gen++
		m.run.steps, m.run.current, m.run.failed, m.run.runErr = nil, -1, -1, nil
		m, cmd := m.plan()
		return m, cmd, true

	case planMsg:
		if msg.gen != m.run.gen || m.stage != StageRun {
			return m, nil, true
		}
		if msg.err != nil {
			m.run.phase, m.run.runErr = phaseFailed, msg.err
			return m, nil, true
		}
		m.run.phase, m.run.steps, m.run.current = phaseRunning, msg.steps, -1
		return m, m.execCmd(msg.gen, msg.req, msg.steps), true

	case progressMsg:
		if msg.gen == m.run.gen && m.stage == StageRun {
			m.run.current = msg.i
		}
		return m, waitCmd(msg.ch), true

	case runDoneMsg:
		if msg.gen != m.run.gen || m.stage != StageRun {
			return m, nil, true
		}
		if msg.err != nil {
			if errors.Is(msg.err, setup.ErrNoIdentity) {
				return m.showIdentity(""), nil, true
			}
			m.run.phase, m.run.runErr = phaseFailed, msg.err
			var se *setup.StepError
			if errors.As(msg.err, &se) {
				m.run.failed = se.Index
			}
			return m, nil, true
		}
		m.run.phase, m.run.conflicted, m.run.current = phaseDone, msg.conflicted, len(m.run.steps)
		m, cmd := m.finishRun()
		return m, cmd, true
	}
	return m, nil, false
}

// finishRun moves on after a successful run: the theme step on first run,
// DoneMsg in SetupSync mode.
func (m Model) finishRun() (Model, tea.Cmd) {
	if m.mode == FirstRun {
		return m.enterTheme()
	}
	return m, emit(m.done(m.currentTheme))
}

func (m Model) done(themeName string) DoneMsg {
	return DoneMsg{Vault: m.vaultPath(), Theme: themeName, Conflicted: m.run.conflicted, Choice: m.choice}
}

func (m Model) showIdentity(errText string) Model {
	m.stage = StageIdentity
	m.run.identityField = 0
	if strings.TrimSpace(m.run.nameInput.Value()) != "" {
		m.run.identityField = 1
	}
	m.run.identityErr = errText
	m.focus()
	return m
}

func (m Model) runKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.run.phase != phaseFailed {
		return m, nil // nothing to do while running
	}
	switch msg.String() {
	case "r":
		return m.startSetup()
	case "esc":
		m.stage = StageSync
		m.sub = subList
		m.focus()
	}
	return m, nil
}

func (m Model) identityKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.run.settingIdentity {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.stage = StageSync
		m.focus()
		return m, nil
	case "tab", "down", "ctrl+j":
		m.run.identityField = 1
		m.focus()
		return m, nil
	case "shift+tab", "up", "ctrl+k":
		m.run.identityField = 0
		m.focus()
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.run.nameInput.Value())
		email := strings.TrimSpace(m.run.emailInput.Value())
		if name == "" {
			m.run.identityField, m.run.identityErr = 0, "Enter your name."
			m.focus()
			return m, nil
		}
		if m.run.identityField == 0 {
			m.run.identityField = 1
			m.focus()
			return m, nil
		}
		if email == "" {
			m.run.identityErr = "Enter your email."
			return m, nil
		}
		set := m.env.SetIdentity
		if set == nil {
			m.run.identityErr = "Can't set the git identity here; run git config --global user.name and user.email."
			return m, nil
		}
		m.run.settingIdentity, m.run.identityErr = true, ""
		m.focus()
		cmd := func() tea.Msg { return identitySetMsg{err: set(context.Background(), name, email)} }
		return m, tea.Batch(cmd, m.spin())
	}
	return m.updateInput(msg)
}

// runErrorText returns a friendly sentence for a known failure, or "".
func runErrorText(err error) string {
	switch {
	case errors.Is(err, gitsync.ErrAuth):
		return textAuth
	case errors.Is(err, gitsync.ErrNetwork):
		return textNetwork
	case errors.Is(err, setup.ErrRemoteExists):
		return "This vault already has an origin remote with a different URL."
	case errors.Is(err, setup.ErrGHUnavailable):
		return "gh isn't installed or logged in."
	}
	return ""
}

// runErrorLines describes a failed run: the failed step and its message, a
// friendly hint when the error is recognized, and the raw error.
func runErrorLines(err error) (headline, hint, detail string) {
	var se *setup.StepError
	if errors.As(err, &se) {
		headline = fmt.Sprintf("Couldn't finish: %s", se.Step.Desc)
		detail = se.Err.Error()
	} else {
		headline = "Setup failed"
		detail = err.Error()
	}
	return headline, runErrorText(err), detail
}
