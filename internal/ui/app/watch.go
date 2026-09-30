package app

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
	"github.com/mathieucroset/notty/internal/watcher"
)

// watchEventMsg is one debounced batch of paths changed outside the app.
type watchEventMsg struct{ paths []string }

// pathsGoneMsg lists changed paths that no longer exist on disk.
type pathsGoneMsg struct{ paths []string }

// watchErrMsg is a watcher error.
type watchErrMsg struct{ err error }

// savedMsg reports a finished note save. version is the caller's buffer
// version at the time of the save, so the editor can tell whether edits
// were made since.
type savedMsg struct {
	path    string
	content string
	version uint64
	err     error
	// restoredFrom is the short revision when the save restores a
	// history version, so the toast waits for the save to succeed.
	restoredFrom string
	// stale reports a snapshot skipped because a newer one of the same
	// note was already written.
	stale bool
}

// listenWatcherCmd waits for the next watcher event or error. It returns
// nil (ending the loop) once the watcher is closed.
func listenWatcherCmd(w *watcher.Watcher) tea.Cmd {
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case ev, ok := <-w.Events():
			if !ok {
				return nil
			}
			return watchEventMsg{paths: ev.Paths}
		case err, ok := <-w.Errors():
			if !ok {
				return nil
			}
			return watchErrMsg{err: err}
		}
	}
}

// handleWatchEvent re-indexes the changed paths, refreshes the tree and
// reloads the open note if it changed, then keeps listening.
func (m *Model) handleWatchEvent(msg watchEventMsg) tea.Cmd {
	m.queueReindex(msg.paths...)
	m.noteChanged(msg.paths...)
	return tea.Batch(
		reindexCmd(m.opts.Vault, m.ix, msg.paths),
		loadTreeCmd(m.opts.Vault),
		m.reloadNoteIf(msg.paths...),
		goneCmd(m.opts.Vault, msg.paths),
		listenWatcherCmd(m.opts.Watcher),
	)
}

// goneCmd reports which of paths were deleted (or renamed away).
func goneCmd(v *vault.Vault, paths []string) tea.Cmd {
	if v == nil {
		return nil
	}
	return func() tea.Msg {
		var gone []string
		for _, p := range paths {
			if _, err := os.Lstat(v.Abs(p)); errors.Is(err, fs.ErrNotExist) {
				gone = append(gone, p)
			}
		}
		if len(gone) == 0 {
			return nil
		}
		return pathsGoneMsg{paths: gone}
	}
}

// handlePathsGone forgets deleted paths in the pins, the local state
// (recents, expanded folders) and the open note.
func (m *Model) handlePathsGone(msg pathsGoneMsg) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(msg.paths))
	for _, p := range msg.paths {
		cmds = append(cmds, m.pathRemoved(p, true))
	}
	return tea.Batch(cmds...)
}

// handleWatchErr reports a watcher error. When the vault root itself is
// gone the watcher stops for good. Lost events are recovered by indexing
// the whole vault again and re-reading the tree and the open note.
func (m *Model) handleWatchErr(msg watchErrMsg) tea.Cmd {
	switch {
	case errors.Is(msg.err, watcher.ErrEventsLost):
		slog.Warn("file watcher lost events: re-indexing the vault", "err", msg.err)
		return tea.Batch(m.rebuildIndex(), loadTreeCmd(m.opts.Vault), m.reloadNoteIf(m.note.path),
			listenWatcherCmd(m.opts.Watcher))
	case errors.Is(msg.err, watcher.ErrWatchStopped):
		return tea.Batch(m.pushToast(msgs.ToastError, fmt.Sprintf(
			"Notty stopped watching the vault: changes made outside Notty are no longer noticed. Restart Notty to watch it again. (%v)",
			msg.err)), listenWatcherCmd(m.opts.Watcher))
	case errors.Is(msg.err, watcher.ErrRootGone):
		var closeCmd tea.Cmd
		if w := m.opts.Watcher; w != nil {
			m.opts.Watcher = nil
			closeCmd = func() tea.Msg {
				_ = w.Close()
				return nil
			}
		}
		return tea.Batch(closeCmd, m.pushToast(msgs.ToastError,
			"The vault folder was removed or renamed: changes made outside Notty are no longer noticed"))
	}
	return tea.Batch(m.pushToast(msgs.ToastWarn, fmt.Sprintf("File watcher: %v", msg.err)),
		listenWatcherCmd(m.opts.Watcher))
}

// closeWatcher stops the watcher, if any.
func (m *Model) closeWatcher() {
	if m.opts.Watcher != nil {
		_ = m.opts.Watcher.Close()
		m.opts.Watcher = nil
	}
}

// changedOnDiskError refuses a save of the open note whose file changed
// since Notty last read or wrote it; content is what the file holds.
type changedOnDiskError struct{ content string }

func (e *changedOnDiskError) Error() string {
	return "the file changed on disk since Notty last read it"
}

// diskCheck is what a save of the open note expects to find on disk.
type diskCheck struct {
	baseline string
	gen      uint64
}

// verify reads the note at p and returns a *changedOnDiskError unless it
// holds the baseline, or what saver last wrote under the same baseline
// generation (a save still being reported). A missing file overwrites
// nothing and passes. saver.mu is held.
func (c *diskCheck) verify(v *vault.Vault, p string, saver *orderedSaver) error {
	disk, err := v.Read(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("check the file on disk: %w", err)
	case disk == c.baseline, saver.hasLast && saver.lastGen == c.gen && disk == saver.last:
		return nil
	}
	return &changedOnDiskError{content: disk}
}

