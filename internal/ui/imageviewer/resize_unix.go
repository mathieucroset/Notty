//go:build !windows

package imageviewer

import (
	"os"
	"os/signal"
	"syscall"
)

// watchResize reports SIGWINCH until stop is called. Resizes are coalesced:
// a pending notification is not duplicated.
func watchResize(func() (int, int, error)) (<-chan struct{}, func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	resized := make(chan struct{}, 1)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-sig:
				select {
				case resized <- struct{}{}:
				default:
				}
			case <-quit:
				return
			}
		}
	}()
	return resized, func() {
		signal.Stop(sig)
		close(quit)
	}
}
