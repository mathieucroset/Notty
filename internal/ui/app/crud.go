package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// Dialog IDs for the note and folder operations.
const (
	dlgNewNote   = "new-note"
	dlgNewFolder = "new-folder"
	dlgRename    = "rename"
	dlgMove      = "move"
	dlgTrash     = "trash"
)

// fileOpMsg reports a finished vault operation. path is the resulting path
// ("" when the operation failed); err may be set alongside a path when the
// operation succeeded but rewriting image links failed.
type fileOpMsg struct {
	op   opKind
	old  string
	path string
	err  error
}

// externalDoneMsg reports that $EDITOR exited after editing path.
type externalDoneMsg struct {
	path string
	err  error
}

// displayName is the name shown for a vault path: its base name, without
// ".md" for notes.
func displayName(p string) string {
	base := path.Base(p)
	if strings.EqualFold(path.Ext(base), ".md") {
		return base[:len(base)-3]
	}
	return base
}

// isUnder reports whether p is target or lies inside the folder target.
func isUnder(p, target string) bool {
	return p == target || strings.HasPrefix(p, target+"/")
}

// validationError is a dialog validation message, shown as is under the
// input.
type validationError string

func (e validationError) Error() string { return string(e) }

// requireName is the validation for name inputs.
func requireName(s string) error {
	if strings.TrimSpace(s) == "" {
		return validationError("Enter a name")
	}
	return nil
}

func inFolder(what, folder string) string {
	if folder == "" {
		return what
	}
	return what + " in " + folder
}

func (m *Model) requestNewNote(folder string) tea.Cmd {
	if m.opts.Vault == nil {
		return nil
	}
	if folder == "" {
		// From the palette: the sidebar's selection decides, as n would.
		folder = m.sidebar.SelectedFolder()
	}
	d := dialog.NewInput(dlgNewNote, inFolder("New note", folder), "Note title", "", requireName, m.opts.Styles)
	m.openDialog(d, pendingOp{kind: opNewNote, path: folder})
	return nil
}

func (m *Model) requestNewFolder(parent string) tea.Cmd {
	if m.opts.Vault == nil {
		return nil
	}
	if parent == "" {
		parent = m.sidebar.SelectedFolder()
	}
	d := dialog.NewInput(dlgNewFolder, inFolder("New folder", parent), "Folder name", "", requireName, m.opts.Styles)
	m.openDialog(d, pendingOp{kind: opNewFolder, path: parent})
	return nil
}

func (m *Model) requestRename(p string) tea.Cmd {
	if m.opts.Vault == nil || p == "" {
		return nil
	}
	d := dialog.NewInput(dlgRename, "Rename '"+displayName(p)+"'", "New name", displayName(p), requireName, m.opts.Styles)
	m.openDialog(d, pendingOp{kind: opRename, path: p})
	return nil
}

func (m *Model) requestMove(p string) tea.Cmd {
	if m.opts.Vault == nil || p == "" {
		return nil
	}
	folders := m.moveTargets(p)
	suggest := func(prefix string) []string {
		q := strings.ToLower(strings.Trim(prefix, "/"))
		if q == "" {
			// Nothing highlighted, so enter on an empty field means the
			// vault root.
			return nil
		}
		var starts, contains []string
		for _, f := range folders {
			lf := strings.ToLower(f)
			switch {
			case strings.HasPrefix(lf, q):
				starts = append(starts, f)
			case strings.Contains(lf, q):
				contains = append(contains, f)
			}
		}
		return append(starts, contains...)
	}
	// Only existing folders: a typo must not silently create a folder.
	validate := func(s string) error {
		dest := strings.Trim(strings.TrimSpace(s), "/")
		if dest == "" || slices.Contains(folders, dest) || dest == parentOf(p) {
			return nil
		}
		return validationError("No such folder")
	}
	d := dialog.NewInput(dlgMove, "Move '"+displayName(p)+"' to", "Folder (empty for the vault root)",
		parentOf(p), validate, m.opts.Styles).WithSuggestions(suggest)
	m.openDialog(d, pendingOp{kind: opMove, path: p})
	return nil
}

func (m *Model) requestTrash(p string) tea.Cmd {
	if m.opts.Vault == nil || p == "" {
		return nil
	}
	d := dialog.NewConfirm(dlgTrash, "Move to trash", "Move '"+displayName(p)+"' to trash?",
		"Move to trash", "Cancel", true, m.opts.Styles)
	m.openDialog(d, pendingOp{kind: opTrash, path: p})
	return nil
}

// parentOf returns the folder containing p ("" for the vault root).
func parentOf(p string) string {
	if d := path.Dir(p); d != "." {
		return d
	}
	return ""
}

// moveTargets lists the folders p can move into, sorted: every folder in
// the tree except p itself and anything inside it.
func (m *Model) moveTargets(p string) []string {
	var out []string
	var walk func(n *vault.Node)
	walk = func(n *vault.Node) {
		for _, c := range n.Children {
			if !c.IsDir || isUnder(c.Path, p) {
				continue
			}
			out = append(out, c.Path)
			walk(c)
		}
	}
	if m.tree != nil {
		walk(m.tree)
	}
	slices.Sort(out)
	return out
}

// createNoteCmd creates a note and indexes it.
func createNoteCmd(v *vault.Vault, ix *index.Index, folder, title string) tea.Cmd {
	return func() tea.Msg {
		rel, err := v.CreateNote(folder, title)
		if err == nil && ix != nil {
			_ = ix.Update(v, rel)
		}
		return fileOpMsg{op: opNewNote, path: rel, err: err}
	}
}

func createFolderCmd(v *vault.Vault, parent, name string) tea.Cmd {
	return func() tea.Msg {
		rel, err := v.CreateFolder(parent, name)
		return fileOpMsg{op: opNewFolder, path: rel, err: err}
	}
}

