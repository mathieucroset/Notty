//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach makes cmd start a new session, without a controlling terminal, so
// closing the terminal does not stop it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
