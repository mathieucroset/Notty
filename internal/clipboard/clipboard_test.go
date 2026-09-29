package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

var validPNG = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, []byte("fake-image-data")...)

// tempPNGName is the temp file name ReadImage uses on darwin/windows,
// mirroring the pid-suffixed name clipboard.go builds so tests stay in
// sync regardless of which process runs them.
var tempPNGName = fmt.Sprintf("notty-clip-%d.png", os.Getpid())

// runCall records one invocation of the fake Run function.
type runCall struct {
	name string
	args []string
}

// fakeClipboard builds a *Clipboard wired to fakes, recording Run calls into calls.
func fakeClipboard(calls *[]runCall) *Clipboard {
	return &Clipboard{
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		Run: func(name string, args ...string) ([]byte, error) {
			*calls = append(*calls, runCall{name: name, args: args})
			return nil, errors.New("unstubbed call: " + name)
		},
		Env:      func(string) string { return "" },
		TempDir:  func() string { return "/faketmp" },
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("unstubbed ReadFile") },
		Remove:   func(string) error { return nil },
	}
}

func TestReadImage_Linux_Wayland_HasPNG(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	c.Env = func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		switch {
		case name == "wl-paste" && len(args) == 1 && args[0] == "--list-types":
			return []byte("text/plain;charset=utf-8\nimage/png\n"), nil
		case name == "wl-paste" && len(args) == 2 && args[0] == "--type" && args[1] == "image/png":
			return validPNG, nil
		}
		t.Fatalf("unexpected Run call: %s %v", name, args)
		return nil, nil
	}

	got, err := c.ReadImage()
	if err != nil {
		t.Fatalf("ReadImage() error = %v, want nil", err)
	}
	if !bytes.Equal(got, validPNG) {
		t.Fatalf("ReadImage() = %v, want %v", got, validPNG)
	}
	wantCalls := []runCall{
		{name: "wl-paste", args: []string{"--list-types"}},
		{name: "wl-paste", args: []string{"--type", "image/png"}},
	}
	assertCalls(t, calls, wantCalls)
}

func TestReadImage_Linux_Wayland_NoPNGType(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	c.Env = func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return []byte("text/plain;charset=utf-8\n"), nil
	}

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want ErrNoImage", err)
	}
}

// TestReadImage_Linux_Wayland_ListTypesFailureIsWrapped ensures a genuine
// wl-paste failure (as opposed to "clipboard is simply empty") is still
// debuggable: errors.Is must still match ErrNoImage, but the underlying
// error text must not be discarded.
func TestReadImage_Linux_Wayland_ListTypesFailureIsWrapped(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	c.Env = func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	underlying := errors.New("wl-paste: no compositor running")
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, underlying
	}

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want it to satisfy errors.Is(err, ErrNoImage)", err)
	}
	if !strings.Contains(err.Error(), underlying.Error()) {
		t.Fatalf("ReadImage() error = %q, want it to contain underlying error %q", err.Error(), underlying.Error())
	}
}

func TestReadImage_Linux_Wayland_MissingTool(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	c.Env = func(k string) string {
		if k == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	c.LookPath = func(name string) (string, error) { return "", errors.New("not found") }

	_, err := c.ReadImage()
	var noTool ErrNoTool
	if !errors.As(err, &noTool) {
		t.Fatalf("ReadImage() error = %v, want ErrNoTool", err)
	}
	if noTool.Tool != "wl-paste" {
		t.Fatalf("ErrNoTool.Tool = %q, want wl-paste", noTool.Tool)
	}
	if got, want := noTool.Error(), "clipboard tool not found: wl-paste (install wl-clipboard)"; got != want {
		t.Fatalf("ErrNoTool.Error() = %q, want %q", got, want)
	}
	if len(calls) != 0 {
		t.Fatalf("Run should not be called when tool is missing, got %d calls", len(calls))
	}
}

func TestReadImage_Linux_X11_HasPNG(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	c.Env = func(k string) string {
		if k == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		switch {
		case name == "xclip" && len(args) == 5 && args[3] == "TARGETS":
			return []byte("TARGETS\nimage/png\n"), nil
		case name == "xclip" && len(args) == 5 && args[3] == "image/png":
			return validPNG, nil
		}
		t.Fatalf("unexpected Run call: %s %v", name, args)
		return nil, nil
	}

	got, err := c.ReadImage()
	if err != nil {
		t.Fatalf("ReadImage() error = %v, want nil", err)
	}
	if !bytes.Equal(got, validPNG) {
		t.Fatalf("ReadImage() = %v, want %v", got, validPNG)
	}
	wantCalls := []runCall{
		{name: "xclip", args: []string{"-selection", "clipboard", "-t", "TARGETS", "-o"}},
		{name: "xclip", args: []string{"-selection", "clipboard", "-t", "image/png", "-o"}},
	}
	assertCalls(t, calls, wantCalls)
}

func TestReadImage_Linux_X11_NoPNGType(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	c.Env = func(k string) string {
		if k == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return []byte("TARGETS\nUTF8_STRING\n"), nil
	}

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want ErrNoImage", err)
	}
}

