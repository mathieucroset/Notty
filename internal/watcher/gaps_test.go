package watcher

import (
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// blockers are entries fsnotify's kqueue backend cannot open. Each creates
// one named rel under root.
var blockers = []struct {
	name   string
	create func(t *testing.T, root, rel string)
}{
	{"unreadable directory", func(t *testing.T, root, rel string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.Mkdir(p, 0); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
	}},
	{"unreadable file", func(t *testing.T, root, rel string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), []byte("x"), 0); err != nil {
			t.Fatalf("write: %v", err)
		}
	}},
	{"dangling symlink", func(t *testing.T, root, rel string) {
		t.Helper()
		if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, rel)); err != nil {
			t.Fatalf("symlink: %v", err)
		}
	}},
}

func skipWithoutPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root user on a POSIX system to deny access")
	}
}

// TestEntriesAfterUnopenableEntryNoticed creates, edits and replaces notes
// sorting after an entry fsnotify cannot open, in the same directory. The
// kqueue backend (macOS, BSD) never lists those notes by itself.
func TestEntriesAfterUnopenableEntryNoticed(t *testing.T) {
	skipWithoutPermissions(t)
	for _, b := range blockers {
		for _, existing := range []bool{true, false} {
			name := b.name + "/created later"
			if existing {
				name = b.name + "/existing"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				writeFile(t, root, "a.md", "a")
				if existing {
					b.create(t, root, "b")
				}
				w, _ := newLoggedWatcher(t, root)
				if !existing {
					b.create(t, root, "b")
					collectUntil(t, w, "b")
				}

				writeFile(t, root, "c.md", "created")
				collectUntil(t, w, "c.md")
				writeFile(t, root, "d/e.md", "in a new folder")
				collectUntil(t, w, "d/e.md")
				writeFile(t, root, "c.md", "edited")
				collectUntil(t, w, "c.md")
				writeFile(t, root, "c.md.new", "replaced")
				if err := os.Rename(filepath.Join(root, "c.md.new"), filepath.Join(root, "c.md")); err != nil {
					t.Fatalf("rename: %v", err)
				}
				collectUntil(t, w, "c.md")
				writeFile(t, root, "c.md", "edited after the replace")
				collectUntil(t, w, "c.md")
			})
		}
	}
}

