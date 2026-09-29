package vault

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// lockPollInterval is how often AcquireLock retries a held lock.
const lockPollInterval = 100 * time.Millisecond

// corruptLockGrace is how long an unparsable lock file is treated as held
// (its creator may still be writing it) before it counts as stale.
const corruptLockGrace = 2 * time.Second

// gitignoreEntries are the lines EnsureGitignore guarantees (spec §3).
var gitignoreEntries = []string{
	".DS_Store",
	"Thumbs.db",
	"desktop.ini",
	"*" + tmpSuffix,
	".notty/recovery/",
	".notty/lock",
}

// Lock is a held vault instance lock: the file .notty/lock (spec §9).
type Lock struct {
	path    string
	content string // what we wrote, "pid host\n"
}

// ErrLocked means another Notty process holds the vault lock. Pid is 0
// and Host empty when the lock file could not be parsed.
type ErrLocked struct {
	Pid  int
	Host string
}

func (e ErrLocked) Error() string {
	switch {
	case e.Pid > 0 && e.Host != "":
		return fmt.Sprintf("vault is open in another Notty (pid %d on %s)", e.Pid, e.Host)
	case e.Pid > 0:
		return fmt.Sprintf("vault is open in another Notty (pid %d)", e.Pid)
	default:
		return "vault is open in another Notty"
	}
}

// AcquireLock takes the instance lock of the vault at root by creating
// .notty/lock exclusively, containing "<pid> <host>\n". If another process
// holds it, AcquireLock retries every 100ms until wait has elapsed and then
// returns ErrLocked (wait 0 means a single attempt).
//
// A lock is stale, and taken over, when it names this host and a process
// that is no longer running, or when it has been unparsable for more than a
// couple of seconds. Takeover re-checks the file just before removing it;
// two processes taking over the same stale lock at the same instant could
// still both succeed.
func AcquireLock(root string, wait time.Duration) (*Lock, error) {
	dir := filepath.Join(root, ".notty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("vault: lock: %w", err)
	}
	host := shortHostname()
	l := &Lock{
		path:    filepath.Join(dir, "lock"),
		content: fmt.Sprintf("%d %s\n", os.Getpid(), host),
	}
	deadline := time.Now().Add(wait)
	vanished := false
	for {
		err := createLock(l.path, l.content)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("vault: lock: %w", err)
		}
		held, seen, err := readLockFile(l.path)
		switch {
		case errors.Is(err, fs.ErrNotExist) && !vanished:
			// Released between our attempt and the read: retry at once,
			// but only once so a dangling symlink cannot spin forever.
			vanished = true
			continue
		case errors.Is(err, fs.ErrNotExist):
			held = ErrLocked{}
		case err != nil:
			return nil, fmt.Errorf("vault: lock: %w", err)
		case isStale(l.path, held, host):
			if err := removeIfUnchanged(l.path, seen); err != nil {
				return nil, fmt.Errorf("vault: lock: remove stale lock: %w", err)
			}
			continue
		}
		vanished = false
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, held
		}
		time.Sleep(min(lockPollInterval, remaining))
	}
}

// createLock creates path exclusively and writes content to it.
func createLock(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

// readLockFile returns the holder named by the lock file and its raw
// content. An unparsable file yields a zero ErrLocked and no error.
func readLockFile(path string) (ErrLocked, []byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ErrLocked{}, nil, err
	}
	fields := strings.Fields(string(b))
	if len(fields) != 2 {
		return ErrLocked{}, b, nil
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return ErrLocked{}, b, nil
	}
	return ErrLocked{Pid: pid, Host: fields[1]}, b, nil
}

// isStale reports whether the lock held by holder can be taken over.
func isStale(path string, holder ErrLocked, host string) bool {
	if holder.Pid == 0 { // unparsable
		fi, err := os.Lstat(path)
		return err == nil && time.Since(fi.ModTime()) > corruptLockGrace
	}
	return holder.Host == host && !processAlive(holder.Pid)
}

// removeIfUnchanged removes path if it still holds exactly seen. A file that
// changed or vanished meanwhile is left alone without error.
func removeIfUnchanged(path string, seen []byte) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !bytes.Equal(b, seen)) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Release gives up the lock. The file is removed only if it still contains
// what this Lock wrote, so a lock taken over by another process survives.
// Releasing twice, or a nil Lock, is a no-op.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	if err := removeIfUnchanged(l.path, []byte(l.content)); err != nil {
		return fmt.Errorf("vault: release lock: %w", err)
	}
	return nil
}

// EnsureGitignore makes sure the .gitignore at root lists the OS junk
// files, save temp files, .notty/recovery/ and .notty/lock. Missing lines
// are appended (matching ignores surrounding whitespace); existing content
// is kept byte for byte, and the file ends with a newline, CRLF if it
// already uses CRLF. It reports whether the file was written.
func EnsureGitignore(root string) (changed bool, err error) {
	p := filepath.Join(root, ".gitignore")
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("vault: read .gitignore: %w", err)
	}
	content := string(b)
	have := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, e := range gitignoreEntries {
		if !have[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	var sb strings.Builder
	sb.WriteString(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		sb.WriteString(nl)
	}
	for _, e := range missing {
		sb.WriteString(e + nl)
	}
	perm, keepMode := fs.FileMode(0o644), false
	if fi, err := os.Stat(p); err == nil {
		perm, keepMode = fi.Mode().Perm(), true
	}
	tmp := p + tmpSuffix
	if err := writeSynced(tmp, sb.String(), perm, keepMode); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("vault: write .gitignore: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("vault: write .gitignore: %w", err)
	}
	syncDir(root)
	return true, nil
}