// TestReadImage_Linux_X11_TargetsFailureIsWrapped ensures a genuine xclip
// failure (as opposed to "clipboard is simply empty") is still debuggable:
// errors.Is must still match ErrNoImage, but the underlying error text
// must not be discarded.
func TestReadImage_Linux_X11_TargetsFailureIsWrapped(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	c.Env = func(k string) string {
		if k == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	underlying := errors.New("xclip: Error: target STRING not available")
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, underlying
	}

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want it to satisfy errors.Is(err, ErrNoImage)", err)
	}
	if !strings.Contains(err.Error(), underlying.Error()) {
		t.Fatalf("ReadImage() error = %q, want it to contain underlying error %q", err.Error(), underlying.Error())
	}
}

func TestReadImage_Linux_X11_MissingTool(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	c.Env = func(k string) string {
		if k == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	c.LookPath = func(name string) (string, error) { return "", errors.New("not found") }

	_, err := c.ReadImage()
	var noTool ErrNoTool
	if !errors.As(err, &noTool) {
		t.Fatalf("ReadImage() error = %v, want ErrNoTool", err)
	}
	if noTool.Tool != "xclip" {
		t.Fatalf("ErrNoTool.Tool = %q, want xclip", noTool.Tool)
	}
	if got, want := noTool.Error(), "clipboard tool not found: xclip (install xclip)"; got != want {
		t.Fatalf("ErrNoTool.Error() = %q, want %q", got, want)
	}
}

func TestReadImage_Linux_NoDisplayServer(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	// Env already returns "" for everything.

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want ErrNoImage", err)
	}
	if len(calls) != 0 {
		t.Fatalf("Run should not be called with no display server, got %d calls", len(calls))
	}
}

func TestReadImage_Linux_WaylandPreferredOverX11(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "linux"
	c.Env = func(k string) string {
		switch k {
		case "WAYLAND_DISPLAY":
			return "wayland-0"
		case "DISPLAY":
			return ":0"
		}
		return ""
	}
	lookedUp := ""
	c.LookPath = func(name string) (string, error) {
		lookedUp = name
		return "", errors.New("not found")
	}

	_, err := c.ReadImage()
	var noTool ErrNoTool
	if !errors.As(err, &noTool) || noTool.Tool != "wl-paste" {
		t.Fatalf("expected wl-paste to be tried first, got err=%v lookedUp=%q", err, lookedUp)
	}
}

func TestReadImage_Darwin_Success(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "darwin"
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, nil
	}
	wantPath := "/faketmp/" + tempPNGName
	c.ReadFile = func(path string) ([]byte, error) {
		if path != wantPath {
			t.Fatalf("ReadFile called with %q, want %q", path, wantPath)
		}
		return validPNG, nil
	}

	got, err := c.ReadImage()
	if err != nil {
		t.Fatalf("ReadImage() error = %v, want nil", err)
	}
	if !bytes.Equal(got, validPNG) {
		t.Fatalf("ReadImage() = %v, want %v", got, validPNG)
	}
	if len(calls) != 1 || calls[0].name != "osascript" {
		t.Fatalf("unexpected Run calls: %v", calls)
	}
	if len(calls[0].args) != 2 || calls[0].args[0] != "-e" {
		t.Fatalf("unexpected osascript args: %v", calls[0].args)
	}
	script := calls[0].args[1]
	wantScript := `write (the clipboard as «class PNGf») to (open for access POSIX file "` + wantPath + `" with write permission)`
	if script != wantScript {
		t.Fatalf("osascript script = %q, want %q", script, wantScript)
	}
}

