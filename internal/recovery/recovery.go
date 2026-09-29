// Package recovery keeps what a crash must not lose (spec §9): a
// thread-safe snapshot of the open note's unsaved text, written after a
// panic to .notty/recovery/<note-slug>-<timestamp>.md in the vault, and the
// listing of those files offered back on the next start.
package recovery

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mathieucroset/notty/internal/attach"
)

// Dir is the recovery folder, relative to the vault root. vault's
// EnsureGitignore keeps it out of git.
const Dir = ".notty/recovery"

// stampLayout is the timestamp in recovery file names.
const stampLayout = "20060102-150405"

// headerPrefix starts the first line of a recovery file, which names the
// note the text was recovered from as a quoted Go string.
const headerPrefix = "<!-- notty-recovery "

const headerSuffix = " -->"

// Snapshot is the open note's path and unsaved text, updated by the app as
// the buffer changes and read after a panic from another goroutine. It
// also records the first panic caught. A nil *Snapshot ignores writes.
type Snapshot struct {
	mu      sync.Mutex
	root    string
	rel     string
	content string
	dirty   bool

	panicked bool
	value    any
	stack    []byte
}

// Set records the buffer: root is the vault, rel the note's vault-relative
// path, content its text and dirty whether it has unsaved edits.
func (s *Snapshot) Set(root, rel, content string, dirty bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.root, s.rel, s.content, s.dirty = root, rel, content, dirty
}

// Get returns the last buffer recorded by Set.
func (s *Snapshot) Get() (root, rel, content string, dirty bool) {
	if s == nil {
		return "", "", "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.root, s.rel, s.content, s.dirty
}

// RecordPanic remembers a panic value and its stack; only the first one
// is kept.
func (s *Snapshot) RecordPanic(v any, stack []byte) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.panicked {
		return
	}
	s.panicked, s.value, s.stack = true, v, stack
}

// Panic returns the panic recorded by RecordPanic, if any.
func (s *Snapshot) Panic() (v any, stack []byte, ok bool) {
	if s == nil {
		return nil, nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.stack, s.panicked
}

// SaveBuffer writes the snapshot's unsaved text to a recovery file and
// returns its path. It writes nothing, returning "", when there is no
// vault or note, the buffer is clean, or the note on disk already holds
// the text.
func (s *Snapshot) SaveBuffer(now time.Time) (string, error) {
	root, rel, content, dirty := s.Get()
	if !dirty || root == "" || rel == "" {
		return "", nil
	}
	if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))); err == nil && string(b) == content {
		return "", nil
	}
	return Write(root, rel, content, now)
}

// File is a recovery file.
type File struct {
	// Name is the file name inside Dir.
	Name string
	// Note is the vault-relative path of the note the text came from (the
	// file name itself for a file without a header).
	Note string
	// Time is when the text was recovered.
	Time time.Time
	seq  int
}

// Write saves content, recovered from the note rel, to
// <root>/.notty/recovery/<slug>-<YYYYMMDD-HHMMSS>.md atomically (a
// -2, -3, ... suffix avoids an existing file) and returns its path.
func Write(root, rel, content string, now time.Time) (string, error) {
	dir := filepath.Join(root, filepath.FromSlash(Dir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("recovery: %w", err)
	}
	base := attach.Slug(rel) + "-" + now.Format(stampLayout)
	for i := 1; i <= 1000; i++ {
		name := base + ".md"
		if i > 1 {
			name = base + "-" + strconv.Itoa(i) + ".md"
		}
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := writeAtomic(p, header(rel)+content); err != nil {
			return "", fmt.Errorf("recovery: %w", err)
		}
		return p, nil
	}
	return "", fmt.Errorf("recovery: no free name for %s", base)
}

// header is the first line of a recovery file for the note rel.
func header(rel string) string {
	return headerPrefix + strconv.Quote(rel) + headerSuffix + "\n"
}

// writeAtomic writes p through a synced temporary file renamed into place.
func writeAtomic(p, content string) error {
	tmp := p + ".notty-tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
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
	if err == nil {
		err = os.Rename(tmp, p)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// nameRE splits a recovery file name into its slug, timestamp and
// collision number.
var nameRE = regexp.MustCompile(`^.+-(\d{8}-\d{6})(?:-(\d+))?\.md$`)

// List returns the recovery files of the vault at root, oldest first. A
// missing folder has none.
func List(root string) ([]File, error) {
	dir := filepath.Join(root, filepath.FromSlash(Dir))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("recovery: %w", err)
	}
	var files []File
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.EqualFold(path.Ext(e.Name()), ".md") {
			continue
		}
		f := File{Name: e.Name(), Note: e.Name()}
		if m := nameRE.FindStringSubmatch(e.Name()); m != nil {
			if t, err := time.ParseInLocation(stampLayout, m[1], time.Local); err == nil {
				f.Time = t
			}
			f.seq, _ = strconv.Atoi(m[2])
		}
		if f.Time.IsZero() {
			if info, err := e.Info(); err == nil {
				f.Time = info.ModTime()
			}
		}
		if note, ok := readHeader(filepath.Join(dir, e.Name())); ok {
			f.Note = note
		}
		files = append(files, f)
	}
	slices.SortFunc(files, func(a, b File) int {
		if c := a.Time.Compare(b.Time); c != 0 {
			return c
		}
		if a.seq != b.seq {
			return a.seq - b.seq
		}
		return strings.Compare(a.Name, b.Name)
	})
	return files, nil
}

// readHeader returns the note named by the header of the file at p.
func readHeader(p string) (string, bool) {
	fh, err := os.Open(p)
	if err != nil {
		return "", false
	}
	defer func() { _ = fh.Close() }()
	buf := make([]byte, 4096)
	n, _ := fh.Read(buf)
	note, _, ok := splitHeader(string(buf[:n]))
	return note, ok
}

// splitHeader separates a recovery file's header from its text.
func splitHeader(s string) (note, rest string, ok bool) {
	line, rest, found := strings.Cut(s, "\n")
	if !found || !strings.HasPrefix(line, headerPrefix) || !strings.HasSuffix(line, headerSuffix) {
		return "", s, false
	}
	note, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(line, headerPrefix), headerSuffix))
	if err != nil || note == "" {
		return "", s, false
	}
	return note, rest, true
}

// Read returns the recovered text of f, without its header.
func Read(root string, f File) (string, error) {
	b, err := os.ReadFile(filePath(root, f))
	if err != nil {
		return "", fmt.Errorf("recovery: %w", err)
	}
	_, rest, _ := splitHeader(string(b))
	return rest, nil
}

// Remove deletes f.
func Remove(root string, f File) error {
	if err := os.Remove(filePath(root, f)); err != nil {
		return fmt.Errorf("recovery: %w", err)
	}
	return nil
}

func filePath(root string, f File) string {
	return filepath.Join(root, filepath.FromSlash(Dir), filepath.Base(f.Name))
}
