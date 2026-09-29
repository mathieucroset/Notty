// Package wizard implements Notty's first-run setup wizard and the palette's
// "Set up sync" flow (spec §4.6, plan Task 33): it collects the vault folder,
// the sync choice and the theme, runs the git steps from package setup with a
// progress list, and reports the result to the app with DoneMsg.
//
// The wizard never touches the config, the lock or the vault itself: the app
// persists DoneMsg's vault and theme and opens the vault (plan amendment A1).
// All git and filesystem access goes through Env so tests can fake it.
//
// Keys (§4.4): text inputs receive every printable key, including q; enter
// confirms; esc goes back one step; esc on the first step asks "Quit Notty?"
// on first run, or cancels in SetupSync mode. ctrl+q is handled by the app.
package wizard

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// Mode says how the wizard was opened.
type Mode int

const (
	// FirstRun: no config or no vault repo yet. Vault, sync and theme steps.
	FirstRun Mode = iota
	// SetupSync: opened from the palette's "Set up sync". The vault is fixed,
	// the wizard starts at the sync step and has no theme step.
	SetupSync
)

// Env is everything the wizard needs from the outside world. DefaultEnv
// returns the real implementation; tests inject fakes.
type Env struct {
	// GH reports whether the GitHub option is offered. nil means unavailable.
	GH setup.GH
	// Inspect reports the state of the vault folder (setup.InspectVault).
	Inspect func(path string) (setup.VaultState, error)
	// InspectRemote runs git ls-remote on the URL (setup.InspectRemote).
	InspectRemote func(ctx context.Context, url string) (setup.RemoteState, error)
	// CheckIdentity returns an error wrapping setup.ErrNoIdentity when git has
	// no commit identity (setup.CheckIdentity). nil skips the check.
	CheckIdentity func(ctx context.Context, dir string) error
	// SetIdentity sets the global git identity (setup.SetIdentity).
	SetIdentity func(ctx context.Context, name, email string) error
	// Run executes the planned steps, calling progress before each one; it
	// may call progress from another goroutine. On first run it wraps
	// setup.Execute; in SetupSync mode the app supplies a runner that goes
	// through syncer.RunSetup(setup.Job(...)) (plan amendment A2).
	Run func(ctx context.Context, req setup.Request, steps []setup.Step, progress func(i int, s setup.Step)) (conflicted bool, err error)
	// CountNotes counts the notes in a folder that has files but no repo,
	// for the vault hint. nil uses a filesystem walk counting .md files.
	CountNotes func(path string) int
	// Host is the short hostname used in commit messages ("" for this
	// machine's).
	Host string
}

// DefaultEnv returns an Env backed by package setup and the real gh binary.
// Its Run executes the steps directly (first run); SetupSync callers replace
// Run with one that goes through the syncer.
func DefaultEnv(host string) Env {
	gh := setup.DefaultGH()
	return Env{
		GH:            gh,
		Inspect:       setup.InspectVault,
		InspectRemote: setup.InspectRemote,
		CheckIdentity: setup.CheckIdentity,
		SetIdentity:   setup.SetIdentity,
		Run: func(ctx context.Context, req setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
			return setup.Execute(ctx, req, steps, gh, progress)
		},
		CountNotes: countNotes,
		Host:       host,
	}
}

// DoneMsg reports a finished setup. Vault is the folder as the user typed it
// (it may start with "~"; expand it with config.ExpandHome). The app persists
// Vault and Theme, acquires the lock, opens the vault and starts the syncer,
// which enters Conflict when Conflicted is set.
type DoneMsg struct {
	Vault      string
	Theme      string
	Conflicted bool
	Choice     setup.Choice
}

// QuitMsg reports that the user confirmed "Quit Notty?" on first run.
type QuitMsg struct{}

// CancelMsg reports esc on the first shown step in SetupSync mode.
type CancelMsg struct{}

// ThemePreviewMsg asks the app to re-theme while the user browses themes.
type ThemePreviewMsg struct{ Name string }

// Stage is the screen the wizard is showing.
type Stage int

const (
	StageVault Stage = iota
	StageSync
	StageIdentity
	StageRun
	StageTheme
)

func (s Stage) String() string {
	switch s {
	case StageVault:
		return "Vault"
	case StageSync:
		return "Sync"
	case StageIdentity:
		return "Identity"
	case StageRun:
		return "Run"
	case StageTheme:
		return "Theme"
	}
	return "Stage(?)"
}

// syncSub is what has focus on the sync step.
type syncSub int

const (
	subList syncSub = iota
	subRepo
	subURL
)

// inspectDelay debounces vault inspection while typing.
var inspectDelay = 250 * time.Millisecond

