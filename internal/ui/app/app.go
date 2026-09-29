// Package app is Notty's root Bubble Tea model: it owns the layout, focus,
// key routing (spec §4.4), and the components on screen.
package app

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/history"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/preview"
	"github.com/mathieucroset/notty/internal/ui/sidebar"
	"github.com/mathieucroset/notty/internal/ui/statusbar"
	"github.com/mathieucroset/notty/internal/ui/tasksview"
	"github.com/mathieucroset/notty/internal/ui/textutil"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/ui/toast"
	"github.com/mathieucroset/notty/internal/ui/trash"
	"github.com/mathieucroset/notty/internal/vault"
	"github.com/mathieucroset/notty/internal/watcher"
)

// Options configures the app. The syncer pass adds its fields.
type Options struct {
	Config    config.Config
	Styles    theme.Styles
	Palette   theme.Palette
	Vault     *vault.Vault
	Local     *localstate.State
	LocalPath string
	Pins      *meta.State
	// Caps are the terminal's image capabilities, passed on to the preview
	// and the image viewer.
	Caps imgrender.Caps
	// WizardNeeded starts the app in first-run wizard mode, without a vault.
	WizardNeeded bool
	// Watcher reports changes made outside the app. The app listens to it
	// from Init and closes it on quit; nil disables watching.
	Watcher *watcher.Watcher
	// Now is the clock (time.Now when nil); tests fix it.
	Now func() time.Time
	// ConfigPath is the local config.toml that palette settings are
	// written to (config.ConfigPath() when empty).
	ConfigPath string
	// Clipboard is the system clipboard the editor pastes from
	// (clipboard.Default() when nil).
	Clipboard editor.Clipboard
}

// Focus is the pane with keyboard focus.
type Focus int

// Focus values.
const (
	FocusSidebar Focus = iota
	FocusMain
)

// MainView is what the main pane shows.
type MainView int

// Main pane views.
const (
	ViewNote MainView = iota
	ViewTasks
	ViewTrash
)

// NoteView is how the open note is shown, cycled with ctrl+g.
type NoteView int

// Note views.
const (
	ViewEditor NoteView = iota
	ViewSplit
	ViewPreview
)

const sidebarTitle = "◆ Notty"

// note describes the open note; its text lives in the editor buffer.
type note struct {
	path  string
	title string
	words int
}

// Model is the root model.
type Model struct {
	opts Options

	width, height  int
	sidebarVisible bool
	// sidebarToggled is set once the user shows or hides the sidebar;
	// from then on resizes no longer apply the narrow-terminal rule.
	sidebarToggled bool
	focus          Focus
	mainView       MainView
	noteView       NoteView

	sidebar sidebar.Model
	status  statusbar.Model
	toast   toast.Model
	// stickyErrors counts the error toasts still shown (they never expire).
	stickyErrors int
	// overlays is the overlay stack, top last.
	overlays []*overlayState

	// pinsSaver and localSaver keep snapshot writes in order.
	pinsSaver, localSaver *orderedSaver

	// ix is the note index, nil until the startup build finishes.
	ix       *index.Index
	indexing bool
	// pendingIndex holds paths changed while the startup index was
	// building; they are re-read once it lands.
	pendingIndex []string
	// filterTag is the tag the sidebar tree is filtered by, or "".
	filterTag string

	// tree is the last vault tree read.
	tree *vault.Node
	// pendingSelect is a path to select in the sidebar once the next tree
	// refresh lands (a note or folder just created, renamed or moved).
	pendingSelect string

	tasks      tasksview.Model
	openTasks  int
	trash      trash.Model
	trashCount int

	// history is the full-screen History view, or nil when closed.
	history     *history.Model
	historyPath string
	note        note
	openSeq     int // number of the latest open request
	sync        msgs.SyncStatusMsg

	// editor holds the open note's buffer.
	editor editor.Model
	// preview renders the buffer in split and preview views.
	preview preview.Model
	// kittyGen numbers the ready ticks: every tea.Exec starts a new
	// generation, dropping ticks armed before it.
	kittyGen int
	// focusReq is a focus change the editor asked for during its Update
	// (tab, ctrl+w h/l, esc with vim off), applied as soon as Update
	// returns so the next key already goes to the new pane.
	focusReq *Focus
	// editorStatus is the editor's last message (unknown command, search
	// wrapped), shown in the status row until the next key.
	editorStatus string
	// baseline is the open note's content as Notty last read or wrote it:
	// a change on disk only counts as external when the file differs
	// from it.
	baseline string
	// extConflict is the open note while the "changed on disk" dialog
	// waits for an answer; its saves are held until then.
	extConflict string
	// discardOnQuit is set when saving on quit failed: the very next quit
	// leaves without saving. Any buffer change or successful save clears
	// it.
	discardOnQuit bool

	// noteSavers order the saves of each note (UI goroutine only).
	noteSavers map[string]*orderedSaver
	// inflight counts the note saves still running; quit waits for them.
	inflight *sync.WaitGroup

	// deferred are commands produced by helpers that cannot return one
	// (a resize, a theme change); Update batches them with its result.
	deferred []tea.Cmd
}

