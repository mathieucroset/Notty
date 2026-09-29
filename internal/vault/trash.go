package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// trashDir is the vault-relative folder holding trashed items (spec §3).
const trashDir = ".trash"

// metaFile is the metadata file inside each trash item's directory.
const metaFile = "meta.json"

// maxMetaSize is the largest meta.json TrashItems accepts.
const maxMetaSize = 64 << 10

// maxIDAttempts bounds the search for an unused trash ID.
const maxIDAttempts = 100

// trashIDChars is the alphabet of a trash ID's random suffix.
const trashIDChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// TrashItem is a trashed note or folder. It lives in .trash/<ID>/, which
// holds meta.json and the item itself under its original Name.
type TrashItem struct {
	ID, OriginalPath, Name, Host string
	DeletedAt                    time.Time
	IsDir                        bool
}

// trashMeta is the JSON layout of meta.json.
type trashMeta struct {
	OriginalPath string `json:"original_path"`
	DeletedAt    string `json:"deleted_at"` // RFC 3339
	Host         string `json:"host"`
	IsDir        bool   `json:"is_dir"`
}

// NewTrashID returns a trash ID "YYYYMMDDHHMMSS-<host>-<4 random chars>"
// (spec §3). The timestamp is in UTC so IDs from different machines sort by
// time. host is reduced to [A-Za-z0-9-] ("unknown" if nothing is left); the
// random suffix uses [a-z0-9].
func NewTrashID(now time.Time, host string) string {
	h := strings.Map(func(r rune) rune {
		if r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, host)
	if h == "" {
		h = "unknown"
	}
	var suffix [4]byte
	for i := range suffix {
		suffix[i] = trashIDChars[rand.IntN(len(trashIDChars))]
	}
	return now.UTC().Format("20060102150405") + "-" + h + "-" + string(suffix[:])
}

// shortHostname returns the machine's hostname up to its first '.', or
// "unknown" if it cannot be determined.
func shortHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	if h, _, _ = strings.Cut(h, "."); h == "" {
		return "unknown"
	}
	return h
}

// hostname is the host recorded in trash metadata.
func (v *Vault) hostname() string {
	if v.host != "" {
		return v.host
	}
	return shortHostname()
}

// Trash moves the note, folder or file at rel to .trash/<id>/<name> and
// writes .trash/<id>/meta.json with its original path, the deletion time and
// this machine's hostname (spec §8). The vault root and hidden locations
// (.trash itself, .git, .notty, dotfiles, ...) are rejected with
// ErrInvalidPath.
func (v *Vault) Trash(rel string) (TrashItem, error) {
	return v.trashAt(rel, time.Now())
}

// trashAt is Trash with an explicit deletion time.
func (v *Vault) trashAt(rel string, now time.Time) (TrashItem, error) {
	src := clean(rel)
	// A path TrashItems would reject is refused up front, so a trashed item
	// can never become invisible.
	if !validOriginal(src) {
		return TrashItem{}, fmt.Errorf("vault: trash %q: %w", src, ErrInvalidPath)
	}
	fi, err := os.Lstat(v.Abs(src))
	if err != nil {
		return TrashItem{}, fmt.Errorf("vault: trash %q: %w", src, err)
	}
	it := TrashItem{
		OriginalPath: src,
		Name:         path.Base(src),
		Host:         v.hostname(),
		DeletedAt:    now.UTC().Truncate(time.Second), // what meta.json can hold
		IsDir:        fi.IsDir(),
	}
	if err := v.trashRoot(true); err != nil {
		return TrashItem{}, fmt.Errorf("vault: trash %q: %w", src, err)
	}
	for i := 1; ; i++ {
		it.ID = NewTrashID(it.DeletedAt, it.Host)
		err := os.Mkdir(v.Abs(itemDir(it.ID)), 0o755)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || i >= maxIDAttempts {
			return TrashItem{}, fmt.Errorf("vault: trash %q: %w", src, err)
		}
	}
	data, err := json.MarshalIndent(trashMeta{
		OriginalPath: it.OriginalPath,
		DeletedAt:    it.DeletedAt.Format(time.RFC3339),
		Host:         it.Host,
		IsDir:        it.IsDir,
	}, "", "  ")
	if err != nil {
		_ = os.RemoveAll(v.Abs(itemDir(it.ID)))
		return TrashItem{}, fmt.Errorf("vault: trash %q: %w", src, err)
	}
	// Metadata first: an interrupted trash leaves an item that lists (and
	// can be deleted) rather than an orphaned, invisible copy.
	if err := v.Save(path.Join(itemDir(it.ID), metaFile), string(data)+"\n"); err != nil {
		_ = os.RemoveAll(v.Abs(itemDir(it.ID)))
		return TrashItem{}, err // already prefixed and names the path
	}
	if _, err := v.move(src, v.TrashContentPath(it)); err != nil {
		_ = os.RemoveAll(v.Abs(itemDir(it.ID)))
		return TrashItem{}, err // already prefixed and names the paths
	}
	return it, nil
}

