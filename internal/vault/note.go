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
)

// maxCollisions bounds the " 2", " 3", ... suffix search.
const maxCollisions = 10000

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
func (v *Vault) CreateNote(folder, title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = untitled
	}
	dir := clean(folder)
	if err := os.MkdirAll(v.Abs(dir), 0o755); err != nil {
		return "", fmt.Errorf("vault: create note in %q: %w", dir, err)
	}
	base := strings.TrimSuffix(FileNameFromTitle(title), ".md")
	rel, err := v.claim(dir, base, ".md", func(abs string) error {
		// O_EXCL reserves the name so concurrent creators cannot collide.
		f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		return f.Close()
	})
	if err != nil {
		return "", fmt.Errorf("vault: create note %q: %w", title, err)
	}
	if err := v.Save(rel, "# "+title+"\n\n"); err != nil {
		_ = os.Remove(v.Abs(rel))
		return "", fmt.Errorf("vault: create note %q: %w", title, err)
	}
	return rel, nil
}

// CreateFolder creates a folder named name (sanitized like note titles)
// inside parent, appending " 2", " 3", ... on collision. It returns the new
// folder's path.
func (v *Vault) CreateFolder(parent, name string) (string, error) {
	dir := clean(parent)
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
// succeeds, skipping names that already exist. It returns the claimed path.
func (v *Vault) claim(dir, base, ext string, create func(abs string) error) (string, error) {
	for i := 1; i <= maxCollisions; i++ {
		name := base + ext
		if i > 1 {
			name = base + " " + strconv.Itoa(i) + ext
		}
		rel := path.Join(dir, name)
		err := create(v.Abs(rel))
		if err == nil {
			return rel, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("no free name for %q: %w", base+ext, ErrExists)
}

// Rename renames the note, folder or file at rel within its folder. newName
// is sanitized like a title; notes keep their ".md" extension, which is
// appended if omitted. It fails with ErrExists if the target is taken.
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
	if fi.Mode().IsRegular() && isNoteName(src) && !isNoteName(name) {
		name += ".md"
	}
	if strings.TrimSuffix(name, ".md") == "" {
		return "", fmt.Errorf("vault: rename %q to %q: %w", src, newName, ErrInvalidName)
	}
	return v.move(src, path.Join(path.Dir(src), name))
}

// Move moves the note, folder or file at rel into destFolder ("" for the
// root), creating destFolder if needed. It fails with ErrExists if the
// target is taken, and with ErrInvalidPath if destFolder is rel itself, lies
// inside rel, or is not a folder.
func (v *Vault) Move(rel, destFolder string) (string, error) {
	src, dest := clean(rel), clean(destFolder)
	if src == "" {
		return "", fmt.Errorf("vault: move vault root: %w", ErrInvalidPath)
	}
	if dest == src || strings.HasPrefix(dest, src+"/") {
		return "", fmt.Errorf("vault: move %q into itself (%q): %w", src, dest, ErrInvalidPath)
	}
	if fi, err := os.Stat(v.Abs(dest)); err == nil && !fi.IsDir() {
		return "", fmt.Errorf("vault: move %q: destination %q is not a folder: %w", src, dest, ErrInvalidPath)
	}
	return v.move(src, path.Join(dest, path.Base(src)))
}

// move renames the clean vault-relative path oldRel to newRel, creating
// newRel's parent folders. It refuses to replace an existing entry, except
// when both paths name the same file (a case-only rename on a
// case-insensitive filesystem). It is the single point where entries change
// path, so link rewriting can hook in here.
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
		if !os.SameFile(srcInfo, dstInfo) {
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
