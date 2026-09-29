package imageviewer

import (
	"io"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// defaultEscWait is how long an incomplete escape sequence at the end of a
// read (most often a lone ESC) waits for the rest before it is decoded as
// is. A real esc key press is a single read; a sequence split across reads
// continues within microseconds.
const defaultEscWait = 25 * time.Millisecond

// keyReader reads the terminal in one goroutine and decodes keys in
// another.
type keyReader struct {
	keys       chan []Key
	done       chan struct{} // closed when the decoder exits (input closed or stopped)
	readerDone chan struct{} // closed when the reading goroutine exits
	quit       chan struct{}
	cr         interface {
		Cancel() bool
		Close() error
	}
}

// startKeyReader starts reading in. A terminal (or any file the platform
// can poll) is read through a cancelable reader, so stop leaves no
// goroutine reading bytes that belong to Bubble Tea afterwards. Other
// readers cannot be interrupted; their goroutine ends at EOF.
//
// On Windows the cancelable reader only works for the process's standard
// input console handle (the CONIN$ Bubble Tea passes when stdin is
// redirected falls back to a plain read that cannot be canceled).
func startKeyReader(in io.Reader, escWait time.Duration) *keyReader {
	if escWait <= 0 {
		escWait = defaultEscWait
	}
	k := &keyReader{
		keys:       make(chan []Key),
		done:       make(chan struct{}),
		readerDone: make(chan struct{}),
		quit:       make(chan struct{}),
	}
	src := in
	if cr, err := uv.NewCancelReader(in); err == nil {
		k.cr, src = cr, cr
	}
	raw := make(chan []byte)
	go k.read(src, raw)
	go k.decode(raw, escWait)
	return k
}

// read forwards raw chunks until the input fails or the reader is stopped.
func (k *keyReader) read(src io.Reader, raw chan<- []byte) {
	defer close(k.readerDone)
	defer close(raw)
	buf := make([]byte, 256)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			select {
			case raw <- append([]byte(nil), buf[:n]...):
			case <-k.quit:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// decode turns raw chunks into key batches. An incomplete sequence at the
// end of a chunk is held back for escWait: continued by the next chunk it
// decodes as one sequence, otherwise it is decoded alone (a lone ESC is the
// esc key).
func (k *keyReader) decode(raw <-chan []byte, escWait time.Duration) {
	defer close(k.done)
	var pending []byte
	var timeout <-chan time.Time
	for {
		select {
		case chunk, ok := <-raw:
			if !ok {
				if keys, _ := decode(pending, true); len(keys) > 0 {
					k.send(keys)
				}
				return
			}
			data := append(pending, chunk...)
			keys, rest := decode(data, false)
			pending, timeout = nil, nil
			if rest > 0 {
				pending = append([]byte(nil), data[len(data)-rest:]...)
				timeout = time.After(escWait)
			}
			if len(keys) > 0 && !k.send(keys) {
				return
			}
		case <-timeout:
			keys, _ := decode(pending, true)
			pending, timeout = nil, nil
			if len(keys) > 0 && !k.send(keys) {
				return
			}
		case <-k.quit:
			return
		}
	}
}

// send delivers a batch, reporting false when the reader was stopped.
func (k *keyReader) send(keys []Key) bool {
	select {
	case k.keys <- keys:
		return true
	case <-k.quit:
		return false
	}
}

// stop ends the reader. For a cancelable reader it waits until the reading
// goroutine has exited.
func (k *keyReader) stop() {
	close(k.quit)
	if k.cr == nil {
		return
	}
	if k.cr.Cancel() {
		select {
		case <-k.readerDone:
		case <-time.After(time.Second):
		}
	}
	_ = k.cr.Close()
}
