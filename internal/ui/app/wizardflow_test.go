package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/palette"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/ui/wizard"
	"github.com/mathieucroset/notty/internal/vault"
)

// setIdentity gives the isolated git a global identity (call it after the
// last gittest.Isolate of the test, which resets the global config).
func setIdentity(t *testing.T) {
	t.Helper()
	gittest.Git(t, "", "config", "--global", "user.name", "Test User")
	gittest.Git(t, "", "config", "--global", "user.email", "test@example.com")
}

// testWizardEnv is the real setup without gh.
func testWizardEnv() *wizard.Env {
	return &wizard.Env{
		Inspect:       setup.InspectVault,
		InspectRemote: setup.InspectRemote,
		CheckIdentity: setup.CheckIdentity,
		SetIdentity:   setup.SetIdentity,
		Run: func(ctx context.Context, req setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
			return setup.Execute(ctx, req, steps, nil, progress)
		},
		Host: "laptop",
	}
}

// wizardOptions starts in first-run mode with the vault folder at
// <tmp>/Notes, not created yet.
func wizardOptions(t *testing.T) Options {
	t.Helper()
	opts := testOptions(t)
	dir := t.TempDir()
	opts.Vault, opts.Local, opts.LocalPath, opts.Pins = nil, nil, "", nil
	opts.WizardNeeded = true
	opts.Config.Vault = filepath.Join(dir, "Notes")
	opts.StateDir = filepath.Join(dir, "state")
	opts.LockWait = 100 * time.Millisecond
	opts.NewSyncer = DefaultSyncFactory
	opts.WizardEnv = testWizardEnv()
	return opts
}

// finishWizard waits for the main screen with the vault open.
func finishWizard(t *testing.T, m *Model) {
	t.Helper()
	waitFor(t, m, func() bool { return m.wizard == nil && m.opts.Vault != nil })
}

func TestWizardFirstRunLocalOnly(t *testing.T) {
	gittest.Isolate(t)
	setIdentity(t)
	opts := wizardOptions(t)
	root := opts.Config.Vault
	m := start(t, opts, 100, 30)
	t.Cleanup(m.Shutdown)
	if m.wizard == nil || m.keyContext() != m.wizard.KeyContext() {
		t.Fatal("wizard not shown")
	}
	if strings.Contains(screen(m), "◆ Notty ─") {
		t.Errorf("main screen drawn in wizard mode:\n%s", screen(m))
	}

	run(t, m, keyMsg("enter")) // vault folder
	run(t, m, keyMsg("j"))     // Existing URL → Local only
	run(t, m, keyMsg("enter"))
	waitFor(t, m, func() bool { return m.wizard != nil && m.wizard.Stage() == wizard.StageTheme })
	run(t, m, keyMsg("enter")) // theme
	finishWizard(t, m)

	if m.opts.Vault.Root != root {
		t.Errorf("vault root = %q, want %q", m.opts.Vault.Root, root)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Errorf("no repository created: %v", err)
	}
	var held vault.ErrLocked
	if _, err := vault.AcquireLock(root, 0); !errors.As(err, &held) {
		t.Errorf("vault lock not held after the wizard: %v", err)
	}
	cfg, err := config.Load(opts.ConfigPath, "")
	if err != nil || cfg.VaultPath() != root {
		t.Errorf("config vault = %q (%v), want %q", cfg.VaultPath(), err, root)
	}
	waitFor(t, m, func() bool { return m.sync.State == msgs.SyncLocalOnly })
	if s := screen(m); !strings.Contains(s, "○ local only") || !strings.Contains(s, "◆ Notty") {
		t.Errorf("main screen:\n%s", s)
	}
	m.Shutdown()
	if lock, err := vault.AcquireLock(root, 0); err != nil {
		t.Errorf("lock not released by Shutdown: %v", err)
	} else {
		_ = lock.Release()
	}
}

func TestWizardFirstRunExistingURLClones(t *testing.T) {
	env := gittest.New(t)
	setIdentity(t)
	opts := wizardOptions(t)
	m := start(t, opts, 100, 30)
	t.Cleanup(m.Shutdown)

	run(t, m, keyMsg("enter")) // vault folder
	run(t, m, keyMsg("enter")) // Existing URL
	typeText(t, m, env.Remote)
	run(t, m, keyMsg("enter")) // checks the remote
	waitFor(t, m, func() bool { return strings.Contains(screen(m), "has notes") || strings.Contains(screen(m), "istory") })
	run(t, m, keyMsg("enter")) // runs the setup
	waitFor(t, m, func() bool { return m.wizard != nil && m.wizard.Stage() == wizard.StageTheme })
	run(t, m, keyMsg("enter"))
	finishWizard(t, m)

	if _, err := os.Stat(filepath.Join(opts.Config.Vault, "README.md")); err != nil {
		t.Errorf("remote not cloned: %v", err)
	}
	waitFor(t, m, func() bool { return m.sync.State == msgs.SyncSynced })
	waitFor(t, m, func() bool { return strings.Contains(screen(m), "README") })
}