// New builds the root model.
func New(opts Options) *Model {
	if opts.Local == nil {
		opts.Local = &localstate.State{Cursor: map[string][2]int{}}
	}
	if opts.Pins == nil {
		opts.Pins = &meta.State{}
	}
	m := &Model{
		opts:           opts,
		sidebarVisible: true,
		focus:          FocusSidebar,
		sidebar:        sidebar.New(opts.Styles),
		status:         statusbar.New(opts.Styles),
		toast:          toast.New(opts.Styles),
		pinsSaver:      &orderedSaver{},
		inflight:       &sync.WaitGroup{},
		localSaver:     &orderedSaver{},
		tasks:          tasksview.New(opts.Styles, opts.Config.Tasks.DueSoonDays, opts.Config.Tasks.ShowDone),
		trash:          trash.New(opts.Styles, opts.Palette),
		preview:        preview.New(opts.Styles, opts.Palette, opts.Caps, vaultRoot(opts)),
	}
	m.editor = newEditor(opts, m.mapEditorMsg)
	m.sidebar.SetExpanded(opts.Local.Expanded)
	m.sidebar.SetPins(opts.Pins.Pins)
	m.sidebar.SetFocused(true)
	return m
}

// vaultRoot is the absolute vault directory, or "" without a vault.
func vaultRoot(opts Options) string {
	if opts.Vault == nil {
		return ""
	}
	return opts.Vault.Root
}

// Focus returns the focused pane.
func (m *Model) Focus() Focus { return m.focus }

// SidebarVisible reports whether the sidebar is drawn.
func (m *Model) SidebarVisible() bool { return m.sidebarShown() }

// MainView returns what the main pane shows.
func (m *Model) MainView() MainView { return m.mainView }

// NoteView returns the current note view.
func (m *Model) NoteView() NoteView { return m.noteView }

// NotePath returns the vault-relative path of the open note, or "".
func (m *Model) NotePath() string { return m.note.path }

// Init loads the vault tree and builds the index.
func (m *Model) Init() tea.Cmd {
	if m.opts.WizardNeeded || m.opts.Vault == nil {
		return nil
	}
	m.indexing = true
	return tea.Batch(loadTreeCmd(m.opts.Vault), buildIndexCmd(m.opts.Vault), m.startupTrashCmd(),
		listenWatcherCmd(m.opts.Watcher), m.reopenLastNoteCmd(), m.readyTickCmd())
}

// reopenLastNoteCmd reopens the note open when the app last quit, at its
// saved cursor, if it still exists.
func (m *Model) reopenLastNoteCmd() tea.Cmd {
	last := m.opts.Local.LastNote
	if last == "" {
		return nil
	}
	abs := m.opts.Vault.Abs(last)
	return func() tea.Msg {
		if info, err := os.Stat(abs); err != nil || !info.Mode().IsRegular() {
			return nil
		}
		return msgs.OpenNoteMsg{Path: last, Line: -1}
	}
}

