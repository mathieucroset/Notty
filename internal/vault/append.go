package vault

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// InboxName is the file quick notes are appended to, in the chosen folder.
const InboxName = "Inbox.md"

// inboxHeader starts a new Inbox note.
const inboxHeader = "# Inbox\n\n"

// ValidateFolderPath checks a user-typed vault-relative folder path and
// returns it in clean "/"-separated form ("" is the vault root). Each
// segment must be non-empty, neither "." nor "..", and already a valid
// name (unchanged by the file name rules); the result must not be, or lie
// inside, a reserved location such as .trash or attachments. It fails with
// ErrInvalidName or ErrInvalidPath.
func ValidateFolderPath(rel string) (string, error) {
	s := strings.Trim(filepath.ToSlash(strings.TrimSpace(rel)), "/")
	if s == "" {
		return "", nil
	}
	segs := strings.Split(s, "/")
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." || sanitizeName(seg) != seg {
			return "", fmt.Errorf("vault: folder %q: %w %q", rel, ErrInvalidName, seg)
		}
	}
	folder := strings.Join(segs, "/")
	if inReserved(folder) {
		return "", fmt.Errorf("vault: folder %q: reserved location: %w", rel, ErrInvalidPath)
	}
	return folder, nil
}

// AppendToInbox appends line to <folder>/Inbox.md and returns that note's
// path. folder is validated with ValidateFolderPath and created if missing;
// a missing Inbox is created with an "# Inbox" heading. The line is added
// with a single append, so concurrent callers never drop each other's
// lines. An Inbox (or folder) that is a symlink or not a regular file
// (folder) is refused.
func (v *Vault) AppendToInbox(folder, line string) (string, error) {
	dir, err := ValidateFolderPath(folder)
	if err != nil {
		return "", err
	}
	if err := v.mkdirReal(dir); err != nil {
		return "", err
	}
	rel := path.Join(dir, InboxName)
	text := strings.TrimSuffix(line, "\n") + "\n"
	// O_EXCL creates the Inbox only if nobody else has; O_APPEND keeps a
	// line appended by a concurrent caller in between from being
	// overwritten.
	f, err := os.OpenFile(v.Abs(rel), os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o644)
	switch {
	case err == nil:
		if err := writeClose(f, inboxHeader+text); err != nil {
			return "", fmt.Errorf("vault: append to %q: %w", rel, err)
		}
		syncDir(filepath.Dir(v.Abs(rel)))
		return rel, nil
	case errors.Is(err, fs.ErrExist):
		return rel, v.appendText(rel, text)
	default:
		// On Windows a folder in the way fails with "Access is denied".
		if fi, serr := os.Lstat(v.Abs(rel)); serr == nil && !fi.Mode().IsRegular() {
			return "", fmt.Errorf("vault: append to %q: not a regular file: %w", rel, ErrInvalidPath)
		}
		return "", fmt.Errorf("vault: append to %q: %w", rel, err)
	}
}

// AppendToNote appends text (a "\n" is added if it does not end with one)
// to the existing note at rel, starting it on a new line. The note must be
// a regular file: a symlink, folder or reserved path is refused with
// ErrInvalidPath.
func (v *Vault) AppendToNote(rel, text string) error {
	c := clean(rel)
	if c == "" || inReserved(c) {
		return fmt.Errorf("vault: append to %q: %w", c, ErrInvalidPath)
	}
	if text == "" {
		return nil
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return v.appendText(c, text)
}

// appendText appends text, which ends with "\n", to the regular file at the
// clean path rel in a single write, preceded by "\n" when the file is
// non-empty and does not end with one; then it syncs the file.
func (v *Vault) appendText(rel, text string) error {
	abs := v.Abs(rel)
	before, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("vault: append to %q: %w", rel, err)
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("vault: append to %q: not a regular file: %w", rel, ErrInvalidPath)
	}
	f, err := os.OpenFile(abs, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("vault: append to %q: %w", rel, err)
	}
	// The file opened must be the one checked: a symlink swapped in
	// between would otherwise be followed.
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("vault: append to %q: %w", rel, err)
	}
	if !os.SameFile(before, fi) {
		_ = f.Close()
		return fmt.Errorf("vault: append to %q: file replaced: %w", rel, ErrInvalidPath)
	}
	if size := fi.Size(); size > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, size-1); err != nil && !errors.Is(err, io.EOF) {
			_ = f.Close()
			return fmt.Errorf("vault: append to %q: %w", rel, err)
		}
		if last[0] != '\n' {
			text = "\n" + text
		}
	}
	if err := writeClose(f, text); err != nil {
		return fmt.Errorf("vault: append to %q: %w", rel, err)
	}
	return nil
}

// writeClose writes s to f in one call, syncs and closes it.
func writeClose(f *os.File, s string) error {
	if _, err := f.WriteString(s); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// mkdirReal creates the clean folder rel and its parents. Existing path
// components must be real folders: a symlink or file is refused with
// ErrInvalidPath, so nothing is written outside the vault.
func (v *Vault) mkdirReal(rel string) error {
	if rel == "" {
		return nil
	}
	cur := ""
	for _, seg := range strings.Split(rel, "/") {
		cur = path.Join(cur, seg)
		fi, err := os.Lstat(v.Abs(cur))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return fmt.Errorf("vault: create folder %q: %w", rel, err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("vault: create folder %q: %q is not a folder: %w", rel, cur, ErrInvalidPath)
		}
	}
	if err := os.MkdirAll(v.Abs(rel), 0o755); err != nil {
		return fmt.Errorf("vault: create folder %q: %w", rel, err)
	}
	return nil
}