// saveNoteCmd saves content to the note at p atomically, tells the watcher
// the write is ours, and indexes the new content.
//
// A save of the open note first checks that the file still holds what
// Notty last read or wrote (the baseline): a file changed by another
// program is never overwritten, whether or not the watcher noticed; the
// save fails with a *changedOnDiskError instead.
//
// Snapshots of one note land in the order they were taken: an older one
// that runs after a newer one was written is skipped (stale). Quit waits
// for the saves still running (waitSaves).
func (m *Model) saveNoteCmd(p, content string, version uint64) tea.Cmd {
	v, w, ix := m.opts.Vault, m.opts.Watcher, m.ix
	if v == nil {
		return nil
	}
	if m.noteSavers == nil {
		m.noteSavers = map[string]*orderedSaver{}
	}
	saver := m.noteSavers[p]
	if saver == nil {
		saver = &orderedSaver{}
		m.noteSavers[p] = saver
	}
	var check *diskCheck
	if p == m.note.path && p == m.editor.Path() {
		check = &diskCheck{baseline: m.baseline, gen: m.baselineGen}
	}
	seq := saver.ticket()
	inflight := m.inflight
	inflight.Add(1)
	return func() tea.Msg {
		defer inflight.Done()
		defer lockFile(v.Abs(p))()
		written := false
		err := saver.save(seq, func() error {
			written = true
			if check != nil {
				if err := check.verify(v, p, saver); err != nil {
					return err
				}
			}
			if err := v.Save(p, content); err != nil {
				return err
			}
			if check != nil {
				saver.last, saver.lastGen, saver.hasLast = content, check.gen, true
			}
			return nil
		})
		if !written {
			return savedMsg{path: p, version: version, stale: true}
		}
		if err != nil {
			return savedMsg{path: p, version: version, err: err}
		}
		if w != nil {
			w.NoteSelfWrite(p)
		}
		if ix != nil {
			ix.UpdateContent(p, content)
		}
		return savedMsg{path: p, content: content, version: version}
	}
}

// handleSaved follows a finished save and tells the syncer.
func (m *Model) handleSaved(msg savedMsg) tea.Cmd {
	cmd := m.applySaved(msg)
	if !msg.stale && msg.err == nil {
		m.noteChanged(msg.path)
	}
	return cmd
}

// applySaved brings the editor, the sidebar and the index views up to date
// after a save.
func (m *Model) applySaved(msg savedMsg) tea.Cmd {
	if msg.stale {
		return nil // a newer snapshot was written
	}
	if cmd, ok := m.changedOnDisk(msg); ok {
		return cmd
	}
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not save %s: %v", msg.path, msg.err))
	}
	m.queueReindex(msg.path)
	m.discardOnQuit = false // a later quit must save again
	if msg.path == m.editor.Path() {
		m.baseline = msg.content
	}
	m.editor = m.editor.MarkSaved(msg.path, msg.version)
	m.sidebar.SetDirty(m.dirtyPath())
	m.refreshIndexViews()
	if msg.restoredFrom != "" {
		return m.pushToast(msgs.ToastInfo, fmt.Sprintf("Restored %s from %s", displayName(msg.path), msg.restoredFrom))
	}
	return nil
}

// changedOnDisk handles a save refused because the open note's file
// changed on disk: the file is treated as re-read, which asks what to do
// when the buffer has other edits (or reloads it). It reports whether the
// save was refused for that reason.
func (m *Model) changedOnDisk(msg savedMsg) (tea.Cmd, bool) {
	var ce *changedOnDiskError
	if !errors.As(msg.err, &ce) {
		return nil, false
	}
	return m.handleNoteReloaded(noteReloadedMsg{path: msg.path, content: ce.content}), true
}

// saveWaitLimit bounds how long quitting waits for saves still running.
const saveWaitLimit = 2 * time.Second

// waitSaves waits, up to saveWaitLimit, for the saves still running.
func (m *Model) waitSaves() {
	done := make(chan struct{})
	go func() {
		m.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(saveWaitLimit):
	}
}

// reindexCmd re-reads paths into the index off the UI goroutine. A path
// naming a folder re-reads every note inside it.
func reindexCmd(v *vault.Vault, ix *index.Index, paths []string) tea.Cmd {
	if v == nil || ix == nil || len(paths) == 0 {
		return nil
	}
	return func() tea.Msg {
		for _, p := range paths {
			reindexPath(v, ix, p)
		}
		return indexChangedMsg{}
	}
}

// reindexPath updates the index for the file or folder at rel.
func reindexPath(v *vault.Vault, ix *index.Index, rel string) {
	info, err := os.Stat(v.Abs(rel))
	if err != nil || !info.IsDir() {
		_ = ix.Update(v, rel)
		return
	}
	_ = filepath.WalkDir(v.Abs(rel), func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && abs != v.Abs(rel) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(abs), ".md") {
			return nil
		}
		if r, err := filepath.Rel(v.Root, abs); err == nil {
			_ = ix.Update(v, filepath.ToSlash(r))
		}
		return nil
	})
}
