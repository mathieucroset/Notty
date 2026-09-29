package app

import (
	"sync"
	"sync/atomic"
)

// fileLocks serializes read-modify-write cycles on one file across the
// commands running concurrently (task toggles, saves).
var fileLocks sync.Map // abs path -> *sync.Mutex

// lockFile locks the file at abs and returns the unlock func.
func lockFile(abs string) (unlock func()) {
	mu, _ := fileLocks.LoadOrStore(abs, &sync.Mutex{})
	l := mu.(*sync.Mutex)
	l.Lock()
	return l.Unlock
}

// orderedSaver writes snapshots in the order they were taken: each
// snapshot gets a ticket on the UI goroutine, and a write whose ticket is
// older than the last one written is skipped, so a slow command can never
// overwrite newer state with an older snapshot.
type orderedSaver struct {
	tickets atomic.Uint64
	mu      sync.Mutex
	written uint64
}

// ticket numbers a snapshot. Call it when the snapshot is taken.
func (s *orderedSaver) ticket() uint64 { return s.tickets.Add(1) }

// save runs write for the snapshot numbered seq unless a newer snapshot
// was already written.
func (s *orderedSaver) save(seq uint64, write func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seq <= s.written {
		return nil
	}
	// Recorded even on failure: retrying an older snapshot later would
	// only write staler state.
	s.written = seq
	return write()
}
