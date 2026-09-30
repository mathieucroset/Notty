package main

import (
	"fmt"
	"os/exec"
)

// startDetached starts name with args in the background and returns
// without waiting for it: the child runs in its own session (Unix) or
// process group without a console window (Windows), with its standard
// input and output on the null device, so it outlives this process and
// its terminal.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	// Stdin, Stdout and Stderr stay nil: exec connects them to the null
	// device.
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release %s: %w", name, err)
	}
	return nil
}