func TestWizardQuitAndCtrlQ(t *testing.T) {
	gittest.Isolate(t)
	m := start(t, wizardOptions(t), 100, 30)
	for _, k := range []string{"ctrl+g", "f1", "ctrl+b", "ctrl+k"} {
		run(t, m, keyMsg(k))
		if m.overlayOpen() || m.wizard == nil {
			t.Errorf("wizard: %s escaped the wizard", k)
		}
	}
	if !hasQuit(run(t, m, keyMsg("ctrl+q"))) {
		t.Error("wizard: ctrl+q did not quit")
	}
}

func TestWizardThemePreviewRethemes(t *testing.T) {
	gittest.Isolate(t)
	m := start(t, wizardOptions(t), 100, 30)
	nord, _ := theme.Get("nord")
	run(t, m, wizard.ThemePreviewMsg{Palette: nord})
	if m.opts.Palette.Name != "nord" {
		t.Errorf("palette = %q, want nord", m.opts.Palette.Name)
	}
}

func TestWizardDoneSavesUserTheme(t *testing.T) {
	gittest.Isolate(t)
	setIdentity(t)
	opts := wizardOptions(t)
	withUserThemes(t, &opts, map[string]string{"mine": userThemeFile})
	m := start(t, opts, 100, 30)
	t.Cleanup(m.Shutdown)

	run(t, m, keyMsg("enter")) // vault folder
	run(t, m, keyMsg("j"))     // Existing URL → Local only
	run(t, m, keyMsg("enter"))
	waitFor(t, m, func() bool { return m.wizard != nil && m.wizard.Stage() == wizard.StageTheme })
	names := opts.Catalog.Names()
	for range slices.Index(names, "mine") - slices.Index(names, "catppuccin-mocha") {
		run(t, m, downKey)
	}
	if m.opts.Palette.Name != "mine" {
		t.Fatalf("previewed palette = %q, want mine", m.opts.Palette.Name)
	}
	run(t, m, keyMsg("enter")) // theme
	finishWizard(t, m)

	if m.opts.Palette.Name != "mine" || m.themeName != "mine" {
		t.Errorf("palette / themeName = %q / %q, want mine", m.opts.Palette.Name, m.themeName)
	}
	b, err := os.ReadFile(opts.ConfigPath)
	if err != nil || !strings.Contains(string(b), `theme = "mine"`) {
		t.Errorf("config = %q (%v), want theme = \"mine\"", b, err)
	}
}

func TestSetupSyncFromLocalOnlyVault(t *testing.T) {
	remote := gittest.NewEmptyRemote(t)
	setIdentity(t)
	dir := filepath.Join(t.TempDir(), "vault")
	repo, err := gitsync.Init(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	gittest.Write(t, repo, "ideas.md", "# Ideas\n")
	gittest.CommitAll(t, repo, "Initial commit · laptop")

	opts := syncOptions(t, repo, fastClock{})
	opts.WizardEnv = testWizardEnv()
	m := startSync(t, opts, msgs.SyncLocalOnly)

	run(t, m, palette.SetupSyncMsg{})
	if m.wizard == nil || m.wizard.Stage() != wizard.StageSync {
		t.Fatalf("Set up sync did not open the wizard at the sync step; toasts %v", toastTexts(m))
	}
	run(t, m, keyMsg("enter")) // Existing URL
	typeText(t, m, remote)
	run(t, m, keyMsg("enter")) // checks the remote
	waitFor(t, m, func() bool { return strings.Contains(screen(m), "empty") || strings.Contains(screen(m), "Empty") })
	run(t, m, keyMsg("enter")) // runs the setup through the syncer
	waitFor(t, m, func() bool { return m.wizard == nil })
	if !hasToast(m, msgs.ToastInfo, "Sync is set up") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	waitFor(t, m, func() bool { return m.sync.State == msgs.SyncSynced })
	if log := gittest.Git(t, remote, "log", "--format=%s", "main"); !strings.Contains(log, "Initial commit") {
		t.Errorf("remote log = %q", log)
	}

	// With a remote, Set up sync refuses.
	run(t, m, palette.SetupSyncMsg{})
	if m.wizard != nil || !hasToast(m, msgs.ToastInfo, textSyncIsSetUp) {
		t.Errorf("Set up sync with a remote: wizard %v, toasts %v", m.wizard != nil, toastTexts(m))
	}
}

func TestSetupSyncCancel(t *testing.T) {
	gittest.Isolate(t)
	setIdentity(t)
	repo, err := gitsync.Init(filepath.Join(t.TempDir(), "vault"), "main")
	if err != nil {
		t.Fatal(err)
	}
	gittest.Write(t, repo, "a.md", "# A\n")
	gittest.CommitAll(t, repo, "Initial commit · laptop")
	opts := syncOptions(t, repo, fastClock{})
	opts.WizardEnv = testWizardEnv()
	m := startSync(t, opts, msgs.SyncLocalOnly)
	run(t, m, palette.SetupSyncMsg{})
	run(t, m, keyMsg("esc"))
	if m.wizard != nil {
		t.Error("esc did not cancel Set up sync")
	}
	if !strings.Contains(screen(m), "◆ Notty") {
		t.Errorf("not back on the main screen:\n%s", screen(m))
	}
}
