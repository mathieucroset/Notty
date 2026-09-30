//go:build unix

package main

import (
	"os/exec"
	"testing"
)

func checkDetached(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if !cmd.SysProcAttr.Setsid {
		t.Error("child does not start a new session")
	}
}
