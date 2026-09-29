package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
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

func TestSelfWriteInSubdirectorySuppressed(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "Work/Projects")
	w := newTestWatcher(t, root)

	atomicSave(t, root, "Work/Projects/plan.md", "saved by app")
	w.NoteSelfWrite("Work/Projects/plan.md")

	expectNoEvent(t, w)
}

func TestSameSizeExternalEditAfterSelfWriteReported(t *testing.T) {
	if !haveFileID {
		t.Skip("no inode/ctime on this platform; only mtime+size are compared")
	}
	root := t.TempDir()
	w := newTestWatcher(t, root)

	atomicSave(t, root, "note.md", "AAAA")
	w.NoteSelfWrite("note.md")
	expectNoEvent(t, w)

	// Same size, and the mtime forced back to the recorded one: only the
	// ctime (and, for a replace, the inode) reveals the external edit.
	p := filepath.Join(root, "note.md")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	writeFile(t, root, "note.md", "BBBB")
	if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"note.md"}) {
		t.Fatalf("paths = %v, want [note.md]", ev.Paths)
	}
}

func TestSelfWriteStamp(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, p string)
		after  time.Duration // time elapsed since NoteSelfWrite
		want   bool
		fileID bool // needs inode/ctime support
	}{
		{name: "unchanged", want: true},
		{name: "unchanged but expired", after: selfWriteTTL + time.Second, want: false},
		{name: "rewritten with other size", change: func(t *testing.T, p string) {
			if err := os.WriteFile(p, []byte("longer content"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: false},
		{name: "replaced, same size and mtime", fileID: true, change: func(t *testing.T, p string) {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p+".other", []byte("BBBB"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p+".other", info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(p+".other", p); err != nil {
				t.Fatal(err)
			}
		}, want: false},
		{name: "deleted", change: func(t *testing.T, p string) {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
		}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.fileID && !haveFileID {
				t.Skip("no inode/ctime on this platform")
			}
			root := t.TempDir()
			w, err := newWatcher(root) // event loop not started
			if err != nil {
				t.Fatalf("newWatcher: %v", err)
			}
			t.Cleanup(func() { _ = w.Close() })

			writeFile(t, root, "Sub/note.md", "AAAA")
			w.NoteSelfWrite("Sub/note.md")
			if tc.change != nil {
				tc.change(t, filepath.Join(root, "Sub", "note.md"))
			}
			w.mu.Lock()
			got := w.isSelfWrite("Sub/note.md", time.Now().Add(tc.after))
			w.mu.Unlock()
			if got != tc.want {
				t.Fatalf("isSelfWrite = %v, want %v", got, tc.want)
			}
		})
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

func TestMaxDelayFlushesOnlyQuietPaths(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	// hot.md is rewritten every 20ms for 1.6s, so the vault never has a 100ms
	// quiet window. After maxDelay (1s) the quiet path must still be
	// delivered, without the hot one; hot.md follows once it settles.
	stop := make(chan struct{})
	stalled := make(chan time.Duration, 1)
	go func() {
		var maxGap time.Duration
		last := time.Now()
		for i := 0; ; i++ {
			select {
			case <-stop:
				stalled <- maxGap
				return
			default:
			}
			_ = os.WriteFile(filepath.Join(root, "hot.md"), []byte{byte(i)}, 0o644)
			now := time.Now()
			maxGap = max(maxGap, now.Sub(last))
			last = now
			time.Sleep(20 * time.Millisecond)
		}
	}()
	time.Sleep(10 * time.Millisecond)
	writeFile(t, root, "quiet.md", "x")
	start := time.Now()

	var first Event
	select {
	case first = <-w.Events():
	case <-time.After(1600 * time.Millisecond):
	}
	close(stop)
	if gap := <-stalled; gap > 70*time.Millisecond {
		t.Skipf("writer stalled for %v; machine too slow to test maxDelay", gap)
	}
	if first.Paths == nil {
		t.Fatal("no event while hot.md kept changing: maxDelay cap not applied")
	}
	if !slices.Equal(first.Paths, []string{"quiet.md"}) {
		t.Fatalf("capped batch paths = %v, want [quiet.md]", first.Paths)
	}
	if d := time.Since(start); d < 800*time.Millisecond {
		t.Fatalf("capped batch after %v, want about maxDelay (1s)", d)
	}

	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"hot.md"}) {
		t.Fatalf("paths after settling = %v, want [hot.md]", ev.Paths)
	}
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

func TestChangesBeforePauseDelivered(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	// Pause right after the write: the kernel event may not have been read
	// yet, and the barrier in Pause must make sure it is not dropped.
	writeFile(t, root, "before.md", "user wrote this")
	w.Pause()
	defer w.Resume()

	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"before.md"}) {
		t.Fatalf("paths = %v, want [before.md]", ev.Paths)
	}
	expectNoEvent(t, w)
}

func TestChangesBeforeResumeDropped(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	w.Pause()
	// Resume right after the write: the barrier in Resume must process the
	// paused write's event before events flow again.
	writeFile(t, root, "during.md", "git wrote this")
	w.Resume()
	writeFile(t, root, "after.md", "user wrote this")

	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"after.md"}) {
		t.Fatalf("paths = %v, want [after.md]", ev.Paths)
	}
	expectNoEvent(t, w)
}

