//go:build windows

package imageviewer

import "time"

// resizePoll is how often the console size is checked: Windows has no
// SIGWINCH.
const resizePoll = 250 * time.Millisecond

// watchResize polls size and reports changes until stop is called.
func watchResize(size func() (int, int, error)) (<-chan struct{}, func()) {
	resized := make(chan struct{}, 1)
	quit := make(chan struct{})
	go func() {
		lastW, lastH, _ := size()
		t := time.NewTicker(resizePoll)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				w, h, err := size()
				if err != nil || (w == lastW && h == lastH) {
					continue
				}
				lastW, lastH = w, h
				select {
				case resized <- struct{}{}:
				default:
				}
			case <-quit:
				return
			}
		}
	}()
	return resized, func() { close(quit) }
}
