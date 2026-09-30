//go:build !unix && !windows

package main

import (
	"os/exec"
	"testing"
)

func checkDetached(*testing.T, *exec.Cmd) {}
