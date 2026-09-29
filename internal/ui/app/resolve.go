package app

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/BurntSushi/toml"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/syncer"
	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/resolver"
	"github.com/mathieucroset/notty/internal/vault"
)

// resolveMu serializes the resolver's git operations. While the syncer is in
// Conflict it runs no git command of its own (spec §7: commits, fetches and
// pushes are paused), so the resolver works on the repository directly.
var resolveMu sync.Mutex

// Resolver messages.
type (
	// conflictsLoadedMsg carries the conflicted files read from git.
	conflictsLoadedMsg struct {
		files []resolver.File
		sel   string
		err   error
	}
	// fileResolvedMsg reports a resolved (written and staged) file. paths
	// are the vault paths written or removed.
	fileResolvedMsg struct {
		path  string
		paths []string
		err   error
	}
	// mergeCommittedMsg reports the merge commit made once every file was
	// resolved.
	mergeCommittedMsg struct{ err error }
)

// conflictRefusal is the toast for a write refused on a conflicted path.
func conflictRefusal(p string) string {
	return fmt.Sprintf("'%s' has a sync conflict — press c to resolve", displayName(p))
}

// refuseConflicted refuses a write to p while it (or something inside it)
// is conflicted (spec §7 "Conflicted files are protected on every write
// path"). ok reports a refusal.
func (m *Model) refuseConflicted(p string) (tea.Cmd, bool) {
	if !m.isConflicted(p) {
		return nil, false
	}
	return m.pushToast(msgs.ToastWarn, conflictRefusal(p)), true
}

// openResolver opens the conflict resolver on p ("" for the first
// unresolved file).
func (m *Model) openResolver(p string) tea.Cmd {
	if m.repo == nil || (len(m.conflicted) == 0 && m.syncStatus.State != syncer.Conflict) {
		return m.pushToast(msgs.ToastInfo, "No conflicts to resolve")
	}
	if m.resolver != nil {
		if p != "" {
			r := m.resolver.Select(p)
			var cmd tea.Cmd
			r, cmd = r.LoadPreviews()
			m.resolver = &r
			return cmd
		}
		return nil
	}
	return loadConflictsCmd(m.repo, p)
}

// loadConflictsCmd reads the merge in progress into resolver files. Paths
// deleted on both sides need no decision and are resolved on the way.
func loadConflictsCmd(repo *gitsync.Repo, sel string) tea.Cmd {
	return func() tea.Msg {
		resolveMu.Lock()
		defer resolveMu.Unlock()
		files, err := conflictFiles(repo)
		return conflictsLoadedMsg{files: files, sel: sel, err: err}
	}
}

// conflictFiles builds the resolver's files from git: path conflicts first,
// then text, binary and modify/delete conflicts.
func conflictFiles(repo *gitsync.Repo) ([]resolver.File, error) {
	pcs, rest, err := repo.PathConflicts()
	if err != nil {
		return nil, fmt.Errorf("read conflicts: %w", err)
	}
	var files []resolver.File
	for _, pc := range pcs {
		ours, _ := repo.ShowBlob(pc.OursBlob)
		theirs, _ := repo.ShowBlob(pc.TheirsBlob)
		files = append(files, resolver.File{
			Path: pc.Original, Kind: resolver.PathConflict, PathKind: pc.Kind,
			Original: pc.Original, OursPath: pc.Ours, TheirsPath: pc.Theirs,
			Ours: ours, Theirs: theirs,
		})
	}
	var bothDeleted []string
	for _, c := range rest {
		stage := func(n int) []byte {
			if c.Blob[n] == "" {
				return nil
			}
			b, err := repo.Show(n, c.Path)
			if err != nil {
				return nil
			}
			return b
		}
		f := resolver.File{Path: c.Path}
		switch c.Kind {
		case gitsync.BothDeleted:
			bothDeleted = append(bothDeleted, c.Path)
			continue
		case gitsync.DeletedByThem, gitsync.AddedByUs:
			f.Kind, f.Deleted, f.Base, f.Ours = resolver.ModifyDelete, "theirs", stage(1), stage(2)
		case gitsync.DeletedByUs, gitsync.AddedByThem:
			f.Kind, f.Deleted, f.Base, f.Theirs = resolver.ModifyDelete, "ours", stage(1), stage(3)
		default:
			f.Base, f.Ours, f.Theirs = stage(1), stage(2), stage(3)
			if repo.IsBinary(c.Path) {
				f.Kind = resolver.Binary
			}
		}
		files = append(files, f)
	}
	if len(bothDeleted) > 0 {
		if err := repo.Remove(bothDeleted...); err != nil {
			return nil, fmt.Errorf("resolve deleted files: %w", err)
		}
	}
	return files, nil
}

