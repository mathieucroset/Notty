package app

import (
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/trash"
	"github.com/mathieucroset/notty/internal/vault"
)

// Dialog IDs for the Trash view.
const (
	dlgDeleteForever = "delete-forever"
	dlgEmptyTrash    = "empty-trash"
)

// trashLoadedMsg carries the trash listing.
type trashLoadedMsg struct {
	items []vault.TrashItem
	err   error
}

// trashPreviewMsg carries the preview content of a trash item.
type trashPreviewMsg struct {
	id      string
	content string
}

// trashPurgedMsg reports the startup purge of old trash items.
type trashPurgedMsg struct {
	n   int
	err error
}

// restoredMsg reports a restored trash item. path is "" when the restore
// failed; err may be set alongside a path when only the cleanup failed.
type restoredMsg struct {
	path string
	err  error
}

// trashOpMsg reports a permanent deletion (one item, or the whole trash).
type trashOpMsg struct {
	op   opKind
	name string
	err  error
}

func loadTrashCmd(v *vault.Vault) tea.Cmd {
	if v == nil {
		return nil
	}
	return func() tea.Msg {
		items, err := v.TrashItems()
		return trashLoadedMsg{items: items, err: err}
	}
}

// purgeTrashCmd deletes trash items older than the retention period
// (spec §8), then the listing is reloaded.
func purgeTrashCmd(v *vault.Vault, days int, now time.Time) tea.Cmd {
	return func() tea.Msg {
		n, err := v.PurgeOlderThan(time.Duration(days)*24*time.Hour, now)
		return trashPurgedMsg{n: n, err: err}
	}
}

// startupTrashCmd purges old items when a retention period is set, and
// loads the trash listing.
func (m *Model) startupTrashCmd() tea.Cmd {
	if days := m.opts.Config.Trash.RetentionDays; days > 0 {
		return purgeTrashCmd(m.opts.Vault, days, m.now())
	}
	return loadTrashCmd(m.opts.Vault)
}

func (m *Model) handleTrashPurged(msg trashPurgedMsg) tea.Cmd {
	var toastCmd tea.Cmd
	if msg.err != nil {
		toastCmd = m.pushToast(msgs.ToastWarn, fmt.Sprintf("Could not purge old trash items: %v", msg.err))
	}
	return tea.Batch(toastCmd, loadTrashCmd(m.opts.Vault))
}

func (m *Model) handleTrashLoaded(msg trashLoadedMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not read the trash: %v", msg.err))
	}
	m.trash = m.trash.SetItems(msg.items, m.now())
	m.trashCount = len(msg.items)
	m.updateCounts()
	return m.loadSelectedPreview()
}

// loadSelectedPreview loads the preview of the selected trash item.
func (m *Model) loadSelectedPreview() tea.Cmd {
	if it, ok := m.trash.Selected(); ok {
		return loadTrashPreviewCmd(m.opts.Vault, it)
	}
	return nil
}

// loadTrashPreviewCmd reads a trash item for the preview: a note's
// content, a folder's listing, or a line for other files.
func loadTrashPreviewCmd(v *vault.Vault, it vault.TrashItem) tea.Cmd {
	if v == nil {
		return nil
	}
	return func() tea.Msg {
		rel := v.TrashContentPath(it)
		var content string
		switch {
		case it.IsDir:
			var b strings.Builder
			b.WriteString("# " + it.Name + "\n\n")
			entries, err := os.ReadDir(v.Abs(rel))
			if err != nil {
				b.WriteString("Could not list the folder: " + err.Error() + "\n")
			}
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				b.WriteString("- " + name + "\n")
			}
			content = b.String()
		case strings.EqualFold(path.Ext(it.Name), ".md"):
			c, err := v.Read(rel)
			if err != nil {
				c = "Could not read the note: " + err.Error()
			}
			content = c
		default:
			content = "`" + it.Name + "` is not a note."
		}
		return trashPreviewMsg{id: it.ID, content: content}
	}
}

// showTrash switches the main pane to the Trash view.
func (m *Model) showTrash() tea.Cmd {
	m.mainView = ViewTrash
	m.setFocus(FocusMain)
	return loadTrashCmd(m.opts.Vault)
}

