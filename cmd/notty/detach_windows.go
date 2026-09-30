//go:build windows

package main

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach starts cmd in a new process group, so a Ctrl+C in the terminal
// does not reach it, with a hidden console rather than none: without
// DETACHED_PROCESS, the git commands it runs share that hidden console
// instead of each flashing a window of its own (amendment A5).
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
}
