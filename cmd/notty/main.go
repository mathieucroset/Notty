// Command notty is a terminal note-taking app with GitHub sync.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/app"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/vault"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const fallbackTheme = "catppuccin-mocha"

// env holds everything run touches outside its arguments, so startup can
// be tested without a terminal.
type env struct {
	stdout, stderr io.Writer
	configPath     string
	stateDir       string
	lookPath       func(name string) (string, error)
	runTUI         func(app.Options) error
}

func main() {
	os.Exit(run(os.Args[1:], env{
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		configPath: config.ConfigPath(),
		stateDir:   config.StateDir(),
		lookPath:   exec.LookPath,
		runTUI: func(opts app.Options) error {
			_, err := tea.NewProgram(app.New(opts)).Run()
			return err
		},
	}))
}

// run parses args, prepares the app (spec §11, plan amendment A1), runs the
// TUI, and returns the process exit code.
func run(args []string, e env) int {
	flags := flag.NewFlagSet("notty", flag.ContinueOnError)
	flags.SetOutput(e.stderr)
	vaultFlag := flags.String("vault", "", "vault `path` (overrides the configured vault)")
	showVersion := flags.Bool("version", false, "print the version and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(e.stderr, "notty: unknown argument %q\n", flags.Arg(0))
		return 2
	}
	if *showVersion {
		fmt.Fprintln(e.stdout, "notty "+version)
		return 0
	}

	opts, release, err := prepare(*vaultFlag, e)
	if err != nil {
		fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	defer release()

	if err := e.runTUI(opts); err != nil {
		fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	return 0
}

// prepare loads the configuration and, unless the first-run wizard is
// needed, takes the vault lock and opens the vault. The returned release
// func frees the lock and is always safe to call.
func prepare(vaultFlag string, e env) (app.Options, func(), error) {
	noop := func() {}

	// 1. Config: the local file first, to find the vault, then again with
	// the vault's synced settings layered underneath.
	cfg, err := config.Load(e.configPath, "")
	if err != nil {
		return app.Options{}, noop, err
	}
	root := cfg.VaultPath()
	if vaultFlag != "" {
		root = config.ExpandHome(vaultFlag)
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	wizard := wizardNeeded(e.configPath, root, e.lookPath)
	if !wizard {
		if cfg, err = config.Load(e.configPath, root); err != nil {
			return app.Options{}, noop, err
		}
	}
	cfg.Vault = root

	// 2. TODO(Task 21): detect image capabilities here (amendment A5).

	p, ok := theme.Get(cfg.Theme)
	if !ok {
		p, _ = theme.Get(fallbackTheme)
	}
	opts := app.Options{
		Config:       cfg,
		Styles:       theme.NewStyles(p),
		Palette:      p,
		WizardNeeded: wizard,
	}

	// 3-4. The wizard runs without the lock and without opening the vault;
	// it takes both itself when it finishes (Task 33).
	if wizard {
		return opts, noop, nil
	}

	// 5. Lock, open, and load per-vault state.
	release, err := acquireLock(root)
	if err != nil {
		return app.Options{}, noop, err
	}
	v, err := vault.Open(root)
	if err != nil {
		release()
		return app.Options{}, noop, err
	}
	opts.Vault = v
	opts.LocalPath = localstate.PathFor(e.stateDir, v.Root)
	if opts.Local, err = localstate.Load(opts.LocalPath); err != nil {
		// Per-machine state is a convenience: start fresh rather than fail.
		fmt.Fprintf(e.stderr, "notty: ignoring local state: %v\n", err)
		opts.Local = &localstate.State{Recents: []string{}, Cursor: map[string][2]int{}, Expanded: []string{}}
	}
	if opts.Pins, err = meta.Load(v.Root); err != nil {
		fmt.Fprintf(e.stderr, "notty: ignoring pins: %v\n", err)
		opts.Pins = &meta.State{Pins: []string{}}
	}
	return opts, release, nil
}

// wizardNeeded reports whether the first-run wizard must run (spec §4.6,
// amendment A1): git is installed, and either there is no config file or
// the vault is not a git repository.
func wizardNeeded(configPath, root string, lookPath func(string) (string, error)) bool {
	if _, err := lookPath("git"); err != nil {
		return false
	}
	if _, err := os.Stat(configPath); errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return true
	}
	return false
}

// acquireLock takes the vault instance lock (spec §9).
// TODO(Task 13): use vault.AcquireLock once it lands on main.
func acquireLock(root string) (release func(), err error) {
	_ = root
	return func() {}, nil
}
