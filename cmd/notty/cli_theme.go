package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mathieucroset/notty/internal/ui/theme"
)

// runTheme runs `notty theme <sub>` (user themes spec §2). `theme matugen`
// writes the matugen template next to the config file, unless it already
// exists, and prints the matugen config snippet that renders it into
// <ConfigDir>/themes/matugen.toml.
func runTheme(args []string, e env) int {
	if len(args) != 1 || args[0] != "matugen" {
		_, _ = fmt.Fprintln(e.stderr, "usage: notty theme matugen")
		return 2
	}
	dir := filepath.Dir(e.configPath)
	tpl := filepath.Join(dir, "matugen-template.toml")
	out := filepath.Join(dir, "themes", "matugen.toml")
	switch _, err := os.Stat(tpl); {
	case err == nil:
		_, _ = fmt.Fprintf(e.stdout, "%s already exists, left unchanged.\n\n", tildePath(tpl))
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
			return 1
		}
		if err := os.WriteFile(tpl, []byte(theme.MatugenTemplate()), 0o600); err != nil {
			_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
			return 1
		}
		_, _ = fmt.Fprintf(e.stdout, "Wrote %s.\n\n", tildePath(tpl))
	default:
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(e.stdout, "Add this to your matugen config (Noctalia: user-templates.toml):\n\n"+
		"[templates.notty]\ninput_path  = %q\noutput_path = %q\n\n"+
		"Then set in %s:\n\ntheme = \"matugen\"\n",
		tildePath(tpl), tildePath(out), tildePath(e.configPath))
	return 0
}

// tildePath abbreviates the home directory at the start of p to ~.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	rel, err := filepath.Rel(home, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return filepath.ToSlash(filepath.Join("~", rel))
}
