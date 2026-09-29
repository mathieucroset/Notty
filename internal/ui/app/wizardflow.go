package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/preview"
	"github.com/mathieucroset/notty/internal/ui/tasksview"
	"github.com/mathieucroset/notty/internal/ui/wizard"
	"github.com/mathieucroset/notty/internal/vault"
	"github.com/mathieucroset/notty/internal/watcher"
)

// defaultLockWait is how long opening the vault after the wizard waits for
// a lock held by another process (spec §9).
const defaultLockWait = 10 * time.Second

// vaultOpenedMsg carries the vault opened once the first-run wizard is done
// (plan amendment A1), or the error that stopped it.
type vaultOpenedMsg struct {
	vault     *vault.Vault
	cfg       config.Config
	local     *localstate.State
	localPath string
	pins      *meta.State
	watcher   *watcher.Watcher
	release   func()
	warnings  []string
	err       error
}

// wizardEnv is the environment the wizard runs its checks and steps in.
func (m *Model) wizardEnv() wizard.Env {
	if m.opts.WizardEnv != nil {
		return *m.opts.WizardEnv
	}
	return wizard.DefaultEnv("")
}

// homeRelative shows p with the home directory as "~", like the wizard's
// placeholder.
func homeRelative(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~/" + filepath.ToSlash(rest)
	}
	return p
}

// newFirstRunWizard builds the first-run wizard (spec §4.6).
func (m *Model) newFirstRunWizard() *wizard.Model {
	def := m.opts.Config.Vault
	if def == "" {
		def = m.opts.Config.VaultPath()
	}
	w := wizard.New(wizard.FirstRun, homeRelative(def), m.opts.Palette, m.opts.Catalog, m.wizardEnv(), m.opts.Styles)
	return &w
}

// updateWizard forwards a message to the open wizard.
func (m *Model) updateWizard(msg tea.Msg) tea.Cmd {
	w, cmd := m.wizard.Update(msg)
	m.wizard = &w
	return cmd
}

// updateWizardMsg handles the wizard's messages.
func (m *Model) updateWizardMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case wizard.ThemePreviewMsg:
		m.applyPalette(msg.Palette)
		if m.wizard != nil {
			w := m.wizard.SetStyles(m.opts.Styles)
			m.wizard = &w
		}
	case wizard.DoneMsg:
		return m.handleWizardDone(msg), true
	case wizard.QuitMsg:
		return m.quit(), true
	case wizard.CancelMsg:
		m.wizard = nil
	case vaultOpenedMsg:
		return m.handleVaultOpened(msg), true
	default:
		return nil, false
	}
	return nil, true
}

// handleWizardDone finishes the wizard: on first run it saves the vault and
// theme and opens the vault; after "Set up sync" it returns to the notes.
func (m *Model) handleWizardDone(msg wizard.DoneMsg) tea.Cmd {
	if !m.opts.WizardNeeded {
		m.wizard = nil
		if msg.Conflicted {
			return m.pushToast(msgs.ToastWarn, "Sync is set up, but the notes conflict: press c to resolve")
		}
		return m.pushToast(msgs.ToastInfo, "Sync is set up")
	}
	root := config.ExpandHome(msg.Vault)
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	// The wizard resolved the theme (it never finishes on one that does
	// not load): apply it, then save its name.
	if msg.Palette.Name != "" {
		m.applyPalette(msg.Palette)
		m.themeName = msg.Palette.Name
	}
	wait := m.opts.LockWait
	if wait <= 0 {
		wait = defaultLockWait
	}
	stateDir := m.opts.StateDir
	if stateDir == "" {
		stateDir = config.StateDir()
	}
	return openVaultCmd(root, m.opts.Palette.Name, m.configPath(), stateDir, wait)
}