// trashRoot checks that .trash is a real directory, not a symlink or a
// file, so trash operations can never create, move or delete anything
// outside the vault. A missing .trash is created if create is set, and
// otherwise reported as fs.ErrNotExist.
func (v *Vault) trashRoot(create bool) error {
	abs := v.Abs(trashDir)
	fi, err := os.Lstat(abs)
	if errors.Is(err, fs.ErrNotExist) && create {
		if err := os.Mkdir(abs, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		fi, err = os.Lstat(abs)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a folder: %w", trashDir, ErrInvalidPath)
	}
	return nil
}

// itemDir returns the vault-relative directory of the trash item id.
func itemDir(id string) string { return trashDir + "/" + id }

// TrashContentPath returns the vault-relative path of the trashed note or
// folder itself: ".trash/<id>/<name>".
func (v *Vault) TrashContentPath(it TrashItem) string {
	return itemDir(it.ID) + "/" + it.Name
}

// TrashItems lists the trash, newest first (ties broken by ID, descending).
// Entries without a valid meta.json are skipped. A vault without a .trash
// folder has an empty trash.
func (v *Vault) TrashItems() ([]TrashItem, error) {
	if err := v.trashRoot(false); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("vault: read trash: %w", err)
	}
	entries, err := os.ReadDir(v.Abs(trashDir))
	if err != nil {
		return nil, fmt.Errorf("vault: read trash: %w", err)
	}
	var items []TrashItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if it, ok := v.readItem(e.Name()); ok {
			items = append(items, it)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if !a.DeletedAt.Equal(b.DeletedAt) {
			return a.DeletedAt.After(b.DeletedAt)
		}
		return a.ID > b.ID
	})
	return items, nil
}

// readItem loads the trash item id from its meta.json. It reports false if
// the metadata is missing, malformed, or names an unusable original path.
func (v *Vault) readItem(id string) (TrashItem, bool) {
	if !isSegment(id) {
		return TrashItem{}, false
	}
	b, err := readMeta(v.Abs(path.Join(itemDir(id), metaFile)))
	if err != nil {
		return TrashItem{}, false
	}
	var m trashMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return TrashItem{}, false
	}
	at, err := time.Parse(time.RFC3339, m.DeletedAt)
	if err != nil || !validOriginal(m.OriginalPath) {
		return TrashItem{}, false
	}
	return TrashItem{
		ID:           id,
		OriginalPath: m.OriginalPath,
		Name:         path.Base(m.OriginalPath),
		Host:         m.Host,
		DeletedAt:    at,
		IsDir:        m.IsDir,
	}, true
}

// readMeta reads a meta.json file, refusing anything but a regular file of
// at most maxMetaSize bytes (a symlink could point anywhere, and a huge file
// would be read into memory on every trash listing).
func readMeta(abs string) ([]byte, error) {
	fi, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file: %w", metaFile, ErrInvalidPath)
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxMetaSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxMetaSize {
		return nil, fmt.Errorf("%s is too large: %w", metaFile, ErrInvalidPath)
	}
	return b, nil
}