// Hint and error texts.
const (
	textAuth      = "Couldn't access the repo. Check `ssh -T git@github.com` or `gh auth status`."
	textNetwork   = "Couldn't reach the remote."
	textGHMissing = "install and log in to gh to enable"
)

type vaultHint struct {
	path  string // expanded path the hint is about
	state setup.VaultState
	notes int
	err   error
	ok    bool // a result is present
}

type remoteCheck struct {
	url   string
	state setup.RemoteState
	err   error
	done  bool
	busy  bool
}

// Internal messages.
type (
	ghMsg          struct{ ok bool }
	vaultTickMsg   struct{ seq int }
	vaultResultMsg struct {
		seq   int
		path  string
		state setup.VaultState
		notes int
		err   error
	}
	remoteResultMsg struct {
		seq   int
		url   string
		state setup.RemoteState
		err   error
	}
)

// Model is the wizard. It is a value type: Update returns the new model.
type Model struct {
	mode   Mode
	env    Env
	styles theme.Styles
	w, h   int
	stage  Stage

	defaultVault string
	currentTheme string

	// Vault step.
	vaultInput  textinput.Model
	vaultErr    string
	confirmQuit bool
	inspectSeq  int
	hint        vaultHint

	// Sync step.
	ghKnown, ghOK bool
	choice        setup.Choice
	choiceMoved   bool // the user moved the cursor (a late gh result keeps it)
	sub           syncSub
	repoInput     textinput.Model
	repoErr       string
	urlInput      textinput.Model
	urlErr        string
	remoteSeq     int
	remote        remoteCheck
	cancelRemote  context.CancelFunc

	// Identity form and setup run.
	run runState

	spinner  spinner.Model
	spinning bool
}

// New builds a wizard. defaultVault prefills the vault step (FirstRun) or is
// the fixed vault (SetupSync); currentTheme is marked in the theme list.
func New(mode Mode, defaultVault string, currentTheme string, env Env, styles theme.Styles) Model {
	if env.CountNotes == nil {
		env.CountNotes = countNotes
	}
	m := Model{
		mode:         mode,
		env:          env,
		styles:       styles,
		defaultVault: defaultVault,
		currentTheme: currentTheme,
		choice:       setup.ExistingURL,
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
	m.vaultInput = newInput("~/Notes")
	m.vaultInput.SetValue(defaultVault)
	m.vaultInput.CursorEnd()
	m.run = newRunState()
	m.repoInput = newInput("notes")
	m.repoInput.SetValue("notes")
	m.repoInput.CursorEnd()
	m.urlInput = newInput("git@github.com:you/notes.git")
	m.applyStyles()
	if mode == SetupSync {
		m.stage = StageSync
	}
	m.focus()
	return m
}

func newInput(placeholder string) textinput.Model {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = placeholder
	return in
}

// SetStyles re-styles the wizard (the app calls it after a theme change).
func (m Model) SetStyles(s theme.Styles) Model {
	m.styles = s
	m.applyStyles()
	return m
}

func (m *Model) applyStyles() {
	st := textinput.Styles{}
	st.Focused.Prompt = m.styles.Accent
	st.Focused.Text = m.styles.StatusText
	st.Focused.Placeholder = m.styles.Muted
	st.Focused.Suggestion = m.styles.Muted
	st.Blurred = st.Focused
	st.Blurred.Prompt = m.styles.Muted
	st.Cursor.Color = m.styles.Accent.GetForeground()
	st.Cursor.Blink = false
	for _, in := range m.inputs() {
		in.SetStyles(st)
	}
	m.spinner.Style = m.styles.Accent
}

func (m *Model) inputs() []*textinput.Model {
	return []*textinput.Model{&m.vaultInput, &m.repoInput, &m.urlInput, &m.run.nameInput, &m.run.emailInput}
}

// activeInput returns the text input that has focus, or nil.
func (m *Model) activeInput() *textinput.Model {
	switch m.stage {
	case StageVault:
		if m.confirmQuit {
			return nil
		}
		return &m.vaultInput
	case StageSync:
		switch m.sub {
		case subRepo:
			return &m.repoInput
		case subURL:
			return &m.urlInput
		}
	case StageIdentity:
		if m.run.settingIdentity {
			return nil
		}
		if m.run.identityField == 1 {
			return &m.run.emailInput
		}
		return &m.run.nameInput
	}
	return nil
}

// focus focuses the active input and blurs the others.
func (m *Model) focus() {
	active := m.activeInput()
	for _, in := range m.inputs() {
		if in == active {
			in.Focus()
		} else {
			in.Blur()
		}
	}
}

// SetSize sets the screen size the wizard fills.
func (m Model) SetSize(w, h int) Model {
	m.w, m.h = w, h
	width := max(8, min(cardMaxWidth, w)-cardChrome-3)
	for _, in := range m.inputs() {
		in.SetWidth(width)
	}
	return m
}

// Stage returns the current screen.
func (m Model) Stage() Stage { return m.stage }

// VaultInput returns the vault path as typed.
func (m Model) VaultInput() string { return m.vaultInput.Value() }

// Choice returns the highlighted sync choice.
func (m Model) Choice() setup.Choice { return m.choice }

// KeyContext returns the §4.4 key context: WizardInput while a text input
// has focus, Wizard otherwise.
func (m Model) KeyContext() keys.Context {
	if m.activeInput() != nil {
		return keys.WizardInput
	}
	return keys.Wizard
}

// vaultPath returns the typed vault path, trimmed.
func (m Model) vaultPath() string {
	if m.mode == SetupSync {
		return m.defaultVault
	}
	return strings.TrimSpace(m.vaultInput.Value())
}

// expandedVault is the vault path with "~" expanded, for git and Inspect.
func (m Model) expandedVault() string { return config.ExpandHome(m.vaultPath()) }

// Init checks gh availability in the background and inspects the prefilled
// vault folder.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.ghCmd()}
	if m.mode == FirstRun {
		cmds = append(cmds, m.inspectCmd(m.inspectSeq, m.expandedVault()))
	}
	return tea.Batch(cmds...)
}