// openVaultCmd saves the wizard's choices, then takes the lock, opens the
// vault and loads its per-vault state, as main does without the wizard.
func openVaultCmd(root, themeName, cfgPath, stateDir string, wait time.Duration) tea.Cmd {
	return func() tea.Msg {
		fail := func(err error) tea.Msg { return vaultOpenedMsg{err: err} }
		if err := config.SetKey(cfgPath, "vault", root); err != nil {
			return fail(err)
		}
		if err := config.SetKey(cfgPath, "theme", themeName); err != nil {
			return fail(err)
		}
		lock, err := vault.AcquireLock(root, wait)
		if err != nil {
			return fail(err)
		}
		release := func() { _ = lock.Release() }
		v, err := vault.Open(root)
		if err != nil {
			release()
			return fail(err)
		}
		cfg, err := config.Load(cfgPath, root)
		if err != nil {
			release()
			return fail(err)
		}
		cfg.Vault = root
		msg := vaultOpenedMsg{vault: v, cfg: cfg, release: release}
		msg.localPath = localstate.PathFor(stateDir, v.Root)
		if msg.local, err = localstate.Load(msg.localPath); err != nil {
			msg.warnings = append(msg.warnings, fmt.Sprintf("Ignoring local state: %v", err))
			msg.local = &localstate.State{Recents: []string{}, Cursor: map[string][2]int{}, Expanded: []string{}}
		}
		if msg.pins, err = meta.Load(v.Root); err != nil {
			msg.warnings = append(msg.warnings, fmt.Sprintf("Ignoring pins: %v", err))
			msg.pins = &meta.State{Pins: []string{}}
		}
		if msg.watcher, err = watcher.New(v.Root); err != nil {
			msg.warnings = append(msg.warnings, fmt.Sprintf("Not watching the vault for changes: %v", err))
		}
		return msg
	}
}

// handleVaultOpened switches from the wizard to the main screen with the
// vault just opened, and starts everything a normal start runs: tree,
// index, trash purge, watcher and syncer (which enters Conflict when the
// setup's merge conflicted).
func (m *Model) handleVaultOpened(msg vaultOpenedMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not open the vault: %v", msg.err))
	}
	m.opts.Vault = msg.vault
	m.opts.Config = msg.cfg
	m.opts.Local, m.opts.LocalPath, m.opts.Pins = msg.local, msg.localPath, msg.pins
	m.opts.Watcher = msg.watcher
	m.host.watcher.Store(msg.watcher)
	m.releaseLock = msg.release
	m.opts.WizardNeeded = false
	m.wizard = nil

	m.editor = newEditor(m.opts, m.mapEditorMsg)
	m.preview = preview.New(m.opts.Styles, m.opts.Palette, m.opts.Caps, msg.vault.Root)
	m.tasks = tasksview.New(m.opts.Styles, msg.cfg.Tasks.DueSoonDays, msg.cfg.Tasks.ShowDone)
	m.sidebar.SetExpanded(m.opts.Local.Expanded)
	m.sidebar.SetPins(m.opts.Pins.Pins)
	m.attachSyncer()
	m.relayout()
	m.setFocus(FocusSidebar)

	cmds := []tea.Cmd{m.Init()}
	for _, w := range msg.warnings {
		cmds = append(cmds, m.pushToast(msgs.ToastWarn, w))
	}
	return tea.Batch(cmds...)
}

// setupSync opens the wizard at its sync step for a local-only vault
// (palette "Set up sync", plan amendment A2). Its git steps run on the
// syncer's queue through RunSetup.
func (m *Model) setupSync() tea.Cmd {
	switch {
	case m.syncSvc == nil && m.opts.GitMissing:
		return m.pushToast(msgs.ToastInfo, textGitMissing)
	case m.syncSvc == nil || m.repo == nil:
		return m.pushToast(msgs.ToastInfo, textNoSyncSetup)
	case m.repo.HasRemote():
		return m.pushToast(msgs.ToastInfo, textSyncIsSetUp)
	}
	env := m.wizardEnv()
	env.Run = setupRunner(m.syncSvc.RunSetup, env.GH, m.repo.Dir)
	w := wizard.New(wizard.SetupSync, m.repo.Dir, m.opts.Palette, m.opts.Catalog, env, m.opts.Styles).SetSize(m.width, m.height)
	m.wizard = &w
	return w.Init()
}

// setupRunner runs the wizard's steps as one job on the syncer's queue and
// reports whether they left a merge in progress (conflicts to resolve).
func setupRunner(runSetup func(func(*gitsync.Repo) error) <-chan error, gh setup.GH, dir string) func(
	ctx context.Context, req setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
	return func(ctx context.Context, _ setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
		errc := runSetup(setup.Job(ctx, steps, gh, progress))
		select {
		case err := <-errc:
			if err != nil {
				return false, err
			}
			return gitsync.Open(dir).MergeInProgress(), nil
		case <-ctx.Done():
			return false, fmt.Errorf("setup: %w", ctx.Err())
		}
	}
}
