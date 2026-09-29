package watcher

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const (
	waitTimeout    = 2 * time.Second        // generous: waiting for an event that must arrive
	silenceTimeout = 400 * time.Millisecond // short: asserting that no event arrives
)

func newTestWatcher(t *testing.T, root string) *Watcher {
	t.Helper()
	w, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func mkdir(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
}

// nextEvent waits up to waitTimeout for one Event.
func nextEvent(t *testing.T, w *Watcher) Event {
	t.Helper()
	select {
	case ev, ok := <-w.Events():
		if !ok {
			t.Fatal("events channel closed unexpectedly")
		}
		return ev
	case err := <-w.Errors():
		t.Fatalf("unexpected watcher error: %v", err)
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for event")
	}
	return Event{}
}

// collectUntil gathers events until every wanted path has been seen, and
// returns the union of all paths observed.
func collectUntil(t *testing.T, w *Watcher, want ...string) []string {
	t.Helper()
	seen := map[string]bool{}
	deadline := time.After(waitTimeout)
	for {
		missing := false
		for _, p := range want {
			if !seen[p] {
				missing = true
			}
		}
		if !missing {
			break
		}
		select {
		case ev, ok := <-w.Events():
			if !ok {
				t.Fatal("events channel closed unexpectedly")
			}
			for _, p := range ev.Paths {
				seen[p] = true
			}
		case err := <-w.Errors():
			t.Fatalf("unexpected watcher error: %v", err)
		case <-deadline:
			t.Fatalf("timed out; wanted %v, saw %v", want, seen)
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func expectNoEvent(t *testing.T, w *Watcher) {
	t.Helper()
	select {
	case ev, ok := <-w.Events():
		if ok {
			t.Fatalf("unexpected event: %v", ev.Paths)
		}
		t.Fatal("events channel closed unexpectedly")
	case err := <-w.Errors():
		t.Fatalf("unexpected watcher error: %v", err)
	case <-time.After(silenceTimeout):
	}
}

func TestExternalWriteProducesOneEvent(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	writeFile(t, root, "a.md", "hello")

	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"a.md"}) {
		t.Fatalf("paths = %v, want [a.md]", ev.Paths)
	}
	expectNoEvent(t, w)
}

func TestExternalWriteToExistingFileInSubdir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Work/Standup.md", "v1")
	w := newTestWatcher(t, root)

	writeFile(t, root, "Work/Standup.md", "v2")

	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"Work/Standup.md"}) {
		t.Fatalf("paths = %v, want [Work/Standup.md]", ev.Paths)
	}
	expectNoEvent(t, w)
}

// atomicSave mimics the app's save: write a tmp file, then rename into place.
func atomicSave(t *testing.T, root, rel, content string) {
	t.Helper()
	dst := filepath.Join(root, filepath.FromSlash(rel))
	tmp := dst + ".notty-tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

func TestSelfWriteSuppressed(t *testing.T) {
	tests := []struct {
		name     string
		existing bool
	}{
		{"new file", false},
		{"existing file", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.existing {
				writeFile(t, root, "note.md", "old")
			}
			w := newTestWatcher(t, root)

			atomicSave(t, root, "note.md", "saved by app")
			w.NoteSelfWrite("note.md")

			expectNoEvent(t, w)
		})
	}
}

func TestExternalWriteAfterSelfWriteReported(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	atomicSave(t, root, "note.md", "saved by app")
	w.NoteSelfWrite("note.md")
	expectNoEvent(t, w)

	writeFile(t, root, "note.md", "edited in $EDITOR, different size")

	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"note.md"}) {
		t.Fatalf("paths = %v, want [note.md]", ev.Paths)
	}
}

func TestSelfWriteDoesNotHideOtherPaths(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	atomicSave(t, root, "mine.md", "app")
	writeFile(t, root, "theirs.md", "external")
	w.NoteSelfWrite("mine.md")

	got := collectUntil(t, w, "theirs.md")
	if slices.Contains(got, "mine.md") {
		t.Fatalf("self-written path reported: %v", got)
	}
	expectNoEvent(t, w)
}

func TestBurstCollapsesIntoSingleEvent(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "dir")
	w := newTestWatcher(t, root)

	files := []string{"a.md", "b.md", "dir/c.md"}
	start := time.Now()
	for i := range 10 {
		writeFile(t, root, files[i%len(files)], string(rune('a'+i)))
		time.Sleep(3 * time.Millisecond)
	}
	if d := time.Since(start); d > 90*time.Millisecond {
		t.Skipf("burst took %v; machine too slow to test debouncing", d)
	}

	ev := nextEvent(t, w)
	want := []string{"a.md", "b.md", "dir/c.md"}
	if !slices.Equal(ev.Paths, want) {
		t.Fatalf("paths = %v, want %v", ev.Paths, want)
	}
	expectNoEvent(t, w)
}

