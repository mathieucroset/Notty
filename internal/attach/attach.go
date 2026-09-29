// Package attach implements importing images into a vault's attachments/
// folder (from a clipboard paste, a pasted/dragged file path, or `:img
// <path>`) and scanning for attachments no longer referenced by any note,
// per docs/superpowers/specs/2026-09-29-notty-design.md §6.2 ("Adding
// images", "Storage", "Links", "Clean unused attachments").
package attach

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mathieucroset/notty/internal/vault"
)

// ErrNotImage means data or a file does not have a supported image
// extension (png, jpg, jpeg, gif, webp).
var ErrNotImage = errors.New("not a supported image")

// allowedExt is the set of supported image extensions, normalized to
// lowercase without a leading dot.
var allowedExt = map[string]bool{
	"png":  true,
	"jpg":  true,
	"jpeg": true,
	"gif":  true,
	"webp": true,
}

// attachDir is the vault-relative folder holding imported images (spec §3).
const attachDir = "attachments"

// suffixChars is the alphabet of an imported file's random suffix.
const suffixChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// suffixLen is the length of an imported file's random suffix.
const suffixLen = 4

// maxSlugLen is Slug's maximum output length.
const maxSlugLen = 40

// unsafeSlugChars matches anything that is not a lowercase letter or digit,
// so it can be replaced with '-' when building a slug.
var unsafeSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// Slug derives a short, filesystem- and URL-safe identifier from a note's
// vault-relative path, for use as the leading component of an imported
// attachment's filename: the note's filename without its ".md" extension,
// lowercased, with spaces and unsafe characters collapsed to single '-'s,
// trimmed of leading/trailing '-', and capped at 40 characters. A note name
// that leaves nothing usable (empty, or entirely unsafe characters) falls
// back to "note".
func Slug(noteRel string) string {
	name := path.Base(strings.ReplaceAll(noteRel, `\`, "/"))
	name = strings.TrimSuffix(name, path.Ext(name))
	lower := strings.ToLower(name)
	s := unsafeSlugChars.ReplaceAllString(lower, "-")
	s = strings.Trim(s, "-")
	if len(s) > maxSlugLen {
		s = strings.TrimRight(s[:maxSlugLen], "-")
	}
	if s == "" {
		s = "note"
	}
	return s
}

// randomSuffix returns n random characters from [a-z0-9].
func randomSuffix(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = suffixChars[rand.IntN(len(suffixChars))]
	}
	return string(b)
}

// Import copies data into the vault's attachments/ folder as
// "attachments/<note-slug>-<YYYYMMDD-HHMMSS>-<4 random chars>.<ext>",
// written atomically (a "<file>.notty-tmp" temp file, fsynced, then renamed
// into place; spec §9), creating attachments/ if needed. ext is normalized
// to lowercase without a leading dot; only png, jpg, jpeg, gif and webp are
// accepted (ErrNotImage otherwise). It returns the vault-root markdown link
// to insert into the note: "![](/attachments/<name>)".
func Import(v *vault.Vault, noteRel string, data []byte, ext string, now time.Time) (string, error) {
	norm := strings.ToLower(strings.TrimPrefix(ext, "."))
	if !allowedExt[norm] {
		return "", fmt.Errorf("attach: import for %q: %w", noteRel, ErrNotImage)
	}
	name := fmt.Sprintf("%s-%s-%s.%s",
		Slug(noteRel),
		now.UTC().Format("20060102-150405"),
		randomSuffix(suffixLen),
		norm,
	)
	rel := path.Join(attachDir, name)
	if err := v.Save(rel, string(data)); err != nil {
		return "", fmt.Errorf("attach: import %q: %w", rel, err)
	}
	return "![](/" + rel + ")", nil
}

// ImportPath reads the regular file at path (which must exist and have a
// supported image extension) and imports it with Import, using its own
// extension.
func ImportPath(v *vault.Vault, noteRel, filePath string, now time.Time) (string, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("attach: import path %q: %w", filePath, err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("attach: import path %q: %w", filePath, ErrNotImage)
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	if !allowedExt[ext] {
		return "", fmt.Errorf("attach: import path %q: %w", filePath, ErrNotImage)
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("attach: import path %q: %w", filePath, err)
	}
	return Import(v, noteRel, data, ext, now)
}
