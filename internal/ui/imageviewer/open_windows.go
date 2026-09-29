//go:build windows

package imageviewer

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// openWithSystem opens path with its default application through
// ShellExecute. No cmd.exe is involved, so characters such as & or ^ in the
// path are not interpreted.
func openWithSystem(path string) error {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	if err := windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(path), err)
	}
	return nil
}