func (m Model) ghCmd() tea.Cmd {
	gh := m.env.GH
	if gh == nil {
		return func() tea.Msg { return ghMsg{ok: false} }
	}
	return func() tea.Msg { return ghMsg{ok: gh.Available(context.Background())} }
}

func (m Model) inspectCmd(seq int, path string) tea.Cmd {
	inspect, count := m.env.Inspect, m.env.CountNotes
	if inspect == nil || path == "" {
		return nil
	}
	return func() tea.Msg {
		st, err := inspect(path)
		n := 0
		if err == nil && st == setup.FilesNoRepo {
			n = count(path)
		}
		return vaultResultMsg{seq: seq, path: path, state: st, notes: n, err: err}
	}
}

func (m Model) remoteCmd(ctx context.Context, seq int, url string) tea.Cmd {
	inspect := m.env.InspectRemote
	return func() tea.Msg {
		if inspect == nil {
			return remoteResultMsg{seq: seq, url: url, err: errors.New("remote inspection unavailable")}
		}
		st, err := inspect(ctx, url)
		return remoteResultMsg{seq: seq, url: url, state: st, err: err}
	}
}

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// busy reports whether something is running that shows the spinner.
func (m Model) busy() bool {
	switch {
	case m.remote.busy, m.run.settingIdentity:
		return true
	case m.stage == StageRun:
		return m.run.phase == phaseChecking || m.run.phase == phasePreparing || m.run.phase == phaseRunning
	}
	return false
}

// spin starts the spinner loop unless it is already running.
func (m *Model) spin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return m.spinner.Tick
}

// Update handles a message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if m, cmd, ok := m.updateRun(msg); ok {
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		return m.updateInput(msg)

	case spinner.TickMsg:
		if !m.busy() {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case ghMsg:
		m.ghKnown, m.ghOK = true, msg.ok
		if msg.ok {
			if !m.choiceMoved {
				m.choice = setup.CreateGitHub
			}
		} else if m.choice == setup.CreateGitHub {
			m.choice = setup.ExistingURL
			if m.sub == subRepo {
				m.sub = subList
				m.focus()
			}
		}
		return m, nil

	case vaultTickMsg:
		if msg.seq != m.inspectSeq {
			return m, nil
		}
		return m, m.inspectCmd(msg.seq, m.expandedVault())

	case vaultResultMsg:
		if msg.seq != m.inspectSeq {
			return m, nil
		}
		m.hint = vaultHint{path: msg.path, state: msg.state, notes: msg.notes, err: msg.err, ok: true}
		return m, nil

	case remoteResultMsg:
		if msg.seq != m.remoteSeq {
			return m, nil
		}
		m.remote = remoteCheck{url: msg.url, state: msg.state, err: msg.err, done: true}
		m.cancelRemote = nil
		return m, nil
	}
	return m, nil
}

// updateInput forwards msg to the focused text input and reacts to edits.
func (m Model) updateInput(msg tea.Msg) (Model, tea.Cmd) {
	in := m.activeInput()
	if in == nil {
		return m, nil
	}
	before := in.Value()
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	if in.Value() == before {
		return m, cmd
	}
	switch in {
	case &m.vaultInput:
		m.vaultErr = ""
		m.inspectSeq++
		seq := m.inspectSeq
		cmd = tea.Batch(cmd, tea.Tick(inspectDelay, func(time.Time) tea.Msg { return vaultTickMsg{seq: seq} }))
	case &m.repoInput:
		m.repoErr = ""
	case &m.urlInput:
		m.urlErr = ""
		m.resetRemote()
	}
	return m, cmd
}

