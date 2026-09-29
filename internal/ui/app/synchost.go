package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/syncer"
	"github.com/mathieucroset/notty/internal/watcher"
)

// flushTimeout bounds how long Host.Flush waits for the UI to save the
// open buffer (plan amendment A7).
const flushTimeout = 3 * time.Second

var (
	// errProgramDone is Host.Flush's answer once the program has exited.
	errProgramDone = errors.New("app: the program has exited")
	// errFlushTimeout is Host.Flush's answer when the UI did not reply.
	errFlushTimeout = errors.New("app: flush timed out")
)

// syncHost implements syncer.Host (plan Task 32). The syncer calls it from
// its worker goroutine; it never touches the model directly. Requests are
// queued, in order, for the app, which receives them through
// listenHostCmd; nothing here ever blocks on the UI except Flush, which
// waits for its reply at most flushTimeout.
//
// The queue is read by a listener command rather than pushed with
// Program.Send: it keeps the order of lock, flush and unlock, never blocks
// the worker while the terminal is released for $EDITOR, and works the
// same in tests that drive Update without a program.
type syncHost struct {
	mu    sync.Mutex
	queue []tea.Msg
	sig   chan struct{}

	done     chan struct{} // closed by Model.Shutdown once the program exited
	doneOnce sync.Once
	// ctx bounds the syncer and the app's listeners; shutdown cancels it.
	ctx    context.Context
	cancel context.CancelFunc

	watcher atomic.Pointer[watcher.Watcher]
	editing atomic.Bool

	flushTimeout time.Duration
}

var _ syncer.Host = (*syncHost)(nil)

func newSyncHost(w *watcher.Watcher) *syncHost {
	h := &syncHost{sig: make(chan struct{}, 1), done: make(chan struct{}), flushTimeout: flushTimeout}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	h.watcher.Store(w)
	return h
}

// Host messages, delivered to Update in the order the syncer made the calls.
type (
	// flushRequestMsg asks the app to save the open buffer and reply.
	flushRequestMsg struct{ reply chan error }
	// lockMutationsMsg starts the locked merge section.
	lockMutationsMsg struct{}
	// unlockMutationsMsg ends it, with the paths the merge left conflicted.
	unlockMutationsMsg struct{ conflicted map[string]bool }
)

// post queues msg for the app.
func (h *syncHost) post(msg tea.Msg) {
	h.mu.Lock()
	h.queue = append(h.queue, msg)
	h.mu.Unlock()
	select {
	case h.sig <- struct{}{}:
	default:
	}
}

// next blocks until a request is queued and returns it, or returns nil once
// the program is done.
func (h *syncHost) next() tea.Msg {
	for {
		h.mu.Lock()
		if len(h.queue) > 0 {
			msg := h.queue[0]
			h.queue = h.queue[1:]
			h.mu.Unlock()
			return msg
		}
		h.mu.Unlock()
		select {
		case <-h.sig:
		case <-h.done:
			return nil
		}
	}
}

// shutdown marks the program as exited: Flush fails fast from now on and the
// listener stops.
func (h *syncHost) shutdown() {
	h.doneOnce.Do(func() {
		close(h.done)
		h.cancel()
	})
}

// isDone reports whether the program has exited.
func (h *syncHost) isDone() bool {
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

// Flush asks the app to save the open buffer and waits for the answer.
func (h *syncHost) Flush() error {
	if h.isDone() {
		return errProgramDone
	}
	reply := make(chan error, 1)
	h.post(flushRequestMsg{reply: reply})
	t := time.NewTimer(h.flushTimeout)
	defer t.Stop()
	select {
	case err := <-reply:
		return err
	case <-t.C:
		return errFlushTimeout
	case <-h.done:
		return errProgramDone
	}
}

// LockMutations locks the editor and queues file-changing actions.
func (h *syncHost) LockMutations() { h.post(lockMutationsMsg{}) }

// UnlockMutations releases them.
func (h *syncHost) UnlockMutations(conflicted map[string]bool) {
	h.post(unlockMutationsMsg{conflicted: conflicted})
}

// PauseWatcher pauses the vault watcher (safe after it closed).
func (h *syncHost) PauseWatcher() {
	if w := h.watcher.Load(); w != nil {
		w.Pause()
	}
}

// ResumeWatcher resumes it.
func (h *syncHost) ResumeWatcher() {
	if w := h.watcher.Load(); w != nil {
		w.Resume()
	}
}

// ExternalEditing reports whether the terminal is handed to another program
// ($EDITOR, the image viewer).
func (h *syncHost) ExternalEditing() bool { return h.editing.Load() }

// hostMsg carries one host request to Update.
type hostMsg struct{ msg tea.Msg }

// listenHostCmd waits for the next host request. It returns nil (ending the
// loop) once the program is done.
func listenHostCmd(h *syncHost) tea.Cmd {
	if h == nil {
		return nil
	}
	return func() tea.Msg {
		msg := h.next()
		if msg == nil {
			return nil
		}
		return hostMsg{msg: msg}
	}
}
