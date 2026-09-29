package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mathieucroset/notty/internal/config"
)

const usage = `usage:
  notty [--vault <path>]                          open the TUI
  notty [--vault <path>] new "<title>" [--folder <folder>]
                                                  create a note and open it
  notty [--vault <path>] sync                     run one sync cycle and exit
  notty --version                                 print the version
`

// errNeedsSetup means the vault is not set up yet: the first-run wizard of
// the TUI must run before a subcommand can use it.
var errNeedsSetup = errors.New("run notty first to set up your vault")

// runSubcommand runs the subcommand named by args[0] (spec §11). vaultFlag
// is the global --vault given before it.
func runSubcommand(args []string, vaultFlag string, e env) int {
	switch args[0] {
	case "new":
		return runNew(args[1:], vaultFlag, e)
	}
	_, _ = fmt.Fprintf(e.stderr, "notty: unknown command %q\n%s", args[0], usage)
	return 2
}

// parseInterspersed parses args with flags, allowing flags after positional
// arguments ("new Title --folder Work"), and returns the positional
// arguments. A bare "--" ends flag parsing.
func parseInterspersed(flags *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		rest := flags.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		// flag stops at "--" (consumed) or at the first non-flag argument.
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// subcommandFlags returns a flag set for a subcommand that also accepts
// --vault, defaulting to the global one.
func subcommandFlags(name, vaultFlag string, e env) (*flag.FlagSet, *string) {
	flags := flag.NewFlagSet("notty "+name, flag.ContinueOnError)
	flags.SetOutput(e.stderr)
	flags.Usage = func() { _, _ = fmt.Fprint(e.stderr, usage) }
	v := flags.String("vault", vaultFlag, "vault `path` (overrides the configured vault)")
	return flags, v
}

// flagExit maps a flag parsing error to an exit code.
func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

// resolveVault loads the local config and returns the absolute vault root:
// vaultFlag if set, else the configured vault. It returns errNeedsSetup
// when the vault does not exist or the first-run wizard would run.
func resolveVault(vaultFlag string, e env) (string, error) {
	cfg, err := config.Load(e.configPath, "")
	if err != nil {
		return "", err
	}
	root := cfg.VaultPath()
	if vaultFlag != "" {
		root = config.ExpandHome(vaultFlag)
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return "", errNeedsSetup
	}
	if wizardNeeded(e.configPath, root, e.lookPath) {
		return "", errNeedsSetup
	}
	return root, nil
}

// runNew is `notty new "<title>" [--folder X]`: it creates the note, then
// opens the TUI on it.
func runNew(args []string, vaultFlag string, e env) int {
	flags, vaultPath := subcommandFlags("new", vaultFlag, e)
	folder := flags.String("folder", "", "vault-relative `folder` to create the note in")
	pos, err := parseInterspersed(flags, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 1 || pos[0] == "" {
		_, _ = fmt.Fprintf(e.stderr, "notty new: expected one note title\n%s", usage)
		return 2
	}

	root, err := resolveVault(*vaultPath, e)
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	opts, release, err := prepare(root, e)
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	defer release()
	if opts.WizardNeeded || opts.Vault == nil {
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", errNeedsSetup)
		return 1
	}
	rel, err := opts.Vault.CreateNote(*folder, pos[0])
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	opts.InitialNote = rel
	if err := e.runTUI(opts); err != nil {
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	return 0
}