// resolverEditorOptions configures the resolver's embedded editor like the
// note editor.
func (m *Model) resolverEditorOptions() editor.Options {
	return editor.Options{
		Vim:         m.opts.Config.Vim,
		LineNumbers: m.opts.Config.LineNumbers,
		Clipboard:   m.editorClipboard(),
		VaultRoot:   vaultRoot(m.opts),
	}
}

// handleConflictsLoaded opens the resolver full screen.
func (m *Model) handleConflictsLoaded(msg conflictsLoadedMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not read the conflicts: %v", msg.err))
	case len(msg.files) == 0:
		// Resolved outside Notty (or only deletions were left): let the
		// syncer commit the merge.
		if m.syncSvc != nil {
			m.syncSvc.ConflictResolved()
		}
		return m.pushToast(msgs.ToastInfo, "No conflicts left to resolve")
	}
	m.conflictFiles = make(map[string]resolver.File, len(msg.files))
	for _, f := range msg.files {
		m.conflictFiles[f.Path] = f
	}
	// Closing a picker mid-preview restores the original theme, which the
	// resolver is then built with.
	m.closeOverlayKind(overlayPalette)
	r := resolver.New(msg.files, m.opts.Styles, m.opts.Palette, m.opts.Caps, m.resolverEditorOptions()).
		SetSize(m.width, m.height)
	if msg.sel != "" {
		r = r.Select(msg.sel)
	}
	var cmd tea.Cmd
	r, cmd = r.LoadPreviews()
	m.resolver = &r
	return cmd
}

// updateResolver forwards a message to the open resolver.
func (m *Model) updateResolver(msg tea.Msg) tea.Cmd {
	r, cmd := m.resolver.Update(msg)
	m.resolver = &r
	return cmd
}

// updateResolverMsg handles the resolver's messages.
func (m *Model) updateResolverMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case conflictsLoadedMsg:
		return m.handleConflictsLoaded(msg), true
	case resolver.ResolveTextMsg:
		if m.resolver == nil || m.repo == nil {
			return nil, true
		}
		return resolveTextCmd(m.repo, m.host, msg.Path, msg.Content), true
	case resolver.ResolveChoiceMsg:
		f, ok := m.conflictFiles[msg.Path]
		if m.resolver == nil || m.repo == nil || !ok {
			return nil, true
		}
		return resolveChoiceCmd(m.repo, m.host, f, msg.Choice), true
	case fileResolvedMsg:
		return m.handleFileResolved(msg), true
	case mergeCommittedMsg:
		return m.handleMergeCommitted(msg), true
	case resolver.CloseMsg:
		m.resolver = nil // the conflicts stay; c reopens it
		return nil, true
	}
	if m.resolver != nil && resolver.Owns(msg) {
		return m.updateResolver(msg), true
	}
	return nil, false
}

// validateResult refuses a .toml result that does not parse (spec §7).
func validateResult(p string, content []byte) error {
	if !strings.EqualFold(path.Ext(p), ".toml") {
		return nil
	}
	var v map[string]any
	if _, err := toml.Decode(string(content), &v); err != nil {
		first, _, _ := strings.Cut(err.Error(), "\n")
		return errors.New("Invalid TOML: " + first)
	}
	return nil
}

// resolveTextCmd writes a merged text result and stages it, with the
// watcher paused (plan amendment A8).
func resolveTextCmd(repo *gitsync.Repo, host *syncHost, p string, content []byte) tea.Cmd {
	return func() tea.Msg {
		if err := validateResult(p, content); err != nil {
			return fileResolvedMsg{path: p, err: err}
		}
		resolveMu.Lock()
		defer resolveMu.Unlock()
		host.PauseWatcher()
		defer host.ResumeWatcher()
		if err := writeRepoFile(repo.Dir, p, content); err != nil {
			return fileResolvedMsg{path: p, err: err}
		}
		if err := repo.Add(p); err != nil {
			return fileResolvedMsg{path: p, err: err}
		}
		return fileResolvedMsg{path: p, paths: []string{p}}
	}
}

