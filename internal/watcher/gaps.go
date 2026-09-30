package watcher

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"runtime"
	"time"
)

// Listing gaps (kqueue: macOS and the BSDs).
//
// kqueue has no event for "an entry was added to this directory". fsnotify's
// kqueue backend re-lists a watched directory whenever it changes, reports
// the entries it has not seen before as created, and opens every entry to
// watch it. It stops at the first entry it cannot open (one the user may not
// read, a dangling symlink): it reports that entry as created again on every
// change to the directory, and never lists the entries sorting after it, so
// notes created there are neither reported nor watched.
//
// A directory holding such an entry (its blocker) has a gap: the watcher lists
// the entries after the blocker itself, reports the new, changed and deleted
// ones, and watches the new ones. It lists the directory whenever an event
// comes from it (the blocker's repeated creation among them), and every
// gapPoll, because a blocker that existed when the directory was first
// watched is never reported again, leaving nothing to act on.

// gapPoll is how often directories with a gap are listed.
const gapPoll = time.Second

// usesKqueue reports whether fsnotify uses its kqueue backend on this system.
func usesKqueue() bool {
	switch runtime.GOOS {
	case "darwin", "dragonfly", "freebsd", "netbsd", "openbsd":
		return true
	}
	return false
}

// gap is a directory's listing past its blocker.
type gap struct {
	blocker string              // first entry, in listing order, that cannot be opened
	after   map[string]gapEntry // entries sorting after it, as last listed
}

// gapEntry identifies an entry after a blocker. For a directory only its
// existence matters: its own watch reports what happens inside.
type gapEntry struct {
	dir   bool
	mtime int64
	size  int64
	id    fileID
}

func gapEntryOf(info fs.FileInfo) gapEntry {
	if info.IsDir() {
		return gapEntry{dir: true}
	}
	return gapEntry{mtime: info.ModTime().UnixNano(), size: info.Size(), id: fileIDOf(info)}
}

// listGap lists dir and returns its gap, or false when every entry can be
// opened. The entries sorting before from are not probed: they come before
// the previous blocker, so fsnotify lists them, and reports a new blocker
// among them as created (see gapEvent).
func (w *Watcher) listGap(dir, from string) (gap, bool) {
	entries, err := os.ReadDir(w.abs(dir))
	if err != nil {
		return gap{}, false
	}
	start := 0
	for start < len(entries) && entries[start].Name() < from {
		start++
	}
	for i := start; i < len(entries); i++ {
		name := entries[i].Name()
		if !blocks(w.abs(path.Join(dir, name)), entries[i].Type()) {
			continue
		}
		g := gap{blocker: name, after: map[string]gapEntry{}}
		for _, e := range entries[i+1:] {
			if ignored(path.Join(dir, e.Name())) {
				continue
			}
			if info, err := e.Info(); err == nil {
				g.after[e.Name()] = gapEntryOf(info)
			}
		}
		return g, true
	}
	return gap{}, false
}

// scanGap lists dir, which has or may have a gap. Every entry after the
// blocker that is new or changed since the last listing (every one, when dir
// had no gap: fsnotify just stopped listing it) is watched, and the new,
// changed and deleted ones are recorded as changed at now unless record is
// false. It reports whether any change was recorded.
func (w *Watcher) scanGap(dir string, now time.Time, record bool) bool {
	old, had := w.gaps[dir]
	g, ok := w.listGap(dir, old.blocker)
	if !ok {
		delete(w.gaps, dir)
		return false
	}
	w.gaps[dir] = g
	recorded := false
	mark := func(sub string) {
		if record && sub != nottyDir {
			w.pending[sub] = now
			recorded = true
		}
	}
	for name, e := range g.after {
		if prev, seen := old.after[name]; had && seen && prev == e {
			continue
		}
		sub := path.Join(dir, name)
		if e.dir {
			if !w.dirs[sub] {
				errs, err := w.addTree(sub, mark)
				for _, e := range errs {
					w.sendError(e)
				}
				if err != nil {
					w.sendError(err)
				}
			}
		} else {
			// fsnotify never listed it, so it does not watch it either.
			_ = w.fsw.Add(w.abs(sub))
		}
		mark(sub)
	}
	for name := range old.after {
		if _, still := g.after[name]; still {
			continue
		}
		sub := path.Join(dir, name)
		if _, err := os.Lstat(w.abs(sub)); errors.Is(err, fs.ErrNotExist) {
			w.unwatchTree(sub)
			mark(sub)
		}
	}
	return recorded
}

// pollGaps lists every directory with a gap. It reports whether any change
// was recorded.
func (w *Watcher) pollGaps(now time.Time) bool {
	record := !w.isPaused()
	recorded := false
	for dir := range w.gaps {
		if w.scanGap(dir, now, record) {
			recorded = true
		}
	}
	return recorded
}

// gapEvent lists the directory of rel when it has a gap, or when the event
// is the creation of an entry that cannot be opened, which starts one. It
// reports whether the event is the blocker's repeated creation, which is not
// a change, and whether any change was recorded.
func (w *Watcher) gapEvent(rel string, create bool, now time.Time, record bool) (repeat, recorded bool) {
	dir, name := path.Dir(rel), path.Base(rel)
	g, had := w.gaps[dir]
	blocker := false
	if create {
		if info, err := os.Lstat(w.abs(rel)); err == nil {
			blocker = blocks(w.abs(rel), info.Mode().Type())
		}
	}
	if !had && !blocker {
		return false, false
	}
	repeat = had && blocker && name == g.blocker
	if had && blocker && name < g.blocker {
		// A new blocker, where fsnotify now stops listing.
		g.blocker = name
		w.gaps[dir] = g
	}
	return repeat, w.scanGap(dir, now, record)
}

// markGap notes, while adding watches, that the entry rel cannot be opened.
// Its directory, if it has no gap yet, is listed and the listing only
// remembered: the watches were just set up, with nothing missed.
func (w *Watcher) markGap(rel string) {
	dir := path.Dir(rel)
	if _, ok := w.gaps[dir]; ok {
		return
	}
	if g, ok := w.listGap(dir, ""); ok {
		w.gaps[dir] = g
	}
}

// blocks reports whether fsnotify's kqueue backend fails to open the entry
// at p, of type typ. Sockets and named pipes it skips without opening them.
func blocks(p string, typ fs.FileMode) bool {
	return typ&(fs.ModeSocket|fs.ModeNamedPipe) == 0 && openable(p) != nil
}