func restoreCmd(v *vault.Vault, ix *index.Index, it vault.TrashItem) tea.Cmd {
	return func() tea.Msg {
		p, err := v.Restore(it)
		if p != "" && ix != nil {
			reindexPath(v, ix, p)
		}
		return restoredMsg{path: p, err: err}
	}
}

func (m *Model) handleRestored(msg restoredMsg) tea.Cmd {
	if msg.path == "" {
		return tea.Batch(m.pushToast(msgs.ToastError, fmt.Sprintf("Could not restore: %v", msg.err)),
			loadTrashCmd(m.opts.Vault))
	}
	m.queueReindex(msg.path)
	m.noteChanged(msg.path)
	cmds := []tea.Cmd{
		m.pushToast(msgs.ToastInfo, "Restored to "+msg.path),
		loadTreeCmd(m.opts.Vault),
		loadTrashCmd(m.opts.Vault),
	}
	if msg.err != nil {
		cmds = append(cmds, m.pushToast(msgs.ToastWarn, fmt.Sprintf("Restored, but the trash entry was not cleaned up: %v", msg.err)))
	}
	m.pendingSelect = msg.path
	m.refreshIndexViews()
	return tea.Batch(cmds...)
}

func (m *Model) requestDeleteForever(it vault.TrashItem) tea.Cmd {
	d := dialog.NewConfirm(dlgDeleteForever, "Delete forever", "Delete '"+it.Name+"' forever? This cannot be undone.",
		"Delete", "Cancel", true, m.opts.Styles)
	m.openDialog(d, pendingOp{kind: opDeleteForever, item: it})
	return nil
}

func (m *Model) requestEmptyTrash() tea.Cmd {
	if m.trashCount == 0 {
		return m.pushToast(msgs.ToastInfo, "The trash is already empty")
	}
	what := "the item"
	if m.trashCount > 1 {
		what = fmt.Sprintf("all %d items", m.trashCount)
	}
	d := dialog.NewConfirm(dlgEmptyTrash, "Empty trash", "Delete "+what+" in the trash forever? This cannot be undone.",
		"Empty trash", "Cancel", true, m.opts.Styles)
	m.openDialog(d, pendingOp{kind: opEmptyTrash})
	return nil
}

func deleteForeverCmd(v *vault.Vault, it vault.TrashItem) tea.Cmd {
	return func() tea.Msg {
		return trashOpMsg{op: opDeleteForever, name: it.Name, err: v.DeleteForever(it)}
	}
}

func emptyTrashCmd(v *vault.Vault) tea.Cmd {
	return func() tea.Msg {
		return trashOpMsg{op: opEmptyTrash, err: v.EmptyTrash()}
	}
}

func (m *Model) handleTrashOp(msg trashOpMsg) tea.Cmd {
	var text string
	level := msgs.ToastInfo
	switch {
	case msg.err != nil:
		level, text = msgs.ToastError, fmt.Sprintf("Could not delete: %v", msg.err)
	case msg.op == opEmptyTrash:
		text = "Emptied the trash"
	default:
		text = "Deleted '" + msg.name + "' forever"
	}
	if msg.err == nil {
		m.noteChanged(".trash")
	}
	return tea.Batch(m.pushToast(level, text), loadTrashCmd(m.opts.Vault))
}

// updateTrashMsg handles the Trash view's messages.
func (m *Model) updateTrashMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case trash.SelectionChangedMsg:
		return loadTrashPreviewCmd(m.opts.Vault, msg.Item), true
	case trashPreviewMsg:
		m.trash = m.trash.SetPreview(msg.id, msg.content)
	case trash.RestoreMsg:
		if m.opts.Vault == nil {
			return nil, true
		}
		return restoreCmd(m.opts.Vault, m.ix, msg.Item), true
	case trash.DeleteForeverMsg:
		return m.requestDeleteForever(msg.Item), true
	case trash.EmptyTrashMsg:
		return m.requestEmptyTrash(), true
	case trash.BackMsg:
		m.mainView = ViewNote
	case trashLoadedMsg:
		return m.handleTrashLoaded(msg), true
	case trashPurgedMsg:
		return m.handleTrashPurged(msg), true
	case restoredMsg:
		return m.handleRestored(msg), true
	case trashOpMsg:
		return m.handleTrashOp(msg), true
	default:
		return nil, false
	}
	return nil, true
}
