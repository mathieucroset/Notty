package syncer

import "time"

// Clock abstracts time so tests can drive the syncer's timers.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a pending AfterFunc call.
type Timer interface {
	// Stop prevents the call if it has not run yet and reports whether it
	// did so.
	Stop() bool
}

// RealClock returns a Clock backed by the time package.
func RealClock() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
