//go:build unix

package imgrender

import (
	"io"
	"os"
	"testing"
	"time"
)

// pipeTTY reads from a pipe (embedding *os.File gives it Fd, SyscallConn and
// SetReadDeadline) and discards writes.
type pipeTTY struct{ *os.File }

func (pipeTTY) Write(p []byte) (int, error) { return len(p), nil }

// fdOnlyTTY hides SetReadDeadline so Detect must poll the fd.
type fdOnlyTTY struct{ f *os.File }

func (t fdOnlyTTY) Read(p []byte) (int, error) { return t.f.Read(p) }
func (fdOnlyTTY) Write(p []byte) (int, error)  { return len(p), nil }
func (t fdOnlyTTY) Fd() uintptr                { return t.f.Fd() }

// TestDetectLeavesNoPendingRead is the regression test for a Read left
// blocked after Detect returns: such a Read would steal the next input byte
// from Bubble Tea. After Detect times out, a byte written to the tty must
// reach a fresh reader.
func TestDetectLeavesNoPendingRead(t *testing.T) {
	tests := []struct {
		name string
		wrap func(*os.File) io.ReadWriter
	}{
		{"file raw fd poll", func(f *os.File) io.ReadWriter { return pipeTTY{f} }},
		{"poll fd", func(f *os.File) io.ReadWriter { return fdOnlyTTY{f} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()

			start := time.Now()
			got := Detect("auto", envMap(), tt.wrap(r), noRun(t))
			if el := time.Since(start); el < 90*time.Millisecond || el > 500*time.Millisecond {
				t.Errorf("Detect took %v, want about 100ms", el)
			}
			if got.Inline != ProtoHalfBlocks {
				t.Errorf("got %+v", got)
			}

			if _, err := w.WriteString("x"); err != nil {
				t.Fatal(err)
			}
			recv := make(chan string, 1)
			go func() {
				b := make([]byte, 1)
				n, _ := r.Read(b)
				recv <- string(b[:n])
			}()
			select {
			case s := <-recv:
				if s != "x" {
					t.Errorf("fresh reader got %q, want x", s)
				}
			case <-time.After(time.Second):
				t.Fatal("byte written after Detect was not received: a Read was left pending")
			}
		})
	}
}

// TestDetectBlockingFile is the regression test for a file switched to
// blocking mode by Fd(): SetReadDeadline then returns nil but no longer
// bounds Read, so trusting it would block Detect forever.
func TestDetectBlockingFile(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	_ = r.Fd() // switches r to blocking mode
	done := make(chan Caps, 1)
	start := time.Now()
	go func() { done <- Detect("auto", envMap(), pipeTTY{r}, noRun(t)) }()
	select {
	case got := <-done:
		if el := time.Since(start); el > 200*time.Millisecond {
			t.Errorf("Detect took %v, want about 100ms", el)
		}
		if got.Inline != ProtoHalfBlocks {
			t.Errorf("got %+v", got)
		}
	case <-time.After(2 * time.Second):
		w.Close() // unblock the stuck Read
		t.Fatal("Detect blocked on a file whose read deadline has no effect")
	}
}

// TestDetectBlockingFileReadsReplies checks that replies are read through
// the raw-fd poll path.
func TestDetectBlockingFileReadsReplies(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	_ = r.Fd()
	if _, err := w.WriteString(replyKittyOK + replyCell + replyDA1Six); err != nil {
		t.Fatal(err)
	}
	got := Detect("auto", envMap(), pipeTTY{r}, noRun(t))
	if got.Inline != ProtoKitty || got.CellW != 10 || got.CellH != 20 {
		t.Errorf("got %+v", got)
	}
}

// TestDetectPollReadsReplies checks that the poll path parses replies.
func TestDetectPollReadsReplies(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if _, err := w.WriteString(replyKittyOK + replyCell + replyDA1Six); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	got := Detect("auto", envMap(), fdOnlyTTY{r}, noRun(t))
	if got.Inline != ProtoKitty || got.CellW != 10 || got.CellH != 20 {
		t.Errorf("got %+v", got)
	}
	if el := time.Since(start); el > 50*time.Millisecond {
		t.Errorf("DA1 should end the wait early, took %v", el)
	}
}
