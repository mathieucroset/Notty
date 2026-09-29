//go:build !unix

package gitsync

import "os/exec"

// killGroupOnCancel keeps exec's default cancellation (kill git only) on
// platforms without Unix process groups.
func killGroupOnCancel(cmd *exec.Cmd) {}
