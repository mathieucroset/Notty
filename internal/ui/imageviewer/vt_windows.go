//go:build windows

package imageviewer

import (
	"io"

	"golang.org/x/sys/windows"
)

// enableVT turns on escape sequence processing for a console output handle.
// Bubble Tea restores the original console mode when it releases the
// terminal for tea.Exec, which can turn it off.
func enableVT(w io.Writer) (restore func()) {
	f, ok := w.(fder)
	if !ok {
		return func() {}
	}
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return func() {}
	}
	if err := windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN); err != nil {
		return func() {}
	}
	return func() { _ = windows.SetConsoleMode(h, mode) }
}
