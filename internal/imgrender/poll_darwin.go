//go:build darwin

package imgrender

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

const pollSupported = true

// waitReadable waits up to timeout for fd to become readable. macOS poll(2)
// does not work on ttys, so this uses select(2).
func waitReadable(fd uintptr, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		var set unix.FdSet
		set.Set(int(fd)) //nolint:gosec // fds fit in int
		tv := unix.NsecToTimeval(max(0, time.Until(deadline)).Nanoseconds())
		n, err := unix.Select(int(fd)+1, &set, nil, nil, &tv) //nolint:gosec // fds fit in int
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return false, err //nolint:wrapcheck // internal
		}
		return n > 0, nil
	}
}