// resolveChoiceCmd applies a choice (spec §7): keep a side's content, keep
// or delete an edited file, or pick the paths of a rename conflict. Chosen
// paths get their side's committed content and are staged; the others are
// removed.
func resolveChoiceCmd(repo *gitsync.Repo, host *syncHost, f resolver.File, choice resolver.ChoiceKind) tea.Cmd {
	return func() tea.Msg {
		resolveMu.Lock()
		defer resolveMu.Unlock()
		host.PauseWatcher()
		defer host.ResumeWatcher()
		paths, err := applyChoice(repo, f, choice)
		return fileResolvedMsg{path: f.Path, paths: paths, err: err}
	}
}

// keep writes data to p and stages it.
func keep(repo *gitsync.Repo, p string, data []byte) error {
	if err := writeRepoFile(repo.Dir, p, data); err != nil {
		return err
	}
	return repo.Add(p)
}

// drop removes paths from the index and the working tree, then drops a
// trash item left with nothing but its meta.json.
func drop(repo *gitsync.Repo, paths ...string) error {
	if err := repo.Remove(paths...); err != nil {
		return err
	}
	for _, p := range paths {
		if id := trashID(p); id != "" {
			if err := dropEmptyTrashItem(repo, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// dropEmptyTrashItem removes .trash/<id>/ when only its meta.json is left.
func dropEmptyTrashItem(repo *gitsync.Repo, id string) error {
	dir := filepath.Join(repo.Dir, ".trash", id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // already gone
	}
	for _, e := range entries {
		if e.Name() != "meta.json" {
			return nil
		}
	}
	if err := repo.Remove(".trash/" + id + "/meta.json"); err != nil {
		return err
	}
	_ = os.Remove(dir)
	return nil
}

// applyChoice performs choice on f and returns the paths it wrote or
// removed.
func applyChoice(repo *gitsync.Repo, f resolver.File, choice resolver.ChoiceKind) ([]string, error) {
	switch f.Kind {
	case resolver.Binary:
		switch choice {
		case resolver.KeepOurs:
			return []string{f.Path}, keepSide(repo, f.Path, f.Ours)
		case resolver.KeepTheirs:
			return []string{f.Path}, keepSide(repo, f.Path, f.Theirs)
		}
	case resolver.ModifyDelete:
		switch choice {
		case resolver.KeepEdited:
			edited := f.Ours
			if f.Deleted == "ours" {
				edited = f.Theirs
			}
			return []string{f.Path}, keep(repo, f.Path, edited)
		case resolver.Delete:
			return []string{f.Path}, drop(repo, f.Path)
		}
	case resolver.PathConflict:
		return applyPathChoice(repo, f, choice)
	}
	return nil, fmt.Errorf("choice %v does not apply to %s", choice, f.Path)
}

// keepSide keeps one side of a binary conflict; a side that deleted the file
// resolves it as deleted.
func keepSide(repo *gitsync.Repo, p string, data []byte) error {
	if data == nil {
		return drop(repo, p)
	}
	return keep(repo, p, data)
}

// applyPathChoice resolves a rename/rename conflict: each kept path gets its
// own side's content, every other path (and the original) is removed.
func applyPathChoice(repo *gitsync.Repo, f resolver.File, choice resolver.ChoiceKind) ([]string, error) {
	type side struct {
		path string
		data []byte
	}
	ours, theirs := side{f.OursPath, f.Ours}, side{f.TheirsPath, f.Theirs}
	var kept, removed []side
	switch choice {
	case resolver.KeepAtNewPath, resolver.KeepInTrash:
		trashSide, moveSide := ours, theirs
		if !strings.HasPrefix(ours.path, ".trash/") {
			trashSide, moveSide = theirs, ours
		}
		if choice == resolver.KeepAtNewPath {
			kept, removed = []side{moveSide}, []side{trashSide}
		} else {
			kept, removed = []side{trashSide}, []side{moveSide}
		}
	case resolver.KeepPathA:
		kept, removed = []side{ours}, []side{theirs}
	case resolver.KeepPathB:
		kept, removed = []side{theirs}, []side{ours}
	case resolver.KeepBoth:
		kept = []side{ours, theirs}
	default:
		return nil, fmt.Errorf("choice %v does not apply to %s", choice, f.Path)
	}
	touched := []string{f.Original}
	for _, s := range kept {
		if err := keep(repo, s.path, s.data); err != nil {
			return touched, err
		}
		touched = append(touched, s.path)
	}
	drops := []string{f.Original}
	for _, s := range removed {
		drops = append(drops, s.path)
		touched = append(touched, s.path)
	}
	return touched, drop(repo, drops...)
}

// writeRepoFile writes data to the vault-relative p atomically.
func writeRepoFile(root, p string, data []byte) error {
	abs := filepath.Join(root, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	tmp := abs + ".notty-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

// handleFileResolved marks a file resolved (or shows the error), refreshes
// what it touched and, once every file is resolved, commits the merge.
func (m *Model) handleFileResolved(msg fileResolvedMsg) tea.Cmd {
	if m.resolver == nil {
		return nil
	}
	if msg.err != nil {
		r := m.resolver.SetError(msg.path, msg.err.Error())
		m.resolver = &r
		return nil
	}
	r := m.resolver.MarkResolved(msg.path)
	var load tea.Cmd
	r, load = r.LoadPreviews()
	m.resolver = &r
	cmds := []tea.Cmd{load, m.refreshPaths(msg.paths)}
	if r.AllResolved() && m.repo != nil {
		cmds = append(cmds, commitMergeCmd(m.repo, m.syncSvc))
	}
	return tea.Batch(cmds...)
}

// refreshPaths re-indexes paths, refreshes the tree and the trash, and
// reloads the open note if it is one of them.
func (m *Model) refreshPaths(paths []string) tea.Cmd {
	if len(paths) == 0 || m.opts.Vault == nil {
		return nil
	}
	m.queueReindex(paths...)
	return tea.Batch(reindexCmd(m.opts.Vault, m.ix, paths), loadTreeCmd(m.opts.Vault),
		goneCmd(m.opts.Vault, paths), loadTrashCmd(m.opts.Vault), m.reloadNoteIf(paths...))
}

// commitMergeCmd concludes the merge (spec §7): the files changed during the
// conflict are staged, the merge is committed as "Merge · <host>", and the
// syncer resumes (committing the .gitignore fix and pushing).
func commitMergeCmd(repo *gitsync.Repo, s *syncer.Syncer) tea.Cmd {
	return func() tea.Msg {
		resolveMu.Lock()
		defer resolveMu.Unlock()
		entries, err := repo.Status()
		if err != nil {
			return mergeCommittedMsg{err: err}
		}
		var changed []string
		for _, e := range entries {
			switch {
			case e.Kind == '?':
				changed = append(changed, e.Path)
			case (e.Kind == '1' || e.Kind == '2') && len(e.XY) == 2 && e.XY[1] != '.':
				changed = append(changed, e.Path)
			}
		}
		if err := repo.Add(changed...); err != nil {
			return mergeCommittedMsg{err: err}
		}
		if err := repo.CommitMerge("Merge · " + repo.Host); err != nil {
			return mergeCommittedMsg{err: err}
		}
		// Best effort; the syncer's next cycle commits the fix.
		_, _ = vault.EnsureGitignore(repo.Dir)
		if s != nil {
			s.ConflictResolved()
		}
		return mergeCommittedMsg{}
	}
}

// handleMergeCommitted closes the resolver once the merge is committed and
// makes the conflicted notes editable again.
func (m *Model) handleMergeCommitted(msg mergeCommittedMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not commit the merge: %v", msg.err))
	}
	paths := make([]string, 0, len(m.conflicted))
	for p := range m.conflicted {
		paths = append(paths, p)
	}
	m.resolver = nil
	m.conflictFiles = nil
	m.setConflicted(nil)
	return tea.Batch(m.pushToast(msgs.ToastInfo, textConflictsFix), m.refreshPaths(paths))
}
