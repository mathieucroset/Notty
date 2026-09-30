package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func lockPath(root string) string { return filepath.Join(root, ".notty", "lock") }

func writeLock(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".notty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(root), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readLock(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(lockPath(root))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	return string(b)
}

// deadPid returns a PID that is not running.
func deadPid(t *testing.T) int {
	t.Helper()
	for _, pid := range []int{999999, 999998, 987654, 876543} {
		if !processAlive(pid) {
			return pid
		}
	}
	t.Skip("no dead PID found")
	return 0
}

func ours() string { return fmt.Sprintf("%d %s\n", os.Getpid(), shortHostname()) }

func TestAcquireLock(t *testing.T) {
	root := t.TempDir()
	l, err := AcquireLock(root, 0)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if got := readLock(t, root); got != ours() {
		t.Errorf("lock content = %q, want %q", got, ours())
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(lockPath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("lock file still present after Release: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Errorf("second Release: %v", err)
	}
	var nilLock *Lock
	if err := nilLock.Release(); err != nil {
		t.Errorf("nil Release: %v", err)
	}
	// Reacquiring after release works.
	l2, err := AcquireLock(root, 0)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	_ = l2.Release()
}

func TestAcquireLockHeld(t *testing.T) {
	host := shortHostname()
	tests := []struct {
		name     string
		content  string // "" means held by this process via AcquireLock
		wantPid  int
		wantHost string
	}{
		{"second holder in this process", "", os.Getpid(), host},
		{"live process on this host", fmt.Sprintf("%d %s\n", os.Getppid(), host), os.Getppid(), host},
		{"dead pid on another host", "999999 otherhost\n", 999999, "otherhost"},
		{"host is rest of line", "  999999 my other laptop \n", 999999, "my other laptop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.content == "" {
				l, err := AcquireLock(root, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = l.Release() }()
			} else {
				writeLock(t, root, tt.content)
			}
			before := readLock(t, root)
			_, err := AcquireLock(root, 0)
			var el ErrLocked
			if !errors.As(err, &el) {
				t.Fatalf("AcquireLock err = %v, want ErrLocked", err)
			}
			if el.Pid != tt.wantPid || el.Host != tt.wantHost {
				t.Errorf("ErrLocked = %+v, want pid %d host %q", el, tt.wantPid, tt.wantHost)
			}
			if got := readLock(t, root); got != before {
				t.Errorf("lock file changed to %q", got)
			}
		})
	}
}

func TestAcquireLockStale(t *testing.T) {
	tests := []struct {
		name    string
		content func(t *testing.T) string
		age     time.Duration
	}{
		{"dead pid on this host", func(t *testing.T) string {
			return fmt.Sprintf("%d %s\n", deadPid(t), shortHostname())
		}, 0},
		{"corrupt and old", func(*testing.T) string { return "garbage" }, time.Minute},
		{"empty and old", func(*testing.T) string { return "" }, time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeLock(t, root, tt.content(t))
			if tt.age > 0 {
				old := time.Now().Add(-tt.age)
				if err := os.Chtimes(lockPath(root), old, old); err != nil {
					t.Fatal(err)
				}
			}
			l, err := AcquireLock(root, 0)
			if err != nil {
				t.Fatalf("AcquireLock over stale lock: %v", err)
			}
			defer func() { _ = l.Release() }()
			if got := readLock(t, root); got != ours() {
				t.Errorf("lock content = %q, want %q", got, ours())
			}
		})
	}
}

func TestAcquireLockDanglingSymlinkIsStale(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".notty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "nowhere"), lockPath(root)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	done := make(chan struct{})
	var l *Lock
	var err error
	go func() {
		defer close(done)
		l, err = AcquireLock(root, time.Second)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("AcquireLock spun on a dangling symlink")
	}
	if err != nil {
		t.Fatalf("AcquireLock over dangling symlink: %v", err)
	}
	defer func() { _ = l.Release() }()
	fi, err := os.Lstat(lockPath(root))
	if err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("lock is not a regular file: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "nowhere")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("lock was written through the symlink: %v", err)
	}
	if got := readLock(t, root); got != ours() {
		t.Errorf("lock content = %q", got)
	}
}

func TestAcquireLockFreshCorruptIsHeld(t *testing.T) {
	root := t.TempDir()
	writeLock(t, root, "") // another process may be mid-write
	_, err := AcquireLock(root, 0)
	var el ErrLocked
	if !errors.As(err, &el) {
		t.Fatalf("AcquireLock err = %v, want ErrLocked", err)
	}
}

