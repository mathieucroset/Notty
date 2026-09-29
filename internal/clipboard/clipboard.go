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
	Remove   func(string) error

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
		Remove:    os.Remove,
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
		// A non-zero exit here typically means the clipboard is empty, but
		// wrap the underlying error so a genuine wl-paste failure is still
		// debuggable via errors.Unwrap.
		return nil, fmt.Errorf("%w: %w", ErrNoImage, err)
	}
	if !hasMIMEType(types, "image/png") {
		return nil, ErrNoImage
	}

	data, err := c.Run("wl-paste", "--type", "image/png")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoImage, err)
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
		// A non-zero exit here typically means the clipboard is empty, but
		// wrap the underlying error so a genuine xclip failure is still
		// debuggable via errors.Unwrap.
		return nil, fmt.Errorf("%w: %w", ErrNoImage, err)
	}
	if !hasMIMEType(targets, "image/png") {
		return nil, ErrNoImage
	}

	data, err := c.Run("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoImage, err)
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
	// Clear any stale file left over from a previous run before invoking
	// osascript, so a crash or a "no image" result can never be masked by
	// leftover data from an earlier successful paste.
	_ = c.Remove(tmpPath)

	script := `write (the clipboard as «class PNGf») to (open for access POSIX file "` + escapeAppleScriptString(tmpPath) + `" with write permission)`
	// osascript fails with "Can't make ... into type PNGf" (or any other
	// non-zero exit) when the clipboard holds no image. Don't return early
	// on that error: fall through to the ReadFile check below, and only
	// surface it (wrapped) if the file really is missing, so a genuine
	// osascript failure is still debuggable via errors.Unwrap.
	_, runErr := c.Run("osascript", "-e", script)

	data, err := c.ReadFile(tmpPath)
	_ = c.Remove(tmpPath)
	if err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrNoImage, runErr)
		}
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
	// Clear any stale file left over from a previous run before invoking
	// powershell: the file's presence afterwards is the source of truth
	// for whether the clipboard held an image, so a leftover file here
	// would be misread as a fresh paste.
	_ = c.Remove(tmpPath)

	script := `Add-Type -AssemblyName System.Windows.Forms, System.Drawing; ` +
		`$i=[Windows.Forms.Clipboard]::GetImage(); ` +
		`if($i){$i.Save('` + escapePowerShellString(tmpPath) + `',[System.Drawing.Imaging.ImageFormat]::Png)}`
	// The file's presence is the source of truth for whether the clipboard
	// held an image, so a non-zero exit here doesn't return early either;
	// it's only surfaced (wrapped) if the file really is missing below, so
	// a genuine powershell failure is still debuggable via errors.Unwrap.
	_, runErr := c.Run("powershell", "-NoProfile", "-Command", script)

	data, err := c.ReadFile(tmpPath)
	_ = c.Remove(tmpPath)
	if err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrNoImage, runErr)
		}
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

// escapeAppleScriptString escapes s for embedding inside an AppleScript
// double-quoted string literal. Backslashes must be escaped before quotes,
// since escaping quotes introduces new backslashes that must not themselves
// be re-escaped.
func escapeAppleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// escapePowerShellString escapes s for embedding inside a PowerShell
// single-quoted string literal, where the only special character is the
// single quote itself, escaped by doubling it.
func escapePowerShellString(s string) string {
	return strings.ReplaceAll(s, `'`, `''`)
}