func TestReadImage_Darwin_MissingTool(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "darwin"
	c.LookPath = func(name string) (string, error) { return "", errors.New("not found") }

	_, err := c.ReadImage()
	var noTool ErrNoTool
	if !errors.As(err, &noTool) || noTool.Tool != "osascript" {
		t.Fatalf("ReadImage() error = %v, want ErrNoTool{osascript}", err)
	}
}

func TestReadImage_Darwin_OsascriptCantMake(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "darwin"
	underlying := errors.New("execution error: Can't make «class PNGf» into type... (-1700)")
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, underlying
	}

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want it to satisfy errors.Is(err, ErrNoImage)", err)
	}
	// The underlying osascript failure must still be visible for debugging.
	if !strings.Contains(err.Error(), underlying.Error()) {
		t.Fatalf("ReadImage() error = %q, want it to contain underlying error %q", err.Error(), underlying.Error())
	}
}

func TestReadImage_Darwin_NonPNGBytes(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "darwin"
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, nil
	}
	c.ReadFile = func(string) ([]byte, error) { return []byte("not a png"), nil }

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want ErrNoImage", err)
	}
}

// TestReadImage_Darwin_StaleTempFileIsNotReportedAsFreshImage is a
// regression test for a critical bug: a PNG left over from a previous
// ReadImage call must never be mistaken for the current clipboard content
// when osascript produces nothing this time (e.g. the clipboard now holds
// no image). ReadImage must remove the temp path before running osascript,
// so that if osascript doesn't recreate it, the post-run ReadFile fails
// and ReadImage returns ErrNoImage instead of the stale bytes.
func TestReadImage_Darwin_StaleTempFileIsNotReportedAsFreshImage(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "darwin"

	removed := false
	c.Remove = func(string) error {
		removed = true
		return nil
	}
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		// osascript "succeeds" but the clipboard has no image, so it
		// writes nothing new to the temp file this time.
		return nil, nil
	}
	c.ReadFile = func(string) ([]byte, error) {
		if removed {
			return nil, errors.New("no such file")
		}
		// A stale PNG left on disk from an earlier, successful call.
		return validPNG, nil
	}

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want ErrNoImage (must not report the stale temp file as a fresh image)", err)
	}
	if !removed {
		t.Fatalf("ReadImage() never called Remove on the temp path")
	}
}

// TestReadImage_Darwin_EscapesTempPathForAppleScript guards against a temp
// dir containing a double quote or backslash breaking out of the
// AppleScript double-quoted POSIX file literal.
func TestReadImage_Darwin_EscapesTempPathForAppleScript(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "darwin"
	c.TempDir = func() string { return `/tmp/weird"quote\slash` }
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, nil
	}
	c.ReadFile = func(string) ([]byte, error) { return validPNG, nil }

	if _, err := c.ReadImage(); err != nil {
		t.Fatalf("ReadImage() error = %v, want nil", err)
	}

	rawPath := `/tmp/weird"quote\slash/` + tempPNGName
	wantEscapedPath := `/tmp/weird\"quote\\slash/` + tempPNGName
	script := calls[0].args[1]
	wantScript := `write (the clipboard as «class PNGf») to (open for access POSIX file "` + wantEscapedPath + `" with write permission)`
	if script != wantScript {
		t.Fatalf("osascript script = %q, want %q", script, wantScript)
	}
	if strings.Contains(script, rawPath) {
		t.Fatalf("osascript script = %q, contains unescaped raw path", script)
	}
}

