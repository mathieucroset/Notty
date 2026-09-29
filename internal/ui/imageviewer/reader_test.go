package imageviewer

import (
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/imgrender"
)

func nextBatch(t *testing.T, k *keyReader, within time.Duration) []Key {
	t.Helper()
	select {
	case keys := <-k.keys:
		return keys
	case <-time.After(within):
		t.Fatal("no keys")
		return nil
	}
}

func TestKeyReaderLoneEscWaits(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	const wait = 40 * time.Millisecond
	k := startKeyReader(r, wait)
	defer k.stop()
	start := time.Now()
	if _, err := w.Write([]byte("\x1b")); err != nil {
		t.Fatal(err)
	}
	got := nextBatch(t, k, 2*time.Second)
	if !reflect.DeepEqual(got, []Key{Quit}) {
		t.Fatalf("keys = %v, want [quit]", got)
	}
	if elapsed := time.Since(start); elapsed < wait {
		t.Errorf("lone esc decoded after %v, want >= %v", elapsed, wait)
	}
}

func TestKeyReaderJoinsSplitSequence(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	k := startKeyReader(r, 5*time.Second)
	defer k.stop()
	go func() {
		for _, chunk := range []string{"\x1b", "[", "C", "\x1b_Gi=1;", "OK\x1b\\q"} {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}()
	if got := nextBatch(t, k, time.Second); !reflect.DeepEqual(got, []Key{Next}) {
		t.Fatalf("first batch = %v, want [next]", got)
	}
	if got := nextBatch(t, k, time.Second); !reflect.DeepEqual(got, []Key{Quit}) {
		t.Fatalf("second batch = %v, want [quit] (kitty reply skipped)", got)
	}
}

func TestKeyReaderEOFFlushesPendingEsc(t *testing.T) {
	k := startKeyReader(strings.NewReader("n\x1b"), 5*time.Second)
	defer k.stop()
	if got := nextBatch(t, k, time.Second); !reflect.DeepEqual(got, []Key{Next}) {
		t.Fatalf("first batch = %v, want [next]", got)
	}
	if got := nextBatch(t, k, time.Second); !reflect.DeepEqual(got, []Key{Quit}) {
		t.Fatalf("second batch = %v, want [quit]", got)
	}
	select {
	case <-k.done:
	case <-time.After(time.Second):
		t.Fatal("decoder still running after EOF")
	}
}

func TestRunCtrlQRequestsQuit(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", 10, 10)
	v, out := newTestViewer([]string{a}, imgrender.ProtoHalfBlocks, strings.NewReader("\x11"))
	runViewer(t, v)
	if !v.QuitRequested {
		t.Error("ctrl+q did not set QuitRequested")
	}
	assertOrder(t, out.String(), altEnter, altLeave)
	for _, in := range []string{"q", "\x1b", "\x03", "\x1b[99;5u"} {
		v.SetStdin(strings.NewReader(in))
		runViewer(t, v)
		if v.QuitRequested {
			t.Errorf("%q set QuitRequested; only ctrl+q quits the app", in)
		}
	}
	v.SetStdin(strings.NewReader("\x1b[113;5u"))
	runViewer(t, v)
	if !v.QuitRequested {
		t.Error("kitty ctrl+q did not set QuitRequested")
	}
}
