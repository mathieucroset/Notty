package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// maxCollisions bounds the " 2", " 3", ... suffix search.
const maxCollisions = 10000

// closeFile closes f; tests make it fail.
var closeFile = (*os.File).Close

// Read returns the content of the file at rel.
func (v *Vault) Read(rel string) (string, error) {
	b, err := os.ReadFile(v.Abs(rel))
	if err != nil {
		return "", fmt.Errorf("vault: read %q: %w", clean(rel), err)
	}
	return string(b), nil
}

// Save writes content to rel atomically: it writes <file>.notty-tmp in the
// same directory, fsyncs it and renames it into place (spec §9). Missing
// parent directories are created. An existing file's permissions are kept.
// If rel is a symlink, the rename replaces the link itself with a regular
// file (atomically); the link's former target is left untouched.
func (v *Vault) Save(rel, content string) error {
	c := clean(rel)
	if c == "" {
		return fmt.Errorf("vault: save vault root: %w", ErrInvalidPath)
	}
	dst := v.Abs(c)
	perm, keepMode := fs.FileMode(0o644), false
	if fi, err := os.Stat(dst); err == nil {
		if fi.IsDir() {
			return fmt.Errorf("vault: save %q: is a directory: %w", c, ErrInvalidPath)
		}
		perm, keepMode = fi.Mode().Perm(), true
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("vault: save %q: create parents: %w", c, err)
	}
	tmp := dst + tmpSuffix
	if err := writeSynced(tmp, content, perm, keepMode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("vault: save %q: %w", c, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("vault: save %q: %w", c, err)
	}
	syncDir(dir)
	return nil
}

// writeSynced writes and fsyncs name. New files get perm filtered by the
// umask; with keepMode, perm is applied exactly (to mirror the file being
// replaced).
func writeSynced(name, content string, perm fs.FileMode, keepMode bool) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if keepMode {
		if err := f.Chmod(perm); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

// syncDir makes a rename durable. It is best-effort: some platforms cannot
// fsync directories, and the data itself is already synced.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// CreateNote creates a note titled title in folder (vault-relative, "" for
// the root), creating the folder if needed. The file name follows spec §3,
// with " 2", " 3", ... appended on collision. It returns the note's path.
// A folder inside a hidden location (the top-level hidden folders, dot
// folders) is rejected with ErrInvalidPath.
func (v *Vault) CreateNote(folder, title string) (string, error) {
	// The heading is one line: control characters (newlines, tabs) become
	// spaces. The file name is derived from the raw title, exactly as
	// FileNameFromTitle does.
	heading := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, title))
	if heading == "" {
		heading = untitled
	}
	dir := clean(folder)
	if inReserved(dir) {
		return "", fmt.Errorf("vault: create note in %q: reserved location: %w", dir, ErrInvalidPath)
	}
	if err := os.MkdirAll(v.Abs(dir), 0o755); err != nil {
		return "", fmt.Errorf("vault: create note in %q: %w", dir, err)
	}
	base := noteBase(title)
	if base == "" {
		base = untitled
	}
	rel, err := v.claim(dir, base, noteExt, func(abs string) error {
		// O_EXCL reserves the name so concurrent creators cannot collide.
		f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		if err := closeFile(f); err != nil {
			// Release the name: claim would take the file for someone
			// else's and try the next name.
			_ = os.Remove(abs)
			return err
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("vault: create note %q: %w", heading, err)
	}
	if err := v.Save(rel, "# "+heading+"\n\n"); err != nil {
		_ = os.Remove(v.Abs(rel))
		return "", err // already prefixed and names the path
	}
	return rel, nil
}

// CreateFolder creates a folder named name (sanitized like note titles)
// inside parent, appending " 2", " 3", ... on collision; reserved names
// count as collisions. It returns the new folder's path. A parent inside a
// hidden location is rejected with ErrInvalidPath.
func (v *Vault) CreateFolder(parent, name string) (string, error) {
	dir := clean(parent)
	if inReserved(dir) {
		return "", fmt.Errorf("vault: create folder in %q: reserved location: %w", dir, ErrInvalidPath)
	}
	if err := os.MkdirAll(v.Abs(dir), 0o755); err != nil {
		return "", fmt.Errorf("vault: create folder in %q: %w", dir, err)
	}
	base := sanitizeName(name)
	if base == "" {
		base = untitled
	}
	rel, err := v.claim(dir, base, "", func(abs string) error {
		return os.Mkdir(abs, 0o755)
	})
	if err != nil {
		return "", fmt.Errorf("vault: create folder %q: %w", name, err)
	}
	return rel, nil
}

// claim calls create on dir/base+ext, then dir/base 2+ext, ... until create
// succeeds, skipping names that already exist or are reserved (so a folder
// named "attachments" at the root becomes "attachments 2"). It returns the
// claimed path. A name counts as taken when create fails and an entry
// exists there, whatever the error: on Windows, creating a file where a
// folder is fails with "Access is denied" (ERROR_ACCESS_DENIED), not
// fs.ErrExist.
func (v *Vault) claim(dir, base, ext string, create func(abs string) error) (string, error) {
	for i := 1; i <= maxCollisions; i++ {
		name := base + ext
		if i > 1 {
			name = base + " " + strconv.Itoa(i) + ext
		}
		if reserved(dir, name) {
			continue
		}
		rel := path.Join(dir, name)
		abs := v.Abs(rel)
		err := create(abs)
		if err == nil {
			return rel, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			if _, serr := os.Lstat(abs); serr != nil {
				return "", err
			}
		}
	}
	return "", fmt.Errorf("no free name for %q: %w", base+ext, ErrExists)
}

// Rename renames the note, folder or file at rel within its folder. newName
// is sanitized like a title and must not be a reserved name
// (ErrInvalidName). Notes always end in ".md": a user-typed ".md" in any
// case is normalized, and it is appended if omitted. It fails with
// ErrExists if the target is taken. Relative image links are rewritten as
// for Move.
func (v *Vault) Rename(rel, newName string) (string, error) {
	src := clean(rel)
	if src == "" {
		return "", fmt.Errorf("vault: rename vault root: %w", ErrInvalidPath)
	}
	fi, err := os.Lstat(v.Abs(src))
	if err != nil {
		return "", fmt.Errorf("vault: rename %q: %w", src, err)
	}
	name := sanitizeName(newName)
	if fi.Mode().IsRegular() && isNoteName(src) {
		if name = noteBase(newName); name != "" {
			name += noteExt
		}
	}
	dir := parentOf(src)
	if name == "" || reserved(dir, name) {
		return "", fmt.Errorf("vault: rename %q to %q: %w", src, newName, ErrInvalidName)
	}
	return v.moveAndRewrite(src, path.Join(dir, name))
}

// moveAndRewrite moves oldRel to newRel, then rewrites the relative image
// links of the moved note or of the notes inside the moved folder (see
// rewriteLinks). If only the rewrite fails, the entry has moved: the new
// path is returned together with the error.
func (v *Vault) moveAndRewrite(oldRel, newRel string) (string, error) {
	rel, err := v.move(oldRel, newRel)
	if err != nil {
		return "", err
	}
	if rel == oldRel {
		return rel, nil
	}
	return rel, v.rewriteLinks(oldRel, rel)
}

// Move moves the note, folder or file at rel into destFolder ("" for the
// root), creating destFolder if needed. It fails with ErrExists if the
// target is taken, and with ErrInvalidPath if destFolder is rel itself, lies
// inside rel, is not a folder, or is (inside) a reserved location such as
// .trash or attachments, or if the moved name is reserved there.
//
// A moved note's note-relative image links, or those of every note inside
// a moved folder, are rewritten so they keep pointing at the same files.
// If only that rewrite fails, the new path is returned with the error.
func (v *Vault) Move(rel, destFolder string) (string, error) {
	src, dest := clean(rel), clean(destFolder)
	if src == "" {
		return "", fmt.Errorf("vault: move vault root: %w", ErrInvalidPath)
	}
	if within(dest, src) {
		return "", fmt.Errorf("vault: move %q into itself (%q): %w", src, dest, ErrInvalidPath)
	}
	if inReserved(dest) || reserved(dest, path.Base(src)) {
		return "", fmt.Errorf("vault: move %q to %q: reserved location: %w", src, dest, ErrInvalidPath)
	}
	if fi, err := os.Stat(v.Abs(dest)); err == nil && !fi.IsDir() {
		return "", fmt.Errorf("vault: move %q: destination %q is not a folder: %w", src, dest, ErrInvalidPath)
	}
	return v.moveAndRewrite(src, path.Join(dest, path.Base(src)))
}

// move renames the clean vault-relative path oldRel to newRel, creating
// newRel's parent folders. It refuses to replace an existing entry, except
// for a case-only rename on a case-insensitive filesystem (paths equal
// ignoring case and naming the same file; a hard link is not exempt). It is
// the single point where entries change path. It neither rewrites links
// (user moves go through moveAndRewrite; trash and restore keep content
// byte-identical) nor checks reserved names, so internal callers (such as
// trash) may target hidden folders.
func (v *Vault) move(oldRel, newRel string) (string, error) {
	if oldRel == newRel {
		if _, err := os.Lstat(v.Abs(oldRel)); err != nil {
			return "", fmt.Errorf("vault: move %q: %w", oldRel, err)
		}
		return newRel, nil
	}
	oldAbs, newAbs := v.Abs(oldRel), v.Abs(newRel)
	srcInfo, err := os.Lstat(oldAbs)
	if err != nil {
		return "", fmt.Errorf("vault: move %q: %w", oldRel, err)
	}
	if dstInfo, err := os.Lstat(newAbs); err == nil {
		if !strings.EqualFold(oldRel, newRel) || !os.SameFile(srcInfo, dstInfo) {
			return "", fmt.Errorf("vault: move %q to %q: %w", oldRel, newRel, ErrExists)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("vault: move %q to %q: %w", oldRel, newRel, err)
	}
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		return "", fmt.Errorf("vault: move %q to %q: create parents: %w", oldRel, newRel, err)
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		return "", fmt.Errorf("vault: move %q to %q: %w", oldRel, newRel, err)
	}
	return newRel, nil
}

// within reports whether the clean path p is base or lies inside it,
// ignoring case so the check also holds on case-insensitive filesystems.
func within(p, base string) bool {
	if len(p) < len(base) || !strings.EqualFold(p[:len(base)], base) {
		return false
	}
	return len(p) == len(base) || p[len(base)] == '/'
}