// renameCmd renames (op opRename) or moves (op opMove) rel, and moves its
// index entries along.
func renameCmd(v *vault.Vault, ix *index.Index, op opKind, rel, arg string) tea.Cmd {
	return func() tea.Msg {
		var newRel string
		var err error
		if op == opMove {
			newRel, err = v.Move(rel, arg)
		} else {
			newRel, err = v.Rename(rel, arg)
		}
		if newRel != "" && ix != nil {
			ix.Rename(rel, newRel)
		}
		return fileOpMsg{op: op, old: rel, path: newRel, err: err}
	}
}

func trashCmd(v *vault.Vault, ix *index.Index, rel string) tea.Cmd {
	return func() tea.Msg {
		if _, err := v.Trash(rel); err != nil {
			return fileOpMsg{op: opTrash, old: rel, err: err}
		}
		if ix != nil {
			ix.Remove(rel)
		}
		return fileOpMsg{op: opTrash, old: rel, path: rel}
	}
}

// opVerb names an operation in error messages.
func opVerb(op opKind) string {
	switch op {
	case opNewNote:
		return "create the note"
	case opNewFolder:
		return "create the folder"
	case opRename:
		return "rename"
	case opMove:
		return "move"
	case opTrash:
		return "move to trash"
	}
	return "do that"
}

// friendlyError turns a vault error into toast text.
func friendlyError(op opKind, err error) string {
	switch {
	case errors.Is(err, vault.ErrExists):
		return fmt.Sprintf("Could not %s: a note or folder with that name already exists", opVerb(op))
	case errors.Is(err, vault.ErrInvalidName):
		return fmt.Sprintf("Could not %s: that name is not valid", opVerb(op))
	case errors.Is(err, vault.ErrInvalidPath):
		return fmt.Sprintf("Could not %s: that location is not allowed", opVerb(op))
	}
	return fmt.Sprintf("Could not %s: %v", opVerb(op), err)
}

// handleFileOp brings the app up to date after a vault operation.
func (m *Model) handleFileOp(msg fileOpMsg) tea.Cmd {
	if msg.path == "" {
		return m.pushToast(msgs.ToastError, friendlyError(msg.op, msg.err))
	}
	var cmds []tea.Cmd
	if msg.err != nil {
		cmds = append(cmds, m.pushToast(msgs.ToastWarn,
			fmt.Sprintf("Moved '%s', but some image links could not be updated: %v", displayName(msg.path), msg.err)))
	}
	switch msg.op {
	case opNewNote:
		m.pendingSelect = msg.path
		cmds = append(cmds, emit(msgs.OpenNoteMsg{Path: msg.path, Line: 2}))
	case opNewFolder:
		m.pendingSelect = msg.path
	case opRename, opMove:
		m.pendingSelect = msg.path
		cmds = append(cmds, m.pathRenamed(msg.old, msg.path))
	case opTrash:
		cmds = append(cmds, m.pathRemoved(msg.old), loadTrashCmd(m.opts.Vault),
			m.pushToast(msgs.ToastInfo, fmt.Sprintf("Moved '%s' to trash", displayName(msg.old))))
	}
	m.refreshIndexViews()
	cmds = append(cmds, loadTreeCmd(m.opts.Vault))
	return tea.Batch(cmds...)
}

// pathRenamed follows a rename or move of oldPath to newPath in the pins,
// the local state and the open note.
func (m *Model) pathRenamed(oldPath, newPath string) tea.Cmd {
	m.opts.Pins.Rename(oldPath, newPath)
	m.sidebar.SetPins(m.opts.Pins.Pins)
	m.opts.Local.Rename(oldPath, newPath)
	m.sidebar.SetExpanded(m.opts.Local.Expanded)
	if m.note.path != "" && isUnder(m.note.path, oldPath) {
		m.note.path = newPath + m.note.path[len(oldPath):]
		m.note.title = vault.Title(m.note.content, m.note.path)
	}
	return tea.Batch(m.savePinsCmd(), m.saveLocalCmd())
}

// pathRemoved forgets p (and everything under it) in the pins, the local
// state and the open note.
func (m *Model) pathRemoved(p string) tea.Cmd {
	m.opts.Pins.Remove(p)
	m.sidebar.SetPins(m.opts.Pins.Pins)
	m.opts.Local.Remove(p)
	if m.note.path != "" && isUnder(m.note.path, p) {
		m.note = note{}
		m.openSeq++ // drop any load still in flight
	}
	return tea.Batch(m.savePinsCmd(), m.saveLocalCmd())
}

// editorCommand builds the $EDITOR command for the file at abs.
func (m *Model) editorCommand(abs string) *exec.Cmd {
	fields := strings.Fields(m.opts.Config.EditorCommand(os.Getenv, runtime.GOOS))
	if len(fields) == 0 {
		fields = []string{"nano"}
	}
	return exec.Command(fields[0], append(fields[1:], abs)...) //nolint:gosec // the user's own editor
}

// openExternal hands the vault file at rel to $EDITOR.
func (m *Model) openExternal(rel string) tea.Cmd {
	if m.opts.Vault == nil || rel == "" {
		return nil
	}
	return tea.ExecProcess(m.editorCommand(m.opts.Vault.Abs(rel)), func(err error) tea.Msg {
		return externalDoneMsg{path: rel, err: err}
	})
}

// handleExternalDone re-reads a file edited in $EDITOR.
func (m *Model) handleExternalDone(msg externalDoneMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, fmt.Sprintf("The editor failed: %v", msg.err))
	}
	return tea.Batch(reindexCmd(m.opts.Vault, m.ix, []string{msg.path}), loadTreeCmd(m.opts.Vault), m.reloadNoteIf(msg.path))
}
