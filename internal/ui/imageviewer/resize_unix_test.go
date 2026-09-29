//go:build !windows

package imageviewer

import (
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/imgrender"
)

func TestWatchResizeSIGWINCH(t *testing.T) {
	resized, stop := watchResize(nil)
	defer stop()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	select {
	case <-resized:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGWINCH not reported")
	}
}

func TestRunRedrawsOnSIGWINCH(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 40, 20)
	inR, inW := io.Pipe()
	defer inR.Close()
	v, out := newTestViewer([]string{a}, imgrender.ProtoHalfBlocks, inR)
	widths := make(chan int)
	v.Size = func() (int, int, error) { return <-widths, 24, nil }
	done := make(chan error, 1)
	go func() { done <- v.Run() }() // the real watcher (v.watch is nil)
	widths <- 80                    // initial draw; the watcher is running
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	select {
	case widths <- 40: // the redraw asks for the new size
	case <-time.After(2 * time.Second):
		t.Fatal("no redraw after SIGWINCH")
	}
	if _, err := inW.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	inW.Close()
	s := out.String()
	assertOrder(t, s, "\x1b[24;1H\x1b[7ma.png", "\x1b[24;1H\x1b[7ma.png", altLeave)
	if !strings.Contains(s, padRight("a.png  40x20  1/1    "+hints, 39)+"\x1b[0m") {
		t.Error("redraw did not use the new width")
	}
}
