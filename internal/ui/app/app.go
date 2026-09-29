// Package app is Notty's root Bubble Tea model: it owns the layout, focus,
// key routing (spec §4.4), and the components on screen.
package app

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/history"
	"github.com/mathieucroset/notty/internal/ui/msgs"
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

// Options configures the app. Later tasks add fields (capabilities, the
// syncer, the watcher).
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
	// InitialNote, if set, is the vault-relative note opened at start in
	// place of the last open note (notty new, spec §11).
	InitialNote string
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

// note is the open note. The content is a placeholder until the editor
// component exists (Task 18).
type note struct {
	path    string
	title   string
	content string
	words   int
	dirty   bool
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
		localSaver:     &orderedSaver{},
		tasks:          tasksview.New(opts.Styles, opts.Config.Tasks.DueSoonDays, opts.Config.Tasks.ShowDone),
		trash:          trash.New(opts.Styles, opts.Palette),
	}
	m.sidebar.SetExpanded(opts.Local.Expanded)
	m.sidebar.SetPins(opts.Pins.Pins)
	m.sidebar.SetFocused(true)
	return m
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
		listenWatcherCmd(m.opts.Watcher), m.reopenLastNoteCmd())
}

// reopenLastNoteCmd reopens the note open when the app last quit, at its
// saved cursor, if it still exists. Options.InitialNote takes precedence.
func (m *Model) reopenLastNoteCmd() tea.Cmd {
	if p := m.opts.InitialNote; p != "" {
		return func() tea.Msg { return msgs.OpenNoteMsg{Path: p, Line: 2} }
	}
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

	case msgs.OpenNoteMsg:
		if m.opts.Vault == nil {
			return m, nil
		}
		m.openSeq++
		return m, loadNoteCmd(m.opts.Vault, msg.Path, m.openSeq)

	case noteLoadedMsg:
		if msg.seq != m.openSeq {
			return m, nil // superseded by a newer open request
		}
		if msg.err != nil {
			return m, errorToast("Could not open %s: %v", msg.path, msg.err)
		}
		return m, m.showNote(msg.path, msg.content)

	case msgs.FocusMainMsg:
		m.setFocus(FocusMain)
	case msgs.FocusSidebarMsg:
		if !m.sidebarShown() {
			m.sidebarVisible, m.sidebarToggled = true, true
			m.relayout()
		}
		m.setFocus(FocusSidebar)
	case msgs.ToggleSidebarMsg:
		m.toggleSidebar()
	case msgs.CycleNoteViewMsg:
		m.cycleNoteView()
	case msgs.QuitMsg:
		return m, m.quit()
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
		return m, m.openExternal(msg.Path)
	case externalDoneMsg:
		return m, m.handleExternalDone(msg)
	case noteReloadedMsg:
		m.handleNoteReloaded(msg)
	case watchEventMsg:
		return m, m.handleWatchEvent(msg)
	case watchErrMsg:
		return m, m.handleWatchErr(msg)
	case pathsGoneMsg:
		return m, m.handlePathsGone(msg)
	case savedMsg:
		return m, m.handleSaved(msg)
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
		// Toast expiry ticks.
		var cmd tea.Cmd
		m.toast, cmd = m.toast.Update(msg)
		return m, cmd
	}
	return m, nil
}

// quit stops the watcher, saves the local state and ends the program.
// Both happen synchronously: nothing runs after tea.Quit.
// TODO(syncer pass): the full quit sequence (buffer save, kitty cleanup,
// syncer flush; plan amendment A7).
func (m *Model) quit() tea.Cmd {
	m.closeWatcher()
	if m.opts.LocalPath != "" && !m.opts.WizardNeeded {
		m.syncExpandedState()
		local, path := m.opts.Local, m.opts.LocalPath
		// Nowhere left to report an error.
		_ = m.localSaver.save(m.localSaver.ticket(), func() error { return local.Save(path) })
	}
	return tea.Quit
}

// showNote records the open note and moves focus to the main pane.
func (m *Model) showNote(p, content string) tea.Cmd {
	m.note = note{
		path:    p,
		title:   vault.Title(content, p),
		content: content,
		words:   len(strings.Fields(content)),
	}
	m.mainView = ViewNote
	m.opts.Local.Touch(p)
	m.sidebar.Select(p)
	m.setFocus(FocusMain)
	m.syncExpandedState()
	return m.saveLocalCmd()
}

// sidebarShown reports whether the sidebar is actually drawn: it is
// toggled on and the terminal is wide enough for it.
func (m *Model) sidebarShown() bool {
	return ComputeLayout(m.width, m.height, m.sidebarVisible).SidebarVisible
}

// setFocus moves focus, never onto a sidebar that is not drawn.
func (m *Model) setFocus(f Focus) {
	if f == FocusSidebar && !m.sidebarShown() {
		f = FocusMain
	}
	m.focus = f
	m.sidebar.SetFocused(f == FocusSidebar)
}

func (m *Model) toggleSidebar() {
	m.sidebarVisible = !m.sidebarVisible
	m.sidebarToggled = true
	m.relayout()
}

func (m *Model) cycleNoteView() {
	m.noteView = (m.noteView + 1) % 3
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

func (m *Model) modeLabel() string {
	if m.opts.Config.Vim {
		return "NORMAL"
	}
	return "PLAIN"
}

// View renders the screen.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
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
	status := m.status
	status.Mode = m.modeLabel()
	status.Path = m.note.path
	if m.indexing {
		status.Busy = "indexing…"
	}
	status.Words = m.note.words
	status.Sync = m.sync
	screen := status.View()
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
		if m.note.dirty {
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

// mainContent renders the placeholder main-pane content at w×h.
// TODO(Task 18): replace with the editor, preview, Tasks and Trash views.
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
	text := strings.NewReplacer("\r", "", "\t", "    ").Replace(strings.TrimRight(m.note.content, "\n"))
	return " " + strings.ReplaceAll(text, "\n", "\n ")
}
