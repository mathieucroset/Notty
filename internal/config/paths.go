// Package config loads and persists Notty's layered TOML configuration
// (spec §10) and resolves the XDG-style directories it lives under.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ConfigDir returns the directory containing notty's local config file:
// $XDG_CONFIG_HOME/notty, or ~/.config/notty if unset ($APPDATA%\notty on
// Windows).
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("APPDATA"); v != "" {
			return filepath.Join(v, "notty")
		}
		return filepath.Join(ExpandHome("~"), "AppData", "Roaming", "notty")
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "notty")
	}
	return filepath.Join(ExpandHome("~"), ".config", "notty")
}

// StateDir returns the directory for notty's state (logs, recovery, lock):
// $XDG_STATE_HOME/notty, or ~/.local/state/notty if unset
// (%LOCALAPPDATA%\notty on Windows).
func StateDir() string {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "notty")
		}
		return filepath.Join(ExpandHome("~"), "AppData", "Local", "notty")
	}
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "notty")
	}
	return filepath.Join(ExpandHome("~"), ".local", "state", "notty")
}

// ConfigPath returns the path to notty's local config file.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.toml")
}

// ExpandHome expands a leading "~" or "~/" in p to the current user's home
// directory. Paths that don't start with "~" (or "~" followed immediately
// by "/") are returned unchanged.
func ExpandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