func TestPauseNests(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	w.Pause()
	w.Pause()
	w.Resume() // still paused once
	writeFile(t, root, "inner.md", "x")
	expectNoEvent(t, w)

	w.Resume()
	w.Resume() // unbalanced: no-op
	writeFile(t, root, "after.md", "y")
	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"after.md"}) {
		t.Fatalf("paths = %v, want [after.md]", ev.Paths)
	}
}

func TestBarrierCompletesThroughSentinelEvent(t *testing.T) {
	tests := []struct {
		name     string
		nottyDir bool
	}{
		{"with .notty", true},
		{"creates .notty", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.nottyDir {
				mkdir(t, root, ".notty")
			}
			w := newTestWatcher(t, root)

			// Unlike Pause, no timeout: req is only closed once the
			// sentinel's creation event has come back through the loop.
			req := make(chan struct{})
			w.ctl <- req
			select {
			case <-req:
			case <-time.After(waitTimeout):
				t.Fatal("barrier never completed")
			}
			expectNoEvent(t, w) // neither the sentinel nor .notty is reported
		})
	}
}

func TestPauseLeavesNoBarrierFileBehind(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)

	w.Pause()
	w.Resume()
	expectNoEvent(t, w)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(barrierRel))); !os.IsNotExist(err) {
		t.Fatalf("barrier file still present (stat err = %v)", err)
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

	waitClosed(t, "Events", w.Events())
	waitClosed(t, "Errors", w.Errors())
}

// waitClosed drains ch until it is closed, failing after waitTimeout.
func waitClosed[T any](t *testing.T, name string, ch <-chan T) {
	t.Helper()
	deadline := time.After(waitTimeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("%s channel not closed after Close", name)
		}
	}
}

func TestUnreadableSubdirectoryIsNotFatal(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root user on a POSIX system to deny directory access")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	mkdir(t, root, "locked/inner")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	w := newTestWatcher(t, root)

	select {
	case err := <-w.Errors():
		if err == nil {
			t.Fatal("nil error delivered")
		}
	case <-time.After(waitTimeout):
		t.Fatal("no error reported for the unreadable directory")
	}

	writeFile(t, root, "a.md", "still watched")
	ev := nextEvent(t, w) // also fails on any further error
	if !slices.Equal(ev.Paths, []string{"a.md"}) {
		t.Fatalf("paths = %v, want [a.md]", ev.Paths)
	}
}

func TestUnreadableNewSubdirectoryIsNotFatal(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root user on a POSIX system to deny directory access")
	}
	root := t.TempDir()
	w := newTestWatcher(t, root)

	// Created with no permissions at all, so the watcher cannot watch it.
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	select {
	case err := <-w.Errors():
		if err == nil {
			t.Fatal("nil error delivered")
		}
	case <-time.After(waitTimeout):
		t.Fatal("no error reported for the unreadable directory")
	}
	collectUntil(t, w, "locked")

	writeFile(t, root, "sub/a.md", "still watched")
	collectUntil(t, w, "sub/a.md")
}

func TestRootRemovalReportsError(t *testing.T) {
	tests := []struct {
		name   string
		remove func(root string) error
	}{
		{"renamed", func(root string) error { return os.Rename(root, root+"-moved") }},
		{"deleted", os.RemoveAll},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "vault")
			writeFile(t, root, "a.md", "x")
			w := newTestWatcher(t, root)

			if err := tc.remove(root); err != nil {
				t.Fatalf("remove root: %v", err)
			}
			deadline := time.After(waitTimeout)
			for {
				select {
				case <-w.Events(): // the deleted note may be reported; ignore
					continue
				case err := <-w.Errors():
					if !errors.Is(err, ErrRootGone) {
						t.Fatalf("error = %v, want ErrRootGone", err)
					}
				case <-deadline:
					t.Fatal("no error reported for the removed root")
				}
				break
			}
		})
	}
}

func TestOverflowDeliveredWhenErrorBufferFullAndWatchesResynced(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, "missed")
	w, err := newWatcher(root)
	if err != nil {
		t.Fatalf("newWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// Simulate a directory whose creation event was lost in the overflow.
	if err := w.fsw.Remove(filepath.Join(w.root, "missed")); err != nil {
		t.Fatalf("remove watch: %v", err)
	}
	delete(w.dirs, "missed")
	// Fill the Errors buffer so an ordinary send would be dropped.
	filler := errors.New("filler")
	for range cap(w.errors) {
		w.sendError(filler)
	}
	w.handleFSError(fsnotify.ErrEventOverflow)
	w.handleFSError(fsnotify.ErrEventOverflow) // sticky: queued only once
	w.start()

	var overflows int
	for range cap(w.errors) + 1 {
		select {
		case err := <-w.Errors():
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				overflows++
			}
		case <-time.After(waitTimeout):
			t.Fatal("timed out draining errors")
		}
	}
	if overflows != 1 {
		t.Fatalf("overflow errors = %d, want 1", overflows)
	}

	writeFile(t, root, "missed/a.md", "x")
	ev := nextEvent(t, w)
	if !slices.Equal(ev.Paths, []string{"missed/a.md"}) {
		t.Fatalf("paths = %v, want [missed/a.md]", ev.Paths)
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