func TestReadImage_Windows_Success(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "windows"
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, nil
	}
	wantPath := `/faketmp\` + tempPNGName
	c.ReadFile = func(path string) ([]byte, error) {
		if path != wantPath {
			t.Fatalf("ReadFile called with %q, want %q", path, wantPath)
		}
		return validPNG, nil
	}

	got, err := c.ReadImage()
	if err != nil {
		t.Fatalf("ReadImage() error = %v, want nil", err)
	}
	if !bytes.Equal(got, validPNG) {
		t.Fatalf("ReadImage() = %v, want %v", got, validPNG)
	}
	if len(calls) != 1 || calls[0].name != "powershell" {
		t.Fatalf("unexpected Run calls: %v", calls)
	}
	if len(calls[0].args) != 3 || calls[0].args[0] != "-NoProfile" || calls[0].args[1] != "-Command" {
		t.Fatalf("unexpected powershell args: %v", calls[0].args)
	}
	script := calls[0].args[2]
	if !strings.Contains(script, wantPath) {
		t.Fatalf("powershell script = %q, want it to reference temp path", script)
	}
	if !strings.Contains(script, "System.Windows.Forms") || !strings.Contains(script, "ImageFormat]::Png") {
		t.Fatalf("powershell script = %q, missing expected fragments", script)
	}
	// System.Drawing must be loaded alongside System.Windows.Forms: the
	// script references [System.Drawing.Imaging.ImageFormat]::Png, which
	// System.Windows.Forms alone does not guarantee is loaded.
	if !strings.Contains(script, "Add-Type -AssemblyName System.Windows.Forms, System.Drawing") {
		t.Fatalf("powershell script = %q, want it to load System.Drawing", script)
	}
}

// TestReadImage_Windows_EscapesTempPathForPowerShell guards against a temp
// dir containing an apostrophe breaking out of the PowerShell
// single-quoted path literal.
func TestReadImage_Windows_EscapesTempPathForPowerShell(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "windows"
	c.TempDir = func() string { return `/fake'tmp` }
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, nil
	}
	c.ReadFile = func(string) ([]byte, error) { return validPNG, nil }

	if _, err := c.ReadImage(); err != nil {
		t.Fatalf("ReadImage() error = %v, want nil", err)
	}

	wantEscapedPath := `/fake''tmp\` + tempPNGName
	script := calls[0].args[2]
	if !strings.Contains(script, `'`+wantEscapedPath+`'`) {
		t.Fatalf("powershell script = %q, want it to contain escaped path %q", script, wantEscapedPath)
	}
}

// TestTempPNGPath_IncludesPID guards against a filename collision when two
// notty processes race to read an image from the clipboard at once: each
// process must round-trip through its own temp file.
func TestTempPNGPath_IncludesPID(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "darwin"
	got := c.tempPNGPath()
	want := "/faketmp/" + fmt.Sprintf("notty-clip-%d.png", os.Getpid())
	if got != want {
		t.Fatalf("tempPNGPath() = %q, want %q", got, want)
	}
}

func TestReadImage_Windows_MissingTool(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "windows"
	c.LookPath = func(name string) (string, error) { return "", errors.New("not found") }

	_, err := c.ReadImage()
	var noTool ErrNoTool
	if !errors.As(err, &noTool) || noTool.Tool != "powershell" {
		t.Fatalf("ReadImage() error = %v, want ErrNoTool{powershell}", err)
	}
}

func TestReadImage_Windows_FileNotCreated(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "windows"
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, nil
	}
	c.ReadFile = func(string) ([]byte, error) { return nil, errors.New("file does not exist") }

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want ErrNoImage", err)
	}
}

// TestReadImage_Windows_PowershellFailureIsWrapped ensures a genuine
// powershell failure (as opposed to "clipboard simply had no image") is
// still debuggable: errors.Is must still match ErrNoImage, but the
// underlying error text must not be discarded.
func TestReadImage_Windows_PowershellFailureIsWrapped(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "windows"
	underlying := errors.New("powershell: script execution is disabled on this system")
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		return nil, underlying
	}
	c.ReadFile = func(string) ([]byte, error) { return nil, errors.New("file does not exist") }

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want it to satisfy errors.Is(err, ErrNoImage)", err)
	}
	if !strings.Contains(err.Error(), underlying.Error()) {
		t.Fatalf("ReadImage() error = %q, want it to contain underlying error %q", err.Error(), underlying.Error())
	}
}

// TestReadImage_Windows_StaleTempFileIsNotReportedAsFreshImage is the
// Windows counterpart of the macOS regression test: a PNG left over from a
// previous ReadImage call must never be mistaken for the current clipboard
// content when powershell produces nothing this time.
func TestReadImage_Windows_StaleTempFileIsNotReportedAsFreshImage(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "windows"

	removed := false
	c.Remove = func(string) error {
		removed = true
		return nil
	}
	c.Run = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, runCall{name: name, args: args})
		// powershell runs but the clipboard has no image, so $i is $null
		// and nothing is (re)written to the temp file this time.
		return nil, nil
	}
	c.ReadFile = func(string) ([]byte, error) {
		if removed {
			return nil, errors.New("file does not exist")
		}
		// A stale PNG left on disk from an earlier, successful call.
		return validPNG, nil
	}

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want ErrNoImage (must not report the stale temp file as a fresh image)", err)
	}
	if !removed {
		t.Fatalf("ReadImage() never called Remove on the temp path")
	}
}

func TestReadImage_UnsupportedGOOS(t *testing.T) {
	var calls []runCall
	c := fakeClipboard(&calls)
	c.GOOS = "plan9"

	_, err := c.ReadImage()
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("ReadImage() error = %v, want ErrNoImage", err)
	}
}

func TestReadText(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := &Clipboard{readText: func() (string, error) { return "hello", nil }}
		got, err := c.ReadText()
		if err != nil {
			t.Fatalf("ReadText() error = %v, want nil", err)
		}
		if got != "hello" {
			t.Fatalf("ReadText() = %q, want %q", got, "hello")
		}
	})
	t.Run("error", func(t *testing.T) {
		wantErr := errors.New("boom")
		c := &Clipboard{readText: func() (string, error) { return "", wantErr }}
		_, err := c.ReadText()
		if err == nil || !errors.Is(err, wantErr) {
			t.Fatalf("ReadText() error = %v, want wrapped %v", err, wantErr)
		}
	})
}

func TestWriteText(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var got string
		c := &Clipboard{writeText: func(s string) error { got = s; return nil }}
		if err := c.WriteText("hi"); err != nil {
			t.Fatalf("WriteText() error = %v, want nil", err)
		}
		if got != "hi" {
			t.Fatalf("writeText called with %q, want %q", got, "hi")
		}
	})
	t.Run("error", func(t *testing.T) {
		wantErr := errors.New("no clipboard tool")
		c := &Clipboard{writeText: func(string) error { return wantErr }}
		err := c.WriteText("hi")
		if err == nil || !errors.Is(err, wantErr) {
			t.Fatalf("WriteText() error = %v, want wrapped %v", err, wantErr)
		}
	})
}

func TestErrNoTool_Error(t *testing.T) {
	tests := []struct {
		tool string
		want string
	}{
		{"wl-paste", "clipboard tool not found: wl-paste (install wl-clipboard)"},
		{"xclip", "clipboard tool not found: xclip (install xclip)"},
		{"osascript", "clipboard tool not found: osascript"},
		{"powershell", "clipboard tool not found: powershell"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			err := ErrNoTool{Tool: tt.tool}
			if got := err.Error(); got != tt.want {
				t.Fatalf("ErrNoTool{%q}.Error() = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}

func TestDefault(t *testing.T) {
	c := Default()
	if c.LookPath == nil || c.Run == nil || c.Env == nil || c.TempDir == nil || c.ReadFile == nil || c.Remove == nil {
		t.Fatalf("Default() left required fields nil: %+v", c)
	}
	if c.GOOS == "" {
		t.Fatalf("Default().GOOS is empty")
	}
}

func assertCalls(t *testing.T, got, want []runCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Run calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].name != want[i].name || !equalArgs(got[i].args, want[i].args) {
			t.Fatalf("Run call[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
