//go:build unix && !darwin

package imgrender

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

const pollSupported = true

// waitReadable waits up to timeout for fd to become readable (or hung up,
// so the following Read returns EOF instead of blocking).
func waitReadable(fd uintptr, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		ms := max(0, int((time.Until(deadline)+time.Millisecond-1)/time.Millisecond))
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} //nolint:gosec // fds fit in int32
		n, err := unix.Poll(fds, ms)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return false, err //nolint:wrapcheck // internal
		}
		return n > 0 && fds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0, nil
	}
}
