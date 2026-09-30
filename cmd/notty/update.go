package main

import (
	"os"
	"path/filepath"

	"github.com/mathieucroset/notty/internal/ui/app"
)

// updateCheckOptions fills the app's release check options: disabled by
// NOTTY_NO_UPDATE_CHECK=1, its state kept in stateDir, and the install
// paths the upgrade hint is worked out from.
func updateCheckOptions(stateDir string) app.UpdateCheckOptions {
	uc := app.UpdateCheckOptions{EnvDisabled: os.Getenv("NOTTY_NO_UPDATE_CHECK") == "1"}
	if stateDir != "" {
		uc.StatePath = filepath.Join(stateDir, "update-check.json")
	}
	if exe, err := os.Executable(); err == nil {
		uc.Exe = resolvePath(exe)
	}
	home, _ := os.UserHomeDir()
	if dir := goBinDir(os.Getenv("GOBIN"), os.Getenv("GOPATH"), home); dir != "" {
		uc.GoBin = resolvePath(dir)
	}
	return uc
}

// goBinDir is where go install puts binaries: $GOBIN, else the first
// $GOPATH entry's bin, else ~/go/bin; "" when none is known.
func goBinDir(gobin, gopath, home string) string {
	if gobin != "" {
		return gobin
	}
	for _, p := range filepath.SplitList(gopath) {
		if p != "" {
			return filepath.Join(p, "bin")
		}
	}
	if home != "" {
		return filepath.Join(home, "go", "bin")
	}
	return ""
}

// resolvePath resolves p's symlinks, falling back to the cleaned path when
// it cannot (it does not exist, say).
func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
