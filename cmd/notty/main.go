// Command notty is a terminal note-taking app with GitHub sync.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/imgrender"
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
	// lockWait is how long to wait for a vault lock held by another
	// process (spec §9: 10s, covering a running headless sync).
	lockWait time.Duration
	// detectCaps works out the terminal's image capabilities for the
	// images.protocol setting (amendment A5).
	detectCaps func(protocol string) imgrender.Caps
	runTUI     func(app.Options) error
}

func main() {
	os.Exit(run(os.Args[1:], env{
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		configPath: config.ConfigPath(),
		stateDir:   config.StateDir(),
		lookPath:   exec.LookPath,
		lockWait:   10 * time.Second,
		detectCaps: detectTerminalCaps,
		runTUI:     runProgram,
	}))
}

// runProgram runs the Bubble Tea program.
func runProgram(opts app.Options) error {
	var progOpts []tea.ProgramOption
	if p, force := colorProfileFor(opts.Caps); force {
		progOpts = append(progOpts, tea.WithColorProfile(p))
	}
	_, err := tea.NewProgram(app.New(opts), progOpts...).Run()
	return err
}

// colorProfileFor returns the color profile the program must use for caps.
// Kitty placeholder cells carry the image ID in a truecolor foreground, so
// with inline Kitty images the program must render in TrueColor (kitty
// spike, condition 2); otherwise the detected profile is kept.
func colorProfileFor(caps imgrender.Caps) (colorprofile.Profile, bool) {
	if caps.Inline == imgrender.ProtoKitty {
		return colorprofile.TrueColor, true
	}
	return colorprofile.TrueColor, false
}

// detectTerminalCaps queries the controlling terminal for its image
// capabilities before Bubble Tea takes it over. The tty is put in raw mode
// for the queries and restored afterwards; without a terminal (or on
// Windows) only the configuration and environment are used.
func detectTerminalCaps(protocol string) imgrender.Caps {
	var tty io.ReadWriter
	if runtime.GOOS != "windows" {
		if f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
			defer f.Close()
			if restore, ok := makeRaw(f); ok {
				defer restore()
				tty = f
			}
		}
	}
	return imgrender.Detect(protocol, os.Getenv, tty, runWithTimeout(2*time.Second))
}

// makeRaw puts the terminal f in raw mode and returns a func restoring it.
// It reaches the descriptor through SyscallConn rather than f.Fd: Fd
// switches the file to blocking mode, after which read deadlines are
// silently ignored and Detect's bounded reads would block forever.
func makeRaw(f *os.File) (restore func(), ok bool) {
	rc, err := f.SyscallConn()
	if err != nil {
		return nil, false
	}
	var state *term.State
	var fd uintptr
	cerr := rc.Control(func(d uintptr) {
		fd = d
		if term.IsTerminal(d) {
			state, err = term.MakeRaw(d)
		}
	})
	if cerr != nil || err != nil || state == nil {
		return nil, false
	}
	return func() { _ = term.Restore(fd, state) }, true
}

// runWithTimeout returns a command runner whose commands are killed after d.
func runWithTimeout(d time.Duration) func(name string, args ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).Output()
	}
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

	// 2. Image capabilities, while the terminal is still ours to query.
	caps := e.detectCaps(cfg.Images.Protocol)

	p, ok := theme.Get(cfg.Theme)
	if !ok {
		p, _ = theme.Get(fallbackTheme)
	}
	opts := app.Options{
		Config:       cfg,
		Styles:       theme.NewStyles(p),
		Palette:      p,
		Caps:         caps,
		WizardNeeded: wizard,
	}

	// 3-4. The wizard runs without the lock and without opening the vault;
	// it takes both itself when it finishes (Task 33).
	if wizard {
		return opts, noop, nil
	}

	// 5. Lock, open, and load per-vault state.
	release, err := acquireLock(root, e.lockWait, e.stderr)
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

// acquireLock takes the vault instance lock (spec §9). When another
// process holds it, it says so on stderr and waits up to wait before
// giving up with vault.ErrLocked, whose message names the holder.
func acquireLock(root string, wait time.Duration, stderr io.Writer) (release func(), err error) {
	lock, err := vault.AcquireLock(root, 0)
	var held vault.ErrLocked
	if errors.As(err, &held) && wait > 0 {
		fmt.Fprintln(stderr, "notty: waiting for vault lock…")
		lock, err = vault.AcquireLock(root, wait)
	}
	if err != nil {
		return nil, err
	}
	return func() { _ = lock.Release() }, nil
}
