package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// keyContext returns the context that receives non-global keys.
func (m *Model) keyContext() keys.Context {
	if m.opts.WizardNeeded {
		return keys.Wizard
	}
	if m.overlayOpen() {
		return keys.Overlay
	}
	if m.history != nil {
		return keys.History
	}
	if m.focus == FocusSidebar {
		if m.sidebar.PickerOpen() {
			// The inline tag picker behaves like an overlay: ctrl+k moves
			// up instead of opening the palette.
			return keys.Overlay
		}
		return keys.Sidebar
	}
	switch m.mainView {
	case ViewTasks:
		return keys.TasksView
	case ViewTrash:
		return keys.TrashView
	}
	if m.noteView == ViewPreview {
		return keys.Preview
	}
	if m.note.path != "" {
		return m.editorContext()
	}
	if m.opts.Config.Vim {
		return keys.EditorNormal
	}
	return keys.EditorPlain
}

// handleKey routes a key press through the layers in keys.Route.
func (m *Model) handleKey(k tea.KeyPressMsg) tea.Cmd {
	if a, global := keys.Route(m.keyContext(), m.overlayOpen(), k); global {
		return m.handleAction(a)
	}
	if m.opts.WizardNeeded {
		return nil // TODO(Task 33): route keys to the wizard.
	}
	if k.String() == "R" && m.trashWarningNewest() && restoreKeyContext(m.keyContext()) {
		return m.restoreDeletedRemotely()
	}
	if m.overlayOpen() {
		return m.updateOverlay(k)
	}
	if m.history != nil {
		h, cmd := m.history.Update(k)
		m.history = &h
		return cmd
	}
	if m.focus == FocusSidebar {
		// esc dismisses the newest sticky error before it reaches the
		// sidebar (where it clears the tag filter).
		if k.String() == "esc" && !m.sidebar.PickerOpen() && m.dismissToast() {
			return nil
		}
		var cmd tea.Cmd
		m.sidebar, cmd = m.sidebar.Update(k)
		return tea.Batch(cmd, m.syncExpanded())
	}
	return m.handleMainKey(k)
}

// handleMainKey sends a key to the main pane: the Tasks or Trash view, the
// editor (editor and split views) or the preview (preview view).
func (m *Model) handleMainKey(k tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	switch m.mainView {
	case ViewTasks:
		m.tasks, cmd = m.tasks.Update(k)
		return cmd
	case ViewTrash:
		m.trash, cmd = m.trash.Update(k)
		return cmd
	}
	if m.note.path != "" {
		if m.noteView != ViewPreview {
			return m.handleEditorKey(k)
		}
		if k.String() == "esc" && m.dismissToast() {
			return nil
		}
		return m.updatePreview(k)
	}
	switch k.String() {
	case "tab":
		return emit(msgs.FocusSidebarMsg{})
	case "esc":
		// Nothing in the empty main pane uses esc: it dismisses the
		// newest error toast.
		m.dismissToast()
	}
	return nil
}

// handleAction performs a global action.
func (m *Model) handleAction(a keys.Action) tea.Cmd {
	switch a {
	case keys.Quit:
		return m.quit()
	case keys.Help:
		return emit(msgs.OpenHelpMsg{})
	case keys.ToggleSidebar:
		m.toggleSidebar()
	case keys.CycleView:
		return m.cycleNoteView()
	case keys.Finder:
		return emit(msgs.OpenFinderMsg{})
	case keys.Search:
		return emit(msgs.OpenFinderMsg{FullText: true})
	case keys.Palette:
		return emit(msgs.OpenPaletteMsg{})
	case keys.Save:
		return emit(msgs.SaveRequestMsg{})
	case keys.ExternalEditor:
		return m.editExternal(m.note.path)
	}
	return nil
}

// restoreKeyContext reports whether R on a trash warning toast restores its
// note in ctx: everywhere keys are not typed or taken by vim (R is vim's
// replace mode, so the editor keeps it; the palette entry works there).
func restoreKeyContext(ctx keys.Context) bool {
	switch ctx {
	case keys.Sidebar, keys.Preview, keys.TasksView, keys.TrashView, keys.EditorReadOnly:
		return true
	}
	return false
}
