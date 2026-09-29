package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
	version int
	err     error
	// restoredFrom is the short revision when the save restores a
	// history version, so the toast waits for the save to succeed.
	restoredFrom string
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
		cmds = append(cmds, m.pathRemoved(p))
	}
	return tea.Batch(cmds...)
}

// handleWatchErr reports a watcher error. When the vault root itself is
// gone the watcher stops for good.
func (m *Model) handleWatchErr(msg watchErrMsg) tea.Cmd {
	if errors.Is(msg.err, watcher.ErrRootGone) {
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

// saveNoteCmd saves content to the note at p atomically, tells the watcher
// the write is ours, and indexes the new content.
func (m *Model) saveNoteCmd(p, content string, version int) tea.Cmd {
	v, w, ix := m.opts.Vault, m.opts.Watcher, m.ix
	if v == nil {
		return nil
	}
	return func() tea.Msg {
		defer lockFile(v.Abs(p))()
		if err := v.Save(p, content); err != nil {
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

// handleSaved follows a finished save.
func (m *Model) handleSaved(msg savedMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not save %s: %v", msg.path, msg.err))
	}
	m.queueReindex(msg.path)
	if msg.path == m.note.path {
		m.handleNoteReloaded(noteReloadedMsg{path: msg.path, content: msg.content})
		m.note.dirty = false
	}
	m.refreshIndexViews()
	if msg.restoredFrom != "" {
		return m.pushToast(msgs.ToastInfo, fmt.Sprintf("Restored %s from %s", displayName(msg.path), msg.restoredFrom))
	}
	return nil
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
