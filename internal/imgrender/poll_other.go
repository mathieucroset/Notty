//go:build !unix

package imgrender

import (
	"errors"
	"time"
)

// pollSupported is false: without a way to bound a Read on a raw fd, Detect
// does not query the tty (spec §6.3 uses only config and env on Windows).
const pollSupported = false

func waitReadable(uintptr, time.Duration) (bool, error) {
	return false, errors.New("polling not supported on this platform")
}
