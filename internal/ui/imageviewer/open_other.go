//go:build !windows

package imageviewer

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

// openCommand returns the command that opens path with the default
// application on goos: open on macOS, xdg-open elsewhere. The path is a
// single argument (no shell), made absolute so it cannot look like a flag.
func openCommand(goos, path string) *exec.Cmd {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if goos == "darwin" {
		return exec.Command("open", path)
	}
	return exec.Command("xdg-open", path)
}

// openWithSystem opens path with the default application without waiting
// for it. The command's output is discarded so it cannot draw over the
// viewer.
func openWithSystem(path string) error {
	cmd := openCommand(runtime.GOOS, path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
