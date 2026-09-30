package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// detachHelperEnv names the file TestDetachHelper writes when this test
// binary runs as startDetached's child.
const detachHelperEnv = "NOTTY_TEST_DETACH_MARKER"

// TestDetachHelper is not a test: it is the child process of
// TestStartDetached, which runs this test binary again.
func TestDetachHelper(t *testing.T) {
	marker := os.Getenv(detachHelperEnv)
	if marker == "" {
		t.Skip("runs only as TestStartDetached's child")
	}
	if err := os.WriteFile(marker+".tmp", []byte("ran"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(marker+".tmp", marker); err != nil {
		t.Fatal(err)
	}
}

// startDetached really starts the child, on every OS, and returns without
// waiting for it.
func TestStartDetached(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	t.Setenv(detachHelperEnv, marker)
	if err := startDetached(os.Args[0], "-test.run=^TestDetachHelper$"); err != nil {
		t.Fatalf("startDetached: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if b, err := os.ReadFile(marker); err == nil {
			if string(b) != "ran" {
				t.Fatalf("marker = %q", b)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the detached child never ran")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStartDetachedMissingProgram(t *testing.T) {
	if err := startDetached(filepath.Join(t.TempDir(), "no-such-notty")); err == nil {
		t.Fatal("startDetached of a missing program succeeded")
	}
}

// detach gives the child its own session (Unix) or process group (Windows).
func TestDetachSetsAttributes(t *testing.T) {
	cmd := exec.Command("notty")
	detach(cmd)
	switch runtime.GOOS {
	case "windows", "linux", "darwin", "freebsd", "openbsd", "netbsd":
		if cmd.SysProcAttr == nil {
			t.Fatal("no SysProcAttr set")
		}
		checkDetached(t, cmd)
	}
}
