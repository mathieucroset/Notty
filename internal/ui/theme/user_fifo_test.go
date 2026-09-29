//go:build linux || darwin

package theme

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestLoadUserFIFO checks a FIFO named like a theme is rejected without
// blocking (opening a FIFO for reading waits for a writer).
func TestLoadUserFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.toml")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := LoadUser(dir, "x")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("err = %v, want not a regular file", err)
		}
	case <-time.After(5 * time.Second):
		// Unblock the reader so the goroutine can finish.
		if w, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
			_ = w.Close()
		}
		t.Fatal("LoadUser blocked on a FIFO")
	}
}