// isSegment reports whether s is a single path segment, usable as a trash
// ID or an item name without escaping its directory.
func isSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`)
}

// validOriginal reports whether p is a clean, non-root path outside hidden
// locations whose every segment is already a sanitized name (spec §3), so
// restoring to it cannot escape the vault, write into .git, .notty or
// .trash, or use names that are illegal on Windows (including alternate
// data streams such as "a:b").
func validOriginal(p string) bool {
	if p == "" || clean(p) != p || inReserved(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if sanitizeName(seg) != seg {
			return false
		}
	}
	return true
}

// checkAncestors fails with ErrInvalidPath if an existing ancestor folder
// of the clean path rel is a symlink or not a directory, so a restore can
// neither be redirected outside the vault nor fail halfway.
func (v *Vault) checkAncestors(rel string) error {
	dir := parentOf(rel)
	if dir == "" {
		return nil
	}
	cur := ""
	for _, seg := range strings.Split(dir, "/") {
		cur = path.Join(cur, seg)
		fi, err := os.Lstat(v.Abs(cur))
		if errors.Is(err, fs.ErrNotExist) {
			return nil // the rest is created by the move
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("%q is not a folder: %w", cur, ErrInvalidPath)
		}
	}
	return nil
}

// Restore moves a trashed item back to its original path and removes its
// trash directory, recreating missing parent folders. If the original path
// is taken, it uses "<name> (restored).md", then "<name> (restored 2).md",
// ... (folders and other files get the suffix before their extension, or at
// the end if they have none). It returns the restored path. If only the
// final cleanup of .trash/<id> fails, the item is restored and both the path
// and the error are returned.
func (v *Vault) Restore(it TrashItem) (string, error) {
	if !isSegment(it.ID) || !isSegment(it.Name) || !validOriginal(it.OriginalPath) {
		return "", fmt.Errorf("vault: restore trash item %q: %w", it.ID, ErrInvalidPath)
	}
	if err := v.trashRoot(false); err != nil {
		return "", fmt.Errorf("vault: restore trash item %q: %w", it.ID, err)
	}
	if di, err := os.Lstat(v.Abs(itemDir(it.ID))); err != nil {
		return "", fmt.Errorf("vault: restore trash item %q: %w", it.ID, err)
	} else if !di.IsDir() {
		return "", fmt.Errorf("vault: restore trash item %q: not a folder: %w", it.ID, ErrInvalidPath)
	}
	src := v.TrashContentPath(it)
	fi, err := os.Lstat(v.Abs(src))
	if err != nil {
		return "", fmt.Errorf("vault: restore trash item %q: %w", it.ID, err)
	}
	if err := v.checkAncestors(it.OriginalPath); err != nil {
		return "", fmt.Errorf("vault: restore %q: %w", it.OriginalPath, err)
	}
	dir, name := parentOf(it.OriginalPath), path.Base(it.OriginalPath)
	ext := ""
	if !fi.IsDir() {
		if ext = path.Ext(name); ext == name {
			ext = ""
		}
	}
	base := strings.TrimSuffix(name, ext)
	for i := 0; i <= maxCollisions; i++ {
		cand := name
		switch {
		case i == 1:
			cand = base + " (restored)" + ext
		case i > 1:
			cand = base + " (restored " + strconv.Itoa(i) + ")" + ext
		}
		if reserved(dir, cand) {
			continue
		}
		rel, err := v.move(src, path.Join(dir, cand))
		if errors.Is(err, ErrExists) {
			continue
		}
		if err != nil {
			return "", err // already prefixed and names the paths
		}
		if err := os.RemoveAll(v.Abs(itemDir(it.ID))); err != nil {
			return rel, fmt.Errorf("vault: restore trash item %q: remove trash dir: %w", it.ID, err)
		}
		return rel, nil
	}
	return "", fmt.Errorf("vault: restore %q: no free name: %w", it.OriginalPath, ErrExists)
}

// DeleteForever permanently removes a trashed item. Removing an item that
// is already gone is not an error.
func (v *Vault) DeleteForever(it TrashItem) error {
	if !isSegment(it.ID) {
		return fmt.Errorf("vault: delete trash item %q: %w", it.ID, ErrInvalidPath)
	}
	if err := v.trashRoot(false); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("vault: delete trash item %q: %w", it.ID, err)
	}
	// RemoveAll does not follow a symlinked item directory: only the link
	// itself is removed.
	if err := os.RemoveAll(v.Abs(itemDir(it.ID))); err != nil {
		return fmt.Errorf("vault: delete trash item %q: %w", it.ID, err)
	}
	return nil
}

// EmptyTrash permanently removes everything inside .trash, including
// entries without valid metadata.
func (v *Vault) EmptyTrash() error {
	if err := v.trashRoot(false); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("vault: empty trash: %w", err)
	}
	entries, err := os.ReadDir(v.Abs(trashDir))
	if err != nil {
		return fmt.Errorf("vault: empty trash: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if err := os.RemoveAll(v.Abs(path.Join(trashDir, e.Name()))); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("vault: empty trash: %w", err)
	}
	return nil
}

// PurgeOlderThan permanently removes trash items deleted more than d before
// now, returning how many were removed. It keeps going past failures and
// reports them together.
func (v *Vault) PurgeOlderThan(d time.Duration, now time.Time) (int, error) {
	items, err := v.TrashItems()
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, it := range items {
		if now.Sub(it.DeletedAt) <= d {
			continue
		}
		if err := v.DeleteForever(it); err != nil {
			errs = append(errs, err)
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}
