//go:build unix

package gitsync

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// killGroupOnCancel runs git in its own process group and, when the context
// is done, kills the whole group so ssh and other helpers git spawned die
// with it instead of holding the output pipes open.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