func TestRepeatedWritesToOneFileExtendQuietWindow(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	// Writes 25ms apart span well over the 100ms debounce, but there is never
	// a 100ms quiet gap, so they must collapse into a single Event.
	var maxGap time.Duration
	last := time.Now()
	for i := range 10 {
		writeFile(t, root, "typing.md", string(rune('a'+i)))
		now := time.Now()
		maxGap = max(maxGap, now.Sub(last))
		last = now
		time.Sleep(25 * time.Millisecond)
	}
	if maxGap > 80*time.Millisecond {
		t.Skipf("writes stalled for %v; machine too slow to test debouncing", maxGap)
	}

	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"typing.md"}) {
		t.Fatalf("paths = %v, want [typing.md]", ev.Paths)
	}
	expectNoEvent(t, w)
}

func TestPausedEventsDroppedAndResumeDelivers(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	w.Pause()
	writeFile(t, root, "during-pause.md", "git wrote this")
	expectNoEvent(t, w)
	w.Resume()

	writeFile(t, root, "after-resume.md", "user wrote this")
	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"after-resume.md"}) {
		t.Fatalf("paths = %v, want [after-resume.md]", ev.Paths)
	}
}

func TestFileInNewSubdirectoryDetected(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	// Created in one go: the file may exist before the watch on the new
	// directory is set up, so the watcher must scan it.
	writeFile(t, root, "sub/deeper/n.md", "x")
	collectUntil(t, w, "sub/deeper/n.md")

	// Written later: proves the new directories are actually watched.
	time.Sleep(150 * time.Millisecond)
	writeFile(t, root, "sub/deeper/m.md", "y")
	got := collectUntil(t, w, "sub/deeper/m.md")
	if slices.Contains(got, "sub/deeper/n.md") {
		t.Fatalf("unrelated path re-reported: %v", got)
	}
}

func TestRenamedDirectoryWatchedAtNewPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "old/inner/a.md", "x")
	w := newTestWatcher(t, root)

	if err := os.Rename(filepath.Join(root, "old"), filepath.Join(root, "new")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	collectUntil(t, w, "old", "new")

	time.Sleep(150 * time.Millisecond)
	writeFile(t, root, "new/inner/b.md", "y")
	got := collectUntil(t, w, "new/inner/b.md")
	for _, p := range got {
		if p == "old/inner/b.md" {
			t.Fatalf("event reported under stale directory name: %v", got)
		}
	}
}

func TestIgnoredPathsProduceNoEvents(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, ".git/objects")
	mkdir(t, root, ".notty/recovery")
	w := newTestWatcher(t, root)

	writeFile(t, root, ".git/x", "1")
	writeFile(t, root, ".git/objects/ab", "1")
	writeFile(t, root, "a.notty-tmp", "1")
	writeFile(t, root, ".notty/lock", "1")
	writeFile(t, root, ".notty/recovery/buf.md", "1")
	mkdir(t, root, ".git/refs/heads")

	expectNoEvent(t, w)
}

func TestNottyStateFileNotIgnored(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, ".notty")
	w := newTestWatcher(t, root)

	writeFile(t, root, ".notty/state.json", "{}")
	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{".notty/state.json"}) {
		t.Fatalf("paths = %v, want [.notty/state.json]", ev.Paths)
	}
}

func TestDeletionReported(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "gone.md", "bye")
	w := newTestWatcher(t, root)

	if err := os.Remove(filepath.Join(root, "gone.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"gone.md"}) {
		t.Fatalf("paths = %v, want [gone.md]", ev.Paths)
	}
}

func TestDeletedSelfWrittenFileReported(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	atomicSave(t, root, "note.md", "app")
	w.NoteSelfWrite("note.md")
	expectNoEvent(t, w)

	if err := os.Remove(filepath.Join(root, "note.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"note.md"}) {
		t.Fatalf("paths = %v, want [note.md]", ev.Paths)
	}
}

func TestCloseStopsGoroutines(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Leave an undelivered event pending so Close must not block on it.
	writeFile(t, root, "pending.md", "x")
	time.Sleep(200 * time.Millisecond)

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	deadline := time.After(waitTimeout)
	for {
		select {
		case _, ok := <-w.Events():
			if !ok {
				goto errorsClosed
			}
		case <-deadline:
			t.Fatal("events channel not closed after Close")
		}
	}
errorsClosed:
	select {
	case _, ok := <-w.Errors():
		if ok {
			t.Fatal("errors channel delivered a value after Close")
		}
	case <-time.After(waitTimeout):
		t.Fatal("errors channel not closed after Close")
	}
}

func TestNewFailsOnMissingRoot(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestIgnored(t *testing.T) {
	tests := []struct {
		rel  string
		want bool
	}{
		{".git", true},
		{".git/HEAD", true},
		{".git/objects/ab/cd", true},
		{"sub/.git/config", true},
		{"a.notty-tmp", true},
		{"Work/b.md.notty-tmp", true},
		{".notty/recovery", true},
		{".notty/recovery/x.md", true},
		{".notty/lock", true},
		{".notty/state.json", false},
		{".notty", false},
		{".gitignore", false},
		{"notes/.github/x.md", false},
		{"a.md", false},
		{"Work/Standup notes.md", false},
	}
	for _, tc := range tests {
		t.Run(tc.rel, func(t *testing.T) {
			if got := ignored(tc.rel); got != tc.want {
				t.Fatalf("ignored(%q) = %v, want %v", tc.rel, got, tc.want)
			}
		})
	}
}
