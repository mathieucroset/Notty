// Package clipboard reads and writes the system clipboard without cgo.
//
// Text is handled by github.com/atotto/clipboard (the caller falls back to
// OSC52 when WriteText fails, e.g. over SSH with no clipboard tool). Images
// are read by shelling out to platform tools, since there is no pure-Go way
// to read image data from the system clipboard:
//
//   - Linux (Wayland): wl-paste
//   - Linux (X11):     xclip
//   - macOS:            osascript
//   - Windows:          powershell
package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	atotto "github.com/atotto/clipboard"
)

// ErrNoImage is returned by ReadImage when the clipboard holds no image data.
var ErrNoImage = errors.New("clipboard has no image")

// pngSignature is the 8-byte magic header of a PNG file.
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// toolInstallHints maps an external tool name to a human-readable package to
// install. Tools with no entry (osascript, powershell) are expected to be
// preinstalled on their OS, so the message omits the install hint.
var toolInstallHints = map[string]string{
	"wl-paste": "wl-clipboard",
	"xclip":    "xclip",
}

// ErrNoTool is returned by ReadImage when the external tool needed to read
// the clipboard on the current platform is not on PATH.
type ErrNoTool struct{ Tool string }

func (e ErrNoTool) Error() string {
	if hint, ok := toolInstallHints[e.Tool]; ok {
		return fmt.Sprintf("clipboard tool not found: %s (install %s)", e.Tool, hint)
	}
	return fmt.Sprintf("clipboard tool not found: %s", e.Tool)
}

// Clipboard reads and writes the system clipboard. All external
// dependencies are injected as fields so tests can fake them; Default
// wires up the real ones.
type Clipboard struct {
	LookPath func(string) (string, error)
	Run      func(name string, args ...string) ([]byte, error) // returns stdout
	GOOS     string
	Env      func(string) string
	TempDir  func() string
	ReadFile func(string) ([]byte, error)

	readText  func() (string, error) // atotto by default
	writeText func(string) error     // atotto by default
}

// Default returns a Clipboard wired to the real OS, exec, and atotto
// clipboard implementations.
func Default() *Clipboard {
	return &Clipboard{
		LookPath: exec.LookPath,
		Run: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).Output()
		},
		GOOS:      runtime.GOOS,
		Env:       os.Getenv,
		TempDir:   os.TempDir,
		ReadFile:  os.ReadFile,
		readText:  atotto.ReadAll,
		writeText: atotto.WriteAll,
	}
}

// ReadText returns the current text clipboard contents.
func (c *Clipboard) ReadText() (string, error) {
	s, err := c.readText()
	if err != nil {
		return "", fmt.Errorf("clipboard: read text: %w", err)
	}
	return s, nil
}

// WriteText sets the text clipboard contents. Callers should fall back to
// an OSC52 escape sequence when this returns an error (for example, no
// clipboard tool is available over SSH).
func (c *Clipboard) WriteText(s string) error {
	if err := c.writeText(s); err != nil {
		return fmt.Errorf("clipboard: write text: %w", err)
	}
	return nil
}

// ReadImage returns the current image clipboard contents as PNG bytes. It
// returns ErrNoImage when the clipboard holds no image, and ErrNoTool when
// the external tool needed on this platform is not installed.
func (c *Clipboard) ReadImage() ([]byte, error) {
	switch c.GOOS {
	case "linux":
		return c.readImageLinux()
	case "darwin":
		return c.readImageDarwin()
	case "windows":
		return c.readImageWindows()
	default:
		return nil, ErrNoImage
	}
}

func (c *Clipboard) readImageLinux() ([]byte, error) {
	if c.Env("WAYLAND_DISPLAY") != "" {
		return c.readImageWayland()
	}
	if c.Env("DISPLAY") != "" {
		return c.readImageX11()
	}
	return nil, ErrNoImage
}