// Update handles a message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	if len(m.deferred) > 0 {
		cmd = tea.Batch(append([]tea.Cmd{cmd}, m.deferred...)...)
		m.deferred = nil
	}
	return m, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.sidebarToggled {
			m.sidebarVisible = SidebarStartsVisible(msg.Width)
		}
		m.relayout()
		return m, nil

	case tea.KeyPressMsg:
		return m, m.handleKey(msg)

	case treeLoadedMsg:
		if msg.err != nil {
			return m, errorToast("Could not read the vault: %v", msg.err)
		}
		m.tree = msg.root
		m.sidebar.SetTree(msg.root)
		if m.pendingSelect != "" {
			m.sidebar.Select(m.pendingSelect)
			m.pendingSelect = ""
			return m, m.syncExpanded()
		}
		return m, nil

	case tea.PasteMsg:
		return m, m.handlePaste(msg)

	case msgs.OpenNoteMsg:
		return m, m.openNote(msg)

	case noteLoadedMsg:
		if msg.seq != m.openSeq {
			return m, nil // superseded by a newer open request
		}
		if msg.err != nil {
			return m, errorToast("Could not open %s: %v", msg.path, msg.err)
		}
		if p := m.editor.Path(); p != "" && p != msg.path {
			// Typed into the old note while the new one was read: save
			// it before its buffer is replaced.
			if cmd := m.saveThen(msg, "it stays open"); cmd != nil {
				return m, cmd
			}
		}
		return m, m.showNote(msg.path, msg.content, msg.line)
	case loadNoteMsg:
		return m, m.loadNote(msg)
	case savedThenMsg:
		return m, m.handleSavedThen(msg)
	case runPendingMsg:
		return m, m.runPending(msg.op, msg.res)

	case editor.ChangedMsg:
		return m, m.handleEditorChanged(msg)
	case editor.AutosaveTickMsg:
		return m, m.updateEditor(msg)
	case editor.StatusMsg:
		m.editorStatus = msg.Text
	case readyTickMsg:
		if msg.gen == m.kittyGen {
			return m, m.terminalReady()
		}
	case tea.KeyboardEnhancementsMsg:
		return m, m.terminalReady()
	case msgs.SaveRequestMsg:
		return m, m.saveRequest()

	case msgs.FocusMainMsg:
		m.setFocus(FocusMain)
	case msgs.FocusSidebarMsg:
		m.focusSidebar()
	case msgs.ToggleSidebarMsg:
		m.toggleSidebar()
	case msgs.CycleNoteViewMsg:
		return m, m.cycleNoteView()
	case msgs.QuitMsg:
		return m, m.quit()
	case quitSavedMsg:
		return m, m.handleQuitSaved(msg)
	case msgs.ActivateEntryMsg:
		switch msg.Entry {
		case msgs.EntryTasks:
			m.showTasks()
		case msgs.EntryTrash:
			return m, m.showTrash()
		case msgs.EntryConflicts:
			return m, emit(msgs.OpenResolverMsg{})
		}
	case msgs.SyncStatusMsg:
		m.sync = msg
	case msgs.ToastMsg:
		return m, m.pushToast(msg.Level, msg.Text)
	case indexBuiltMsg:
		return m, m.handleIndexBuilt(msg)
	case indexChangedMsg:
		m.refreshIndexViews()
	case msgs.FilterTagMsg:
		m.filterTag = msg.Tag
		m.applyFilter()
	case msgs.ClearFilterMsg:
		m.filterTag = ""
		m.applyFilter()
	case msgs.TogglePinMsg:
		return m, m.togglePin(msg.Path)
	case msgs.RequestNewNote:
		return m, m.requestNewNote(msg.Folder)
	case msgs.RequestNewFolder:
		return m, m.requestNewFolder(msg.Parent)
	case msgs.RequestRename:
		return m, m.requestRename(msg.Path)
	case msgs.RequestMove:
		return m, m.requestMove(msg.Path)
	case msgs.RequestTrash:
		return m, m.requestTrash(msg.Path)
	case fileOpMsg:
		return m, m.handleFileOp(msg)
	case msgs.OpenFileExternalMsg:
		return m, m.editExternal(msg.Path)
	case externalSavedMsg:
		return m, m.handleExternalSaved(msg)
	case externalDoneMsg:
		return m, m.handleExternalDone(msg)
	case noteReloadedMsg:
		return m, m.handleNoteReloaded(msg)
	case watchEventMsg:
		return m, m.handleWatchEvent(msg)
	case watchErrMsg:
		return m, m.handleWatchErr(msg)
	case pathsGoneMsg:
		return m, m.handlePathsGone(msg)
	case savedMsg:
		return m, m.handleSaved(msg)
	case msgs.ImportImageMsg:
		return m, m.importImage(msg)
	case imageSizeMsg:
		return m, m.handleImageSize(msg)
	case imageImportedMsg:
		return m, m.handleImageImported(msg)
	case msgs.OpenImageViewerMsg:
		return m, m.openImageViewer(msg)
	case imageViewerDoneMsg:
		return m, m.handleImageViewerDone(msg)
	case msgs.ToggleTaskMsg:
		return m, m.toggleTask(msg)
	case taskToggledMsg:
		return m, m.handleTaskToggled(msg)
	case tasksview.BackMsg:
		m.mainView = ViewNote
	case dialog.ResultMsg:
		return m, m.handleDialogResult(msg)
	default:
		if cmd, ok := m.updateTrashMsg(msg); ok {
			return m, cmd
		}
		if cmd, ok := m.updateHistoryMsg(msg); ok {
			return m, cmd
		}
		if cmd, ok := m.updateCommandMsg(msg); ok {
			return m, cmd
		}
		// Everything else may belong to a component's own pipeline: the
		// editor's timers and clipboard reads, toast expiry ticks.
		edCmd := m.updateEditor(msg)
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Update(msg)
		return m, tea.Batch(edCmd, m.updatePreview(msg), m.updateFinders(msg), toastCmd)
	}
	return m, nil
}

