package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mathieucroset/notty/internal/links"
)

// rewriteLinks keeps note-relative image links valid after the entry at
// oldRel moved to newRel (both clean). A moved note has its own links
// rewritten; a moved folder has the links of every note inside it
// rewritten. Only links resolving to an existing file outside the moved
// entry change: targets inside it moved along, so their relative links
// still hold, and root-relative ("/...") and external links never change.
// A link that reaches into the moved folder through its old name (such as
// "../Work/img.png" from inside Work) is left as is.
//
// Failures on individual notes do not stop the others; they are reported
// together.
func (v *Vault) rewriteLinks(oldRel, newRel string) error {
	newAbs := v.Abs(newRel)
	fi, err := os.Lstat(newAbs)
	if err != nil {
		return fmt.Errorf("vault: rewrite links in %q: %w", newRel, err)
	}
	if !fi.IsDir() {
		if fi.Mode().IsRegular() && isNoteName(newRel) {
			return v.rewriteNoteLinks(oldRel, newRel, oldRel)
		}
		return nil
	}
	var errs []error
	walkErr := filepath.WalkDir(newAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if p == newAbs {
			return nil
		}
		// Skip what Tree hides below the root: dot entries and temp files.
		if name := d.Name(); strings.HasPrefix(name, ".") || strings.HasSuffix(name, tmpSuffix) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !isNoteName(d.Name()) {
			return nil
		}
		sub, err := filepath.Rel(newAbs, p)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		sub = filepath.ToSlash(sub)
		if err := v.rewriteNoteLinks(path.Join(oldRel, sub), path.Join(newRel, sub), oldRel); err != nil {
			errs = append(errs, err)
		}
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("vault: rewrite links in %q: %w", newRel, err)
	}
	return nil
}

// rewriteNoteLinks rewrites the links of the note now at newNote, which was
// at oldNote before the entry oldRoot moved.
func (v *Vault) rewriteNoteLinks(oldNote, newNote, oldRoot string) error {
	if parentOf(oldNote) == parentOf(newNote) {
		return nil // same folder: relative links are unaffected
	}
	content, err := v.Read(newNote)
	if err != nil {
		return err
	}
	exists := func(target string) bool {
		if within(target, oldRoot) {
			return false // moved along with the note
		}
		fi, err := os.Stat(v.Abs(target))
		return err == nil && !fi.IsDir()
	}
	updated, changed := links.RewriteForMove(content, oldNote, newNote, exists)
	if !changed {
		return nil
	}
	return v.Save(newNote, updated)
}
