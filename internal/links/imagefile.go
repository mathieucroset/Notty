package links

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotImage and ErrOutsideVault are the reasons CheckImageFile refuses a
// file.
var (
	ErrNotImage     = errors.New("not an image file")
	ErrOutsideVault = errors.New("outside the vault")
)

// imageExts are the image types Notty shows and opens.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// IsImageName reports whether name has an image extension (png, jpg, jpeg,
// gif or webp, in any case).
func IsImageName(name string) bool {
	return imageExts[strings.ToLower(filepath.Ext(name))]
}

// CheckImageFile reports whether the linked file at abs may be shown, and
// handed to the system's opener, as an image. It must be a regular file
// with an image extension inside vaultRoot, and so must its target when a
// symlink leads to it (the link itself, or a folder on its path): a note in
// a shared vault could otherwise link a script ("![x](run.bat)"), or commit
// "pic.png" as a symlink to one, and have the opener run it.
func CheckImageFile(vaultRoot, abs string) error {
	if !IsImageName(abs) {
		return fmt.Errorf("%s: %w", filepath.Base(abs), ErrNotImage)
	}
	target, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", filepath.Base(abs), err)
	}
	root, err := filepath.EvalSymlinks(vaultRoot)
	if err != nil {
		return fmt.Errorf("resolve the vault: %w", err)
	}
	if rel, err := filepath.Rel(root, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s: %w", filepath.Base(abs), ErrOutsideVault)
	}
	if !IsImageName(target) {
		return fmt.Errorf("%s leads to %s: %w", filepath.Base(abs), filepath.Base(target), ErrNotImage)
	}
	fi, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("stat %s: %w", filepath.Base(abs), err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s: %w", filepath.Base(abs), ErrNotImage)
	}
	return nil
}