// resetRemote forgets the remote check and cancels one in flight.
func (m *Model) resetRemote() {
	if m.cancelRemote != nil {
		m.cancelRemote()
		m.cancelRemote = nil
	}
	m.remoteSeq++
	m.remote = remoteCheck{}
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch m.stage {
	case StageVault:
		return m.vaultKey(msg)
	case StageSync:
		return m.syncKey(msg)
	case StageIdentity:
		return m.identityKey(msg)
	case StageRun:
		return m.runKey(msg)
	}
	return m, nil
}

func (m Model) vaultKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.confirmQuit {
		switch msg.String() {
		case "y", "Y":
			m.confirmQuit = false
			m.focus()
			return m, emit(QuitMsg{})
		case "n", "N", "esc":
			m.confirmQuit = false
			m.focus()
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.confirmQuit = true
		m.focus()
		return m, nil
	case "enter":
		if m.vaultPath() == "" {
			m.vaultErr = "Enter a folder for your notes."
			return m, nil
		}
		if m.hint.ok && m.hint.err != nil && m.hint.path == m.expandedVault() {
			return m, nil
		}
		m.stage = StageSync
		m.sub = subList
		m.focus()
		return m, nil
	}
	return m.updateInput(msg)
}

func (m Model) choiceEnabled(c setup.Choice) bool {
	return c != setup.CreateGitHub || m.ghOK
}

func (m Model) syncKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	k := msg.String()
	if m.sub != subList {
		switch k {
		case "esc":
			if m.sub == subURL {
				m.resetRemote()
			}
			m.sub = subList
			m.focus()
			return m, nil
		case "enter":
			if m.sub == subRepo {
				return m.confirmRepo()
			}
			return m.confirmURL()
		}
		return m.updateInput(msg)
	}

	switch k {
	case "esc":
		if m.mode == SetupSync {
			return m, emit(CancelMsg{})
		}
		m.stage = StageVault
		m.focus()
		return m, nil
	case "up", "k", "ctrl+k", "shift+tab":
		m.choiceMoved = true
		for c := m.choice - 1; c >= setup.CreateGitHub; c-- {
			if m.choiceEnabled(c) {
				m.choice = c
				break
			}
		}
	case "down", "j", "ctrl+j", "tab":
		m.choiceMoved = true
		if m.choice < setup.LocalOnly {
			m.choice++
		}
	case "enter":
		switch m.choice {
		case setup.CreateGitHub:
			if !m.ghOK {
				return m, nil
			}
			m.sub = subRepo
		case setup.ExistingURL:
			m.sub = subURL
		case setup.LocalOnly:
			return m.startSetup()
		}
		m.focus()
	}
	return m, nil
}

func (m Model) confirmRepo() (Model, tea.Cmd) {
	name := strings.TrimSpace(m.repoInput.Value())
	// Reuse setup's validation of the name.
	_, err := setup.Plan(setup.Request{Vault: "x", Choice: setup.CreateGitHub, RepoName: name, GHAvailable: true}, setup.Missing, setup.RemoteState{})
	if err != nil {
		if name == "" {
			m.repoErr = "Enter a name for the repo."
		} else {
			m.repoErr = "Use letters, numbers, - _ and . (or owner/name)."
		}
		return m, nil
	}
	return m.startSetup()
}

func (m Model) confirmURL() (Model, tea.Cmd) {
	url := strings.TrimSpace(m.urlInput.Value())
	switch {
	case url == "":
		m.urlErr = "Enter a repository URL."
		return m, nil
	case m.remote.busy:
		return m, nil
	case m.remote.done && m.remote.err == nil && m.remote.url == url:
		return m.startSetup()
	}
	m.resetRemote()
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelRemote = cancel
	m.remote = remoteCheck{url: url, busy: true}
	return m, tea.Batch(m.remoteCmd(ctx, m.remoteSeq, url), m.spin())
}

// enterTheme follows a successful first-run setup.
func (m Model) enterTheme() (Model, tea.Cmd) {
	return m, emit(m.done(m.currentTheme))
}

// remoteError maps a remote failure to the text shown to the user.
func remoteError(err error) string {
	switch {
	case errors.Is(err, gitsync.ErrAuth):
		return textAuth
	case errors.Is(err, gitsync.ErrNetwork):
		return textNetwork
	}
	return "Couldn't check the remote: " + err.Error()
}
