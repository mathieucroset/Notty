//go:build windows

package main

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func checkDetached(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	const want = windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP
	if got := cmd.SysProcAttr.CreationFlags; got&want != want {
		t.Errorf("CreationFlags = %#x, want %#x set", got, want)
	}
	if cmd.SysProcAttr.CreationFlags&windows.DETACHED_PROCESS != 0 {
		t.Error("DETACHED_PROCESS set: each git call would open a console")
	}
}
