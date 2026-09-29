// Package resolver implements Notty's full-screen conflict resolver (spec
// §7 "Conflicts", §4.4 "Resolver"). The left side lists the conflicted
// files, each marked resolved or unresolved; the right side resolves the
// selected one:
//
//   - text files: three columns (Yours, Theirs, Result) built from a
//     block-level three-way diff against the base (a two-way diff when the
//     file was created on both sides), with per-block choices and an
//     embedded editor for the result;
//   - binary files: Keep yours / Keep theirs, with half-block previews of
//     images side by side;
//   - modify/delete conflicts: Keep edited / Delete;
//   - path (rename/rename) conflicts: Keep at the new path / Keep in trash,
//     or Keep A / Keep B / Keep both.
//
// The resolver never runs git or writes files. It emits ResolveTextMsg and
// ResolveChoiceMsg for the app to perform (the writes and staging go
// through the syncer's queue); the app reports back with MarkResolved or
// SetError. While the resolver is open the app must route every message to
// Update, not just key presses: the embedded editor's own messages
// (clipboard reads, flash timers) and the resolver's private messages come
// back through it.
package resolver

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// FileKind is how a conflicted file is resolved.
type FileKind int

// File kinds.
const (
	// Text files are merged block by block in three columns.
	Text FileKind = iota
	// Binary files offer Keep yours / Keep theirs.
	Binary
	// ModifyDelete files were edited on one side and deleted on the other.
	ModifyDelete
	// PathConflict files were moved or renamed differently on each side.
	PathConflict
)

func (k FileKind) String() string {
	switch k {
	case Text:
		return "Text"
	case Binary:
		return "Binary"
	case ModifyDelete:
		return "ModifyDelete"
	case PathConflict:
		return "PathConflict"
	}
	return "FileKind(?)"
}

// File is one conflicted file handed to the resolver by the app.
type File struct {
	Path string
	Kind FileKind
	// Base, Ours and Theirs are the index stage contents (:1:, :2:, :3:),
	// nil when a stage is absent. For a PathConflict they are the OursBlob
	// and TheirsBlob contents (Base unused).
	Base, Ours, Theirs []byte
	// Deleted says which side deleted a ModifyDelete file: "ours" or
	// "theirs".
	Deleted string
	// PathKind, Original, OursPath and TheirsPath describe a PathConflict.
	PathKind                       gitsync.PathConflictKind
	Original, OursPath, TheirsPath string
	// Resolved marks a file already resolved (and staged).
	Resolved bool
}

// ChoiceKind is the option picked on a choice screen.
type ChoiceKind int

// Choices. The zero value is not a valid choice.
const (
	KeepOurs      ChoiceKind = iota + 1 // binary: keep yours
	KeepTheirs                          // binary: keep theirs
	KeepEdited                          // modify/delete: keep the edited side
	Delete                              // modify/delete: delete the file
	KeepAtNewPath                       // trash vs move: keep at the moved path
	KeepInTrash                         // trash vs move: keep the trashed copy
	KeepPathA                           // rename/rename: keep yours (OursPath)
	KeepPathB                           // rename/rename: keep theirs (TheirsPath)
	KeepBoth                            // rename/rename or both trashed: keep both
)

var choiceNames = map[ChoiceKind]string{
	KeepOurs: "KeepOurs", KeepTheirs: "KeepTheirs", KeepEdited: "KeepEdited",
	Delete: "Delete", KeepAtNewPath: "KeepAtNewPath", KeepInTrash: "KeepInTrash",
	KeepPathA: "KeepPathA", KeepPathB: "KeepPathB", KeepBoth: "KeepBoth",
}

func (c ChoiceKind) String() string {
	if n, ok := choiceNames[c]; ok {
		return n
	}
	return "ChoiceKind(?)"
}

// ResolveTextMsg asks the app to write Content to Path and stage it. It is
// emitted by enter on a text file whose blocks are all resolved.
type ResolveTextMsg struct {
	Path    string
	Content []byte
}

// ResolveChoiceMsg asks the app to apply Choice to Path and stage the
// result (for a PathConflict, Path is the file's Path as given to New).
type ResolveChoiceMsg struct {
	Path   string
	Choice ChoiceKind
}

// CloseMsg asks the app to close the resolver, leaving the remaining
// conflicts unresolved.
type CloseMsg struct{}

// item is the resolver's state for one file.
type item struct {
	file    File
	err     string // last error reported by SetError
	pending bool   // a resolve request is out, waiting for the app
	text    *textState
	choice  *choiceState
}

// Model is the resolver view. Its setters return an updated copy; per-file
// state is copied on write, while the embedded editor shares its buffer
// between copies as usual for Bubble Tea models.
type Model struct {
	styles  theme.Styles
	palette theme.Palette
	caps    imgrender.Caps

	items []item
	sel   int

	ed      editor.Model
	vim     bool
	editing bool
	status  string // editor status message while editing

	pending string // first key of a two-key chord ("]" or "[")
	hint    string // transient hint shown under the selected file
	narrow  column // column shown on narrow terminals

	w, h int
}