// quitSavedMsg reports the buffer save that runs before quitting.
type quitSavedMsg struct{ saved savedMsg }

// quit runs the quit sequence (plan amendment A7): a dirty buffer is saved
// first, and a failed save cancels the quit (quitting again discards the
// edits); then finishQuit ends the program.
func (m *Model) quit() tea.Cmd {
	if !m.discardOnQuit && m.note.path != "" && m.editor.Dirty() {
		if save := m.saveEditorCmd(); save != nil {
			return func() tea.Msg {
				res, _ := save().(savedMsg)
				return quitSavedMsg{saved: res}
			}
		}
	}
	return m.finishQuit()
}

// handleQuitSaved quits once the buffer is saved.
func (m *Model) handleQuitSaved(msg quitSavedMsg) tea.Cmd {
	if msg.saved.err != nil {
		m.discardOnQuit = true
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not save %s: %v. Quit again to discard your edits.",
			msg.saved.path, msg.saved.err))
	}
	m.editor = m.editor.MarkSaved(msg.saved.path, msg.saved.version)
	return m.finishQuit()
}

// finishQuit stops the watcher, saves the local state (with the open
// note's cursor), deletes the Kitty images the preview transmitted, and
// ends the program. The state is written synchronously: nothing runs
// after tea.Quit.
// TODO(syncer pass, plan amendment A7): flush the syncer before quitting.
func (m *Model) finishQuit() tea.Cmd {
	m.waitSaves()
	m.closeWatcher()
	if m.opts.LocalPath != "" && !m.opts.WizardNeeded {
		m.rememberCursor()
		m.syncExpandedState()
		local, path := m.opts.Local, m.opts.LocalPath
		// Nowhere left to report an error.
		_ = m.localSaver.save(m.localSaver.ticket(), func() error { return local.Save(path) })
	}
	if cleanup := m.preview.KittyCleanup(); cleanup != "" {
		return tea.Sequence(tea.Raw(cleanup), tea.Quit)
	}
	return tea.Quit
}