// newGapWatcher is a watcher, event loop not started, that tracks listing
// gaps as on kqueue, over a root holding a.md, the blocker b and c.md.
func newGapWatcher(t *testing.T, create func(*testing.T, string, string)) (*Watcher, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "a.md", "a")
	create(t, root, "b")
	writeFile(t, root, "c.md", "c")
	w, err := newWatcher(root, (&logBuffer{}).logger())
	if err != nil {
		t.Fatalf("newWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	w.trackGaps = true
	if _, err := w.addTree(".", nil); err != nil {
		t.Fatalf("addTree: %v", err)
	}
	return w, root
}

func pendingPaths(w *Watcher) []string {
	return slices.Sorted(maps.Keys(w.pending))
}

// TestGapPolled simulates the kqueue backend on any platform: it reports
// nothing for the entries after the blocker, which the poll finds.
func TestGapPolled(t *testing.T) {
	skipWithoutPermissions(t)
	for _, b := range blockers {
		t.Run(b.name, func(t *testing.T) {
			w, root := newGapWatcher(t, b.create)
			if g, ok := w.gaps["."]; !ok || g.blocker != "b" {
				t.Fatalf("gaps = %v, want a gap in . after b", w.gaps)
			}
			if got := pendingPaths(w); len(got) != 0 {
				t.Fatalf("pending after the first listing = %v, want none", got)
			}
			steps := []struct {
				name   string
				change func()
				want   []string
			}{
				{"nothing", func() {}, []string{}},
				{"created", func() {
					writeFile(t, root, "d.md", "d")
					writeFile(t, root, "e/f.md", "f")
				}, []string{"d.md", "e", "e/f.md"}},
				{"edited", func() { writeFile(t, root, "c.md", "edited") }, []string{"c.md"}},
				{"deleted", func() {
					if err := os.Remove(filepath.Join(root, "d.md")); err != nil {
						t.Fatal(err)
					}
				}, []string{"d.md"}},
				{"ignored", func() { writeFile(t, root, "g.notty-tmp", "x") }, []string{}},
			}
			for _, s := range steps {
				clear(w.pending)
				s.change()
				w.pollGaps(time.Now())
				if got := pendingPaths(w); !slices.Equal(got, s.want) {
					t.Fatalf("%s: pending = %v, want %v", s.name, got, s.want)
				}
			}
			if !w.dirs["e"] {
				t.Error("new folder e not watched")
			}
			if n := len(w.errors); n != 0 {
				t.Errorf("%d errors reported, want none: %v", n, <-w.errors)
			}
		})
	}
}

// TestGapBlockerReportedAgain simulates the kqueue backend reporting the
// blocker as created on every change to its directory: the blocker is not
// reported again, and the directory is listed right away.
func TestGapBlockerReportedAgain(t *testing.T) {
	skipWithoutPermissions(t)
	for _, b := range blockers {
		t.Run(b.name, func(t *testing.T) {
			w, root := newGapWatcher(t, b.create)
			writeFile(t, root, "d.md", "d")
			if !w.handle(fsnotify.Event{Name: filepath.Join(root, "b"), Op: fsnotify.Create}, time.Now()) {
				t.Fatal("handle recorded nothing")
			}
			if got := pendingPaths(w); !slices.Equal(got, []string{"d.md"}) {
				t.Fatalf("pending = %v, want [d.md]", got)
			}
		})
	}
}

// TestEarlierBlockerMovesTheGap simulates the kqueue backend when an entry
// it cannot open is created before the blocker: it now stops listing there.
func TestEarlierBlockerMovesTheGap(t *testing.T) {
	skipWithoutPermissions(t)
	for _, b := range blockers {
		t.Run(b.name, func(t *testing.T) {
			w, root := newGapWatcher(t, b.create)
			b.create(t, root, "a1")
			writeFile(t, root, "a2.md", "between the blockers")
			w.handle(fsnotify.Event{Name: filepath.Join(root, "a1"), Op: fsnotify.Create}, time.Now())
			if g := w.gaps["."]; g.blocker != "a1" {
				t.Fatalf("blocker = %q, want a1", g.blocker)
			}
			if got := pendingPaths(w); !slices.Contains(got, "a1") || !slices.Contains(got, "a2.md") {
				t.Fatalf("pending = %v, want a1 and a2.md in it", got)
			}
		})
	}
}

// TestNewBlockerReportsEntriesAfterIt simulates the kqueue backend when an
// entry it cannot open is created: it stops listing there, so entries
// created after it in the same burst are only found by the watcher.
func TestNewBlockerReportsEntriesAfterIt(t *testing.T) {
	skipWithoutPermissions(t)
	for _, b := range blockers {
		t.Run(b.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "a.md", "a")
			w, err := newWatcher(root, (&logBuffer{}).logger())
			if err != nil {
				t.Fatalf("newWatcher: %v", err)
			}
			t.Cleanup(func() { _ = w.Close() })
			w.trackGaps = true

			b.create(t, root, "b")
			writeFile(t, root, "c.md", "c")
			w.handle(fsnotify.Event{Name: filepath.Join(root, "b"), Op: fsnotify.Create}, time.Now())
			if got := pendingPaths(w); !slices.Equal(got, []string{"b", "c.md"}) {
				t.Fatalf("pending = %v, want [b c.md]", got)
			}
			if n := len(w.errors); n != 0 {
				t.Errorf("%d errors reported, want none: %v", n, <-w.errors)
			}
		})
	}
}

// TestGapClosesWhenTheBlockerGoes checks that a directory stops being
// listed once every entry in it can be opened.
func TestGapClosesWhenTheBlockerGoes(t *testing.T) {
	skipWithoutPermissions(t)
	for _, b := range blockers {
		t.Run(b.name, func(t *testing.T) {
			w, root := newGapWatcher(t, b.create)
			p := filepath.Join(root, "b")
			_ = os.Chmod(p, 0o755)
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			w.pollGaps(time.Now())
			if len(w.gaps) != 0 {
				t.Fatalf("gaps = %v, want none", w.gaps)
			}
		})
	}
}

// addRecordingBackend records the paths watches are added on.
type addRecordingBackend struct {
	backend
	mu   sync.Mutex
	adds []string
}

func (b *addRecordingBackend) Add(p string) error {
	b.mu.Lock()
	b.adds = append(b.adds, p)
	b.mu.Unlock()
	return b.backend.Add(p)
}

// TestGapDoesNotWatchSymlinks: an entry after the blocker is watched with
// the backend's Add, which follows a symlink (possibly out of the vault);
// symlinks are skipped.
func TestGapDoesNotWatchSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs a privilege on Windows")
	}
	var rec *addRecordingBackend
	withBackend(t, func(b backend) backend {
		rec = &addRecordingBackend{backend: b}
		return rec
	})
	dangling := func(t *testing.T, root, rel string) {
		t.Helper()
		if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, rel)); err != nil {
			t.Fatalf("symlink: %v", err)
		}
	}
	w, root := newGapWatcher(t, dangling)
	writeFile(t, root, "d.md", "d")
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "e-link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	rec.mu.Lock()
	rec.adds = nil
	rec.mu.Unlock()

	w.pollGaps(time.Now())

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !slices.Contains(rec.adds, filepath.Join(w.root, "d.md")) {
		t.Errorf("new note after the blocker not watched; adds = %v", rec.adds)
	}
	if slices.Contains(rec.adds, filepath.Join(w.root, "e-link.md")) {
		t.Errorf("symlink after the blocker watched; adds = %v", rec.adds)
	}
}