func TestAcquireLockWaits(t *testing.T) {
	t.Run("times out", func(t *testing.T) {
		root := t.TempDir()
		l, err := AcquireLock(root, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = l.Release() }()
		start := time.Now()
		_, err = AcquireLock(root, 300*time.Millisecond)
		elapsed := time.Since(start)
		var el ErrLocked
		if !errors.As(err, &el) {
			t.Fatalf("err = %v, want ErrLocked", err)
		}
		if elapsed < 300*time.Millisecond || elapsed > 2*time.Second {
			t.Errorf("waited %v, want about 300ms", elapsed)
		}
	})
	t.Run("acquires once released", func(t *testing.T) {
		root := t.TempDir()
		l, err := AcquireLock(root, 0)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			time.Sleep(150 * time.Millisecond)
			done <- l.Release()
		}()
		l2, err := AcquireLock(root, 5*time.Second)
		if err != nil {
			t.Fatalf("AcquireLock while waiting: %v", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("Release: %v", err)
		}
		if got := readLock(t, root); got != ours() {
			t.Errorf("lock content = %q", got)
		}
		_ = l2.Release()
	})
}

func TestReleaseOnlyOwnLock(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"other pid", "12345 " + shortHostname() + "\n"},
		{"other host, same pid", fmt.Sprintf("%d otherhost\n", os.Getpid())},
		{"corrupt", "garbage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			l, err := AcquireLock(root, 0)
			if err != nil {
				t.Fatal(err)
			}
			writeLock(t, root, tt.content) // someone took the lock over
			if err := l.Release(); err != nil {
				t.Fatalf("Release: %v", err)
			}
			if got := readLock(t, root); got != tt.content {
				t.Errorf("lock content = %q, want %q kept", got, tt.content)
			}
		})
	}
}

func TestErrLockedError(t *testing.T) {
	tests := []struct {
		err  ErrLocked
		want string
	}{
		{ErrLocked{Pid: 123, Host: "laptop"}, "vault is open in another Notty (pid 123 on laptop)"},
		{ErrLocked{Pid: 123}, "vault is open in another Notty (pid 123)"},
		{ErrLocked{}, "vault is open in another Notty"},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("Error() = %q, want %q", got, tt.want)
		}
	}
}

// TestAcquireLockRetriesDeletePending covers Windows' answer for a lock file
// being deleted by the previous holder: opening or creating it fails with
// access denied until its last handle closes. That is a busy lock, not a
// failure, until the wait runs out.
func TestAcquireLockRetriesDeletePending(t *testing.T) {
	denied := fmt.Errorf("open lock: %w", fs.ErrPermission)
	tests := []struct {
		name          string
		pending       bool // the platform reports delete-pending as access denied
		createDenials int  // createLock fails this many times, then works
		readDenials   int  // readLockFile fails this many times (after an ErrExist)
		wait          time.Duration
		wantErr       error // nil: acquired
	}{
		{name: "create denied then free", pending: true, createDenials: 2, wait: time.Second},
		{name: "read denied then free", pending: true, readDenials: 2, wait: time.Second},
		{name: "denied past the wait", pending: true, createDenials: 1000, wait: 250 * time.Millisecond, wantErr: fs.ErrPermission},
		{name: "not delete-pending: fails at once", pending: false, createDenials: 1, wait: time.Second, wantErr: fs.ErrPermission},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creates, reads := 0, 0
			exist := tt.readDenials > 0
			swapLockOps(t,
				func(path, content string) error {
					creates++
					if creates <= tt.createDenials {
						return denied
					}
					if exist && reads <= tt.readDenials {
						return fs.ErrExist
					}
					return createLock(path, content)
				},
				func(path string) (ErrLocked, []byte, error) {
					reads++
					if reads <= tt.readDenials {
						return ErrLocked{}, nil, denied
					}
					return readLockFile(path)
				},
				func(err error) bool { return tt.pending && errors.Is(err, fs.ErrPermission) },
			)
			start := time.Now()
			l, err := AcquireLock(t.TempDir(), tt.wait)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if !tt.pending && time.Since(start) > tt.wait/2 {
					t.Errorf("waited %v for a hard failure", time.Since(start))
				}
				return
			}
			if err != nil {
				t.Fatalf("AcquireLock: %v", err)
			}
			_ = l.Release()
		})
	}
}

func swapLockOps(t *testing.T, create func(string, string) error, read func(string) (ErrLocked, []byte, error), pending func(error) bool) {
	t.Helper()
	oc, or, op := createLockFn, readLockFn, deletePending
	createLockFn, readLockFn, deletePending = create, read, pending
	t.Cleanup(func() { createLockFn, readLockFn, deletePending = oc, or, op })
}
