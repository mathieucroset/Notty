//go:build unix

package gitsync_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
)

// KillGroupOnCancel kills grandchildren too, so a canceled helper whose
// child holds the output pipe still returns promptly.
func TestKillGroupOnCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & wait")
	gitsync.KillGroupOnCancel(cmd)
	start := time.Now()
	if _, err := cmd.Output(); err == nil {
		t.Fatal("canceled command succeeded")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("canceled command took %v", d)
	}
}

// On timeout the whole process group is killed: the ssh helper git spawned
// must not outlive the call.
func TestNetworkTimeoutLeavesNoSSHOrphans(t *testing.T) {
	gittest.Isolate(t)
	pids := filepath.Join(t.TempDir(), "pids")
	t.Setenv("GIT_SSH_COMMAND", "echo $$ >> '"+pids+"'; sleep 60; true")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _, err := gitsync.LsRemote(ctx, "ssh://git@example.invalid/notes.git")
	if !errors.Is(err, gitsync.ErrNetwork) {
		t.Fatalf("err = %v, want ErrNetwork", err)
	}

	b, err := os.ReadFile(pids)
	if err != nil {
		t.Fatalf("ssh helper never started: %v", err)
	}
	for _, f := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(f)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for alive(pid) {
			if time.Now().After(deadline) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				t.Fatalf("ssh helper %d survived the timeout", pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// alive reports whether pid is a running (non-zombie) process.
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // no procfs: signal 0 succeeded, assume alive
	}
	// Format: pid (comm) state ...
	s := string(stat)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
		return s[i+2] != 'Z'
	}
	return true
}
