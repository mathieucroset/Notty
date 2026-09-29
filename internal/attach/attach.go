// Package attach implements importing images into a vault's attachments/
// folder (from a clipboard paste, a pasted/dragged file path, or `:img
// <path>`) and scanning for attachments no longer referenced by any note,
// per docs/superpowers/specs/2026-09-29-notty-design.md §6.2 ("Adding
// images", "Storage", "Links", "Clean unused attachments").
package attach

import (
	"errors"
	"fmt"
	"io/fs"
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

// tmpSuffix marks an in-flight write's temp file, matching vault.Save's own
// convention (spec §9) so such files are recognized and skipped the same
// way elsewhere (for example by Unused's attachments/ scan).
const tmpSuffix = ".notty-tmp"

// suffixChars is the alphabet of an imported file's random suffix.
const suffixChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// suffixLen is the length of an imported file's random suffix.
const suffixLen = 4

// maxNameAttempts bounds Import's search for a free file name: on a
// collision it regenerates the random suffix and retries this many times
// before giving up, rather than ever overwriting an existing file.
const maxNameAttempts = 10

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

// newSuffix generates the random suffix for an imported file's name. It is
// a package variable, rather than a direct call to randomSuffix, purely so
// tests can force a name collision (and verify Import retries onto a fresh
// name instead of overwriting anything).
var newSuffix = func() string { return randomSuffix(suffixLen) }

// Import copies data into the vault's attachments/ folder as
// "attachments/<note-slug>-<YYYYMMDD-HHMMSS>-<4 random chars>.<ext>",
// written atomically (a temp file, fsynced, then linked into place under
// its final name; spec §9) and never overwriting an existing file: on the
// rare chance the generated name collides with one already there, the
// random suffix is regenerated and the write retried, up to
// maxNameAttempts times. attachments/ is created if needed. ext is
// normalized to lowercase without a leading dot; only png, jpg, jpeg, gif
// and webp are accepted (ErrNotImage otherwise). It returns the vault-root
// markdown link to insert into the note: "![](/attachments/<name>)".
func Import(v *vault.Vault, noteRel string, data []byte, ext string, now time.Time) (string, error) {
	norm := strings.ToLower(strings.TrimPrefix(ext, "."))
	if !allowedExt[norm] {
		return "", fmt.Errorf("attach: import for %q: %w", noteRel, ErrNotImage)
	}

	dir := v.Abs(attachDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("attach: import: create %q: %w", attachDir, err)
	}

	slug := Slug(noteRel)
	stamp := now.UTC().Format("20060102-150405")

	var lastErr error
	for i := 0; i < maxNameAttempts; i++ {
		name := fmt.Sprintf("%s-%s-%s.%s", slug, stamp, newSuffix(), norm)
		rel := path.Join(attachDir, name)
		err := writeExclusive(v.Abs(rel), data)
		if err == nil {
			return "![](/" + rel + ")", nil
		}
		if errors.Is(err, fs.ErrExist) {
			lastErr = err
			continue
		}
		return "", fmt.Errorf("attach: import %q: %w", rel, err)
	}
	return "", fmt.Errorf("attach: import into %q: no free name after %d attempts: %w", attachDir, maxNameAttempts, lastErr)
}

// linkFile creates a hard link (newname pointing at oldname's data). It is
// a package variable, rather than a direct call to os.Link, so a test can
// inject a non-fs.ErrExist failure and exercise writeExclusive's fallback
// for filesystems that do not support hard links at all.
var linkFile = os.Link

// writeExclusive writes data to dst without ever overwriting an existing
// file at dst: it writes to a uniquely-named temp file in the same
// directory, fsyncs it, then links it to dst (which fails with fs.ErrExist,
// leaving dst untouched, if something is already there) before removing
// the temp file, which is otherwise just a second name for the same data.
//
// Some filesystems (exFAT, FAT, some network shares) do not support hard
// links at all; there, linkFile fails with an error other than
// fs.ErrExist, and writeExclusive falls back to writeExclusiveDirect,
// which creates dst itself with an exclusive, existence-checking open.
func writeExclusive(dst string, data []byte) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".attach-*"+tmpSuffix)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := linkFile(tmpName, dst); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fs.ErrExist
		}
		return writeExclusiveDirect(dst, data)
	}
	syncDir(dir)
	return nil
}

// writeExclusiveDirect writes data to dst by creating it exclusively
// (O_CREATE|O_EXCL), for filesystems where linkFile does not work. Like
// writeExclusive, it never overwrites: it returns fs.ErrExist, leaving dst
// untouched, if something is already there. On any other failure it
// removes the (partial) file it created, so a write error cannot leave a
// corrupt file behind.
func writeExclusiveDirect(dst string, data []byte) error {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fs.ErrExist
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	syncDir(filepath.Dir(dst))
	return nil
}

// syncDir makes a link into dir durable. It is best-effort: some platforms
// cannot fsync directories, and the file's own data is already synced.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
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