// showNote loads a note read from disk into the editor and moves focus to
// the main pane. line is the line to put the cursor on, or -1 for the
// cursor saved when the note was last left.
func (m *Model) showNote(p, content string, line int) tea.Cmd {
	cur := buffer.Pos{}
	if line >= 0 {
		cur.Line = line
	} else if c, ok := m.opts.Local.Cursor[p]; ok {
		cur = buffer.Pos{Line: c[0], Col: c[1]}
	}
	m.editor = m.editor.SetReadOnly(m.isConflicted(p), "").Load(p, content, cur)
	m.editorStatus = ""
	m.extConflict = ""
	m.baseline = content
	m.note = note{
		path:  p,
		title: vault.Title(content, p),
		words: len(strings.Fields(content)),
	}
	m.sidebar.SetDirty("")
	m.mainView = ViewNote
	m.opts.Local.Touch(p)
	m.sidebar.Select(p)
	m.setFocus(FocusMain)
	m.syncExpandedState()
	return tea.Batch(m.saveLocalCmd(), m.syncPreview())
}

// sidebarShown reports whether the sidebar is actually drawn: it is
// toggled on and the terminal is wide enough for it.
func (m *Model) sidebarShown() bool {
	return ComputeLayout(m.width, m.height, m.sidebarVisible).SidebarVisible
}

// focusSidebar moves focus to the sidebar, showing it if it was hidden.
func (m *Model) focusSidebar() {
	if !m.sidebarShown() {
		m.sidebarVisible, m.sidebarToggled = true, true
		m.relayout()
	}
	m.setFocus(FocusSidebar)
}

// setFocus moves focus, never onto a sidebar that is not drawn.
func (m *Model) setFocus(f Focus) {
	if f == FocusSidebar && !m.sidebarShown() {
		f = FocusMain
	}
	m.focus = f
	m.sidebar.SetFocused(f == FocusSidebar)
	m.editor = m.editor.SetFocused(f == FocusMain)
}

func (m *Model) toggleSidebar() {
	m.sidebarVisible = !m.sidebarVisible
	m.sidebarToggled = true
	m.relayout()
}

// relayout resizes components and fixes focus after a layout change.
func (m *Model) relayout() {
	l := ComputeLayout(m.width, m.height, m.sidebarVisible)
	if l.SidebarVisible {
		m.sidebar.SetSize(l.Sidebar.W-2, l.Sidebar.H-2)
	} else if m.focus == FocusSidebar {
		m.setFocus(FocusMain)
	}
	// The Tasks and Trash views get a one-column gutter on each side.
	m.tasks = m.tasks.SetSize(max(l.Content.W-2*viewGutter, 0), l.Content.H)
	m.trash = m.trash.SetSize(max(l.Content.W-2*viewGutter, 0), l.Content.H)
	if m.history != nil {
		h := m.history.SetSize(m.width, m.height)
		m.history = &h
	}
	m.sizeNoteViews(l.Content.W, l.Content.H)
	m.resizeOverlay()
	m.status.SetSize(l.Status.W)
}

// syncExpandedState copies the sidebar's expanded folders into the local
// state, reporting whether they changed.
func (m *Model) syncExpandedState() bool {
	exp := m.sidebar.Expanded()
	cur := slices.Clone(m.opts.Local.Expanded)
	slices.Sort(cur)
	if slices.Equal(exp, cur) {
		return false
	}
	m.opts.Local.Expanded = exp
	return true
}

// syncExpanded saves the local state when the expanded folders changed.
func (m *Model) syncExpanded() tea.Cmd {
	if !m.syncExpandedState() {
		return nil
	}
	return m.saveLocalCmd()
}

