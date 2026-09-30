//go:build !unix && !windows

package main

import "os/exec"

// detach does nothing where there are no sessions or process groups: the
// child still runs without waiting and with null standard streams.
func detach(*exec.Cmd) {}
