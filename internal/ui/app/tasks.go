package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/tasks"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
	"github.com/mathieucroset/notty/internal/watcher"
)

// taskToggledMsg reports a task toggled on disk.
type taskToggledMsg struct {
	path     string
	notFound bool
	err      error
}

// now returns the current time (Options.Now in tests).
func (m *Model) now() time.Time {
	if m.opts.Now != nil {
		return m.opts.Now()
	}
	return time.Now()
}

// isConflicted reports whether p has an unresolved merge conflict, which
// makes it read-only.
// TODO(resolver pass, Task 34): answer from the syncer's conflict set.
func (m *Model) isConflicted(p string) bool {
	_ = p
	return false
}

// applyToggleInEditor applies a toggle to the open editor buffer (as one
// undo step), reporting whether it did. Autosave then writes it.
// TODO(editor pass): apply to the buffer when msg.Path is open.
func (m *Model) applyToggleInEditor(msg msgs.ToggleTaskMsg) bool {
	_ = msg
	return false
}

// refreshTasks rebuilds the Tasks view and the sidebar counts from the
// index.
func (m *Model) refreshTasks() {
	if m.ix == nil {
		return
	}
	refs := m.ix.AllTasks()
	m.tasks = m.tasks.SetTasks(refs, m.now())
	open := 0
	for _, r := range refs {
		if !r.Task.Done {
			open++
		}
	}
	m.openTasks = open
	m.updateCounts()
}

// updateCounts shows the Tasks and Trash counts on the sidebar entries.
func (m *Model) updateCounts() {
	m.sidebar.SetCounts(m.openTasks, 0, m.trashCount)
}

// toggleTask toggles a task from outside the editor (spec §5): in the
// buffer when the note is open, otherwise on disk.
func (m *Model) toggleTask(msg msgs.ToggleTaskMsg) tea.Cmd {
	if m.isConflicted(msg.Path) {
		return m.pushToast(msgs.ToastWarn, "Resolve the conflict in "+msg.Path+" first")
	}
	if m.applyToggleInEditor(msg) {
		return nil
	}
	if m.opts.Vault == nil {
		return nil
	}
	return toggleTaskCmd(m.opts.Vault, m.opts.Watcher, m.ix, msg)
}

// toggleTaskCmd reads the note, finds the task line (by number, or by text
// if the number drifted), toggles it, saves atomically and re-indexes.
func toggleTaskCmd(v *vault.Vault, w *watcher.Watcher, ix *index.Index, msg msgs.ToggleTaskMsg) tea.Cmd {
	return func() tea.Msg {
		defer lockFile(v.Abs(msg.Path))()
		content, err := v.Read(msg.Path)
		if err != nil {
			return taskToggledMsg{path: msg.Path, err: err}
		}
		crlf := strings.Contains(content, "\r\n")
		if crlf {
			content = strings.ReplaceAll(content, "\r\n", "\n")
		}
		lines := strings.Split(content, "\n")
		i, ok := tasks.FindLine(lines, msg.Line, strings.TrimRight(msg.Text, "\r"))
		if !ok {
			return taskToggledMsg{path: msg.Path, notFound: true}
		}
		lines[i] = tasks.ToggleLine(lines[i])
		out := strings.Join(lines, "\n")
		if crlf {
			out = strings.ReplaceAll(out, "\n", "\r\n")
		}
		if err := v.Save(msg.Path, out); err != nil {
			return taskToggledMsg{path: msg.Path, err: err}
		}
		if w != nil {
			w.NoteSelfWrite(msg.Path)
		}
		if ix != nil {
			_ = ix.Update(v, msg.Path)
		}
		return taskToggledMsg{path: msg.Path}
	}
}

// handleTaskToggled follows a toggle written to disk.
func (m *Model) handleTaskToggled(msg taskToggledMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not toggle the task in %s: %v", msg.path, msg.err))
	case msg.notFound:
		return m.pushToast(msgs.ToastWarn, "That task changed in "+msg.path+"; it was not toggled")
	}
	m.queueReindex(msg.path)
	m.refreshIndexViews()
	return m.reloadNoteIf(msg.path)
}

// showTasks switches the main pane to the Tasks view.
func (m *Model) showTasks() {
	m.refreshTasks()
	m.mainView = ViewTasks
	m.setFocus(FocusMain)
}