// View renders the screen.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.Cursor = m.cursor()
	v.AltScreen = true
	v.WindowTitle = "Notty"
	return v
}

func (m *Model) render() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	st := m.opts.Styles
	if m.opts.WizardNeeded {
		msg := st.DialogTitle.Render("Setup wizard coming soon") + "\n\n" +
			st.Muted.Render("ctrl+q to quit")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	if m.history != nil {
		return m.withToasts(m.withOverlay(m.history.View()))
	}
	l := ComputeLayout(m.width, m.height, m.sidebarVisible)
	var panes []string
	if l.SidebarVisible {
		panes = append(panes, renderPane(st, sidebarTitle, "", m.sidebar.View(),
			l.Sidebar.W, l.Sidebar.H, m.focus == FocusSidebar))
	}
	panes = append(panes, m.renderMain(l))
	body := lipgloss.JoinHorizontal(lipgloss.Top, panes...)

	// Fill a copy of the status bar: rendering never changes the model.
	screen := m.statusRow()
	if l.Main.H > 0 {
		screen = body + "\n" + screen
	}
	return m.withToasts(m.withOverlay(screen))
}

// paneTitle is the main pane title: the note's folders and title, like
// "Work / Standup notes".
func (m *Model) paneTitle() string {
	switch m.mainView {
	case ViewTasks:
		return m.tasks.Title()
	case ViewTrash:
		return m.trash.Title()
	}
	if m.note.path == "" {
		if m.opts.Vault == nil {
			return ""
		}
		return filepath.Base(m.opts.Vault.Root)
	}
	parts := []string{}
	if d := path.Dir(m.note.path); d != "." {
		parts = strings.Split(d, "/")
	}
	return strings.Join(append(parts, m.note.title), " / ")
}

func (m *Model) renderMain(l Layout) string {
	st := m.opts.Styles
	var right []string
	if m.mainView == ViewNote && m.note.path != "" {
		switch m.noteView {
		case ViewSplit:
			right = append(right, "split")
		case ViewPreview:
			right = append(right, "preview")
		}
		if m.editor.Dirty() {
			right = append(right, "●")
		}
	}

	innerW, innerH := max(l.Main.W-2, 0), max(l.Main.H-2, 0)
	content := m.mainContent(l.Content.W, l.Content.H)
	// Offset the content inside the pane (zen layout centers it).
	pad := strings.Repeat(" ", max(l.Content.X-l.Main.X-1, 0))
	lines := fitBlock(content, l.Content.W, innerH)
	for i := range lines {
		lines[i] = textutil.PadLine(pad+lines[i], innerW)
	}
	return renderPane(st, m.paneTitle(), strings.Join(right, " "), strings.Join(lines, "\n"),
		l.Main.W, l.Main.H, m.focus == FocusMain)
}

// viewGutter is the blank columns left and right of the Tasks and Trash
// views, inside the pane border.
const viewGutter = 1

// withGutter indents every line of a main-pane view by the gutter (the
// right gutter comes from the view being sized narrower).
func withGutter(s string) string {
	pad := strings.Repeat(" ", viewGutter)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

// emptyHint is the main pane text when no note is open. It only suggests
// keys that work at the current size.
func (m *Model) emptyHint() string {
	switch {
	case m.sidebarShown():
		return "No note open · tab to browse notes"
	case ComputeLayout(m.width, m.height, true).SidebarVisible:
		return "No note open · ctrl+b to show notes"
	}
	return "No note open"
}

// mainContent renders the main-pane content at w×h.
func (m *Model) mainContent(w, h int) string {
	st := m.opts.Styles
	centered := func(s string) string {
		wrapped := st.Muted.Width(w).Align(lipgloss.Center).Render(s)
		return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, wrapped)
	}
	switch m.mainView {
	case ViewTasks:
		return withGutter(m.tasks.View())
	case ViewTrash:
		return withGutter(m.trash.View())
	}
	if m.note.path == "" {
		return centered(m.emptyHint())
	}
	return m.noteContent(w, h)
}