func (c *Clipboard) readImageWayland() ([]byte, error) {
	if _, err := c.LookPath("wl-paste"); err != nil {
		return nil, ErrNoTool{Tool: "wl-paste"}
	}

	types, err := c.Run("wl-paste", "--list-types")
	if err != nil {
		// A non-zero exit here typically means the clipboard is empty.
		return nil, ErrNoImage
	}
	if !hasMIMEType(types, "image/png") {
		return nil, ErrNoImage
	}

	data, err := c.Run("wl-paste", "--type", "image/png")
	if err != nil {
		return nil, ErrNoImage
	}
	if !isPNG(data) {
		return nil, ErrNoImage
	}
	return data, nil
}

func (c *Clipboard) readImageX11() ([]byte, error) {
	if _, err := c.LookPath("xclip"); err != nil {
		return nil, ErrNoTool{Tool: "xclip"}
	}

	targets, err := c.Run("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	if err != nil {
		// A non-zero exit here typically means the clipboard is empty.
		return nil, ErrNoImage
	}
	if !hasMIMEType(targets, "image/png") {
		return nil, ErrNoImage
	}

	data, err := c.Run("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	if err != nil {
		return nil, ErrNoImage
	}
	if !isPNG(data) {
		return nil, ErrNoImage
	}
	return data, nil
}

func (c *Clipboard) readImageDarwin() ([]byte, error) {
	if _, err := c.LookPath("osascript"); err != nil {
		return nil, ErrNoTool{Tool: "osascript"}
	}

	tmpPath := c.tempPNGPath()
	script := `write (the clipboard as «class PNGf») to (open for access POSIX file "` + tmpPath + `" with write permission)`
	if _, err := c.Run("osascript", "-e", script); err != nil {
		// osascript fails with "Can't make ... into type PNGf" (or any
		// other non-zero exit) when the clipboard holds no image.
		return nil, ErrNoImage
	}

	data, err := c.ReadFile(tmpPath)
	_ = os.Remove(tmpPath)
	if err != nil {
		return nil, ErrNoImage
	}
	if !isPNG(data) {
		return nil, ErrNoImage
	}
	return data, nil
}

func (c *Clipboard) readImageWindows() ([]byte, error) {
	if _, err := c.LookPath("powershell"); err != nil {
		return nil, ErrNoTool{Tool: "powershell"}
	}

	tmpPath := c.tempPNGPath()
	script := `Add-Type -AssemblyName System.Windows.Forms; ` +
		`$i=[Windows.Forms.Clipboard]::GetImage(); ` +
		`if($i){$i.Save('` + tmpPath + `',[System.Drawing.Imaging.ImageFormat]::Png)}`
	// Ignore the exit status: the file's presence is the source of truth
	// for whether the clipboard held an image.
	_, _ = c.Run("powershell", "-NoProfile", "-Command", script)

	data, err := c.ReadFile(tmpPath)
	_ = os.Remove(tmpPath)
	if err != nil {
		return nil, ErrNoImage
	}
	if !isPNG(data) {
		return nil, ErrNoImage
	}
	return data, nil
}

// tempPNGPath builds the temp file path used to round-trip an image through
// the OS clipboard tool. It uses c.GOOS (not the build's runtime.GOOS) to
// pick the path separator so behavior is deterministic under test.
func (c *Clipboard) tempPNGPath() string {
	sep := "/"
	if c.GOOS == "windows" {
		sep = "\\"
	}
	dir := strings.TrimRight(c.TempDir(), "/\\")
	name := fmt.Sprintf("notty-clip-%d.png", os.Getpid())
	return dir + sep + name
}

// hasMIMEType reports whether out (newline-separated MIME types, as printed
// by wl-paste --list-types or xclip -t TARGETS -o) lists want.
func hasMIMEType(out []byte, want string) bool {
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// isPNG reports whether b starts with the PNG file signature.
func isPNG(b []byte) bool {
	return bytes.HasPrefix(b, pngSignature)
}