// New returns a resolver for files, selecting the first unresolved one.
func New(files []File, styles theme.Styles, palette theme.Palette, caps imgrender.Caps, editorOpts editor.Options) Model {
	editorOpts.Styles, editorOpts.Palette = styles, palette
	m := Model{
		styles:  styles,
		palette: palette,
		caps:    caps,
		ed:      editor.New(editorOpts),
		vim:     editorOpts.Vim,
		narrow:  colResult,
	}
	m.items = make([]item, len(files))
	for i, f := range files {
		it := item{file: f}
		switch f.Kind {
		case Text:
			it.text = newTextState(f)
		default:
			it.choice = newChoiceState(f)
		}
		m.items[i] = it
	}
	m.sel = m.firstUnresolved(0)
	return m
}

// firstUnresolved returns the first unresolved file at or after from
// (wrapping around), or from itself when every file is resolved.
func (m Model) firstUnresolved(from int) int {
	n := len(m.items)
	for k := range n {
		if i := (from + k) % n; !m.items[i].file.Resolved {
			return i
		}
	}
	return min(max(from, 0), max(n-1, 0))
}

// index returns the position of path, or -1.
func (m Model) index(path string) int {
	for i, it := range m.items {
		if it.file.Path == path {
			return i
		}
	}
	return -1
}

// mutate replaces item i with the result of f applied to a copy, keeping
// earlier Model copies intact.
func (m Model) mutate(i int, f func(*item)) Model {
	if i < 0 || i >= len(m.items) {
		return m
	}
	m.items = slices.Clone(m.items)
	f(&m.items[i])
	return m
}

// SetSize sets the view's size in cells. The resolver is full screen and
// draws its own title and footer.
func (m Model) SetSize(w, h int) Model {
	m.w, m.h = max(0, w), max(0, h)
	m.ed = m.ed.SetSize(m.editorSize())
	return m.scrollToCurrent()
}

// Select selects the file at path (as named in the Files given to New);
// unknown paths are ignored.
func (m Model) Select(path string) Model {
	if i := m.index(path); i >= 0 && !m.editing {
		m.sel = i
		m.hint = ""
		m = m.scrollToCurrent()
	}
	return m
}

// Selected returns the path of the selected file, or "".
func (m Model) Selected() string {
	if m.sel < 0 || m.sel >= len(m.items) {
		return ""
	}
	return m.items[m.sel].file.Path
}

// MarkResolved marks path resolved, after the app wrote and staged it. When
// it is the selected file, the selection moves to the next unresolved one.
func (m Model) MarkResolved(path string) Model {
	i := m.index(path)
	if i < 0 {
		return m
	}
	m = m.mutate(i, func(it *item) {
		it.file.Resolved = true
		it.err = ""
		it.pending = false
	})
	if i == m.sel && !m.editing {
		m.sel = m.firstUnresolved(i)
		m.hint = ""
		m = m.scrollToCurrent()
	}
	return m
}

// SetError shows msg under path (a write that failed, an invalid TOML
// result...). An empty msg clears it.
func (m Model) SetError(path, msg string) Model {
	return m.mutate(m.index(path), func(it *item) {
		it.err = msg
		it.pending = false
	})
}

// AllResolved reports whether every file is resolved.
func (m Model) AllResolved() bool {
	for _, it := range m.items {
		if !it.file.Resolved {
			return false
		}
	}
	return true
}

// KeyContext returns keys.ResolverEdit while the embedded editor is open,
// keys.Resolver otherwise.
func (m Model) KeyContext() keys.Context {
	if m.editing {
		return keys.ResolverEdit
	}
	return keys.Resolver
}

// Editing reports whether the embedded editor is open.
func (m Model) Editing() bool { return m.editing }

// emit returns a Cmd delivering msg.
func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// current returns the selected item, or nil.
func (m Model) current() *item {
	if m.sel < 0 || m.sel >= len(m.items) {
		return nil
	}
	return &m.items[m.sel]
}

// Update handles key presses, pastes, the embedded editor's messages and
// the resolver's own messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.editing {
			return m.editKey(msg)
		}
		return m.navKey(msg)
	case tea.PasteMsg:
		if m.editing {
			return m.forwardToEditor(msg)
		}
		return m, nil
	case acceptEditMsg:
		return m.acceptEdit()
	case leaveEditMsg:
		return m.leaveEdit(), nil
	case editStatusMsg:
		if m.editing {
			m.status = msg.text
		}
		return m, nil
	}
	// Anything else may be one of the editor's internal messages (clipboard
	// reads, flash timers).
	return m.forwardToEditor(msg)
}

// navKey handles a key in the navigation context.
func (m Model) navKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	s := k.String()
	chord := m.pending
	m.pending = ""
	if chord != "" {
		if s == "c" {
			if chord == "]" {
				return m.jump(1), nil
			}
			return m.jump(-1), nil
		}
		// Any other key cancels the chord and is handled normally.
	}
	switch s {
	case "esc", "q":
		return m, emit(CloseMsg{})
	case "j", "down":
		return m.moveFile(1), nil
	case "k", "up":
		return m.moveFile(-1), nil
	case "]", "[":
		m.pending = s
		return m, nil
	}
	it := m.current()
	if it == nil || it.file.Resolved || it.pending {
		return m, nil
	}
	if it.text != nil {
		return m.textKey(s)
	}
	return m.choiceKey(s)
}

// moveFile moves the selection by d files.
func (m Model) moveFile(d int) Model {
	n := len(m.items)
	if n == 0 {
		return m
	}
	m.sel = min(max(m.sel+d, 0), n-1)
	m.hint = ""
	return m.scrollToCurrent()
}
