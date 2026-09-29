package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/syncer"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/history"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/trash"
	"github.com/mathieucroset/notty/internal/vault"
)

// SyncFactory creates the syncer for the git vault at root, with the repo
// it runs on. It returns nils when the vault is not a git repository.
type SyncFactory func(root string, cfg config.Config, host syncer.Host) (*syncer.Syncer, *gitsync.Repo)

// DefaultSyncFactory opens the vault's repository and builds a syncer on the
// real clock.
func DefaultSyncFactory(root string, cfg config.Config, host syncer.Host) (*syncer.Syncer, *gitsync.Repo) {
	repo := gitsync.Open(root)
	if !repo.IsRepo() {
		return nil, nil
	}
	return syncer.New(repo, cfg, host, syncer.RealClock()), repo
}

// Texts, dialog IDs and limits of the sync wiring.
const (
	textGitMissing   = "git not found — sync and history are disabled"
	textDroppedKeys  = "%d keystrokes were dropped (sync conflict)"
	textQuitSyncing  = "syncing before exit…"
	textNoSyncSetup  = "Sync is not available: this vault is not a git repository"
	textSyncIsSetUp  = "Sync is already set up for this vault"
	textConflictsFix = "Conflicts resolved"
	restoreHint      = " · R to restore"
	dlgAuthFailed    = "auth-failed"
	quitSyncLimit    = 5 * time.Second
)

// syncUpdateMsg carries one syncer update.
type syncUpdateMsg struct{ u syncer.Update }

// restoreDeletedRemotelyMsg restores the note of the newest trash warning
// (the R key on its toast, or the palette).
type restoreDeletedRemotelyMsg struct{}

// listenSyncCmd waits for the next syncer update. It returns nil (ending the
// loop) once ctx is done. The syncer's channel is unbuffered, so the loop
// must be re-armed right after each update.
func listenSyncCmd(ctx context.Context, s *syncer.Syncer) tea.Cmd {
	if s == nil {
		return nil
	}
	updates := s.Updates()
	return func() tea.Msg {
		select {
		case u := <-updates:
			return syncUpdateMsg{u: u}
		case <-ctx.Done():
			return nil
		}
	}
}

// attachSyncer creates the syncer for the open vault, if the factory gives
// one.
func (m *Model) attachSyncer() {
	if m.opts.NewSyncer == nil || m.opts.Vault == nil || m.syncSvc != nil {
		return
	}
	s, repo := m.opts.NewSyncer(m.opts.Vault.Root, m.opts.Config, m.host)
	if s == nil || repo == nil {
		return
	}
	m.syncSvc, m.repo = s, repo
}

// startSyncCmds starts the syncer (after making sure .gitignore is right, so
// its first cycle commits the fix) and listens to it and to its host
// requests. Without git it shows the one-time notice instead.
func (m *Model) startSyncCmds() tea.Cmd {
	if m.syncSvc == nil {
		if m.opts.GitMissing && !m.gitNoticeShown {
			m.gitNoticeShown = true
			return emit(msgs.ToastMsg{Level: msgs.ToastWarn, Text: textGitMissing})
		}
		return nil
	}
	s, ctx, root := m.syncSvc, m.host.ctx, m.opts.Vault.Root
	start := func() tea.Msg {
		// Best effort: a failure shows up on the next save anyway.
		_, _ = vault.EnsureGitignore(root)
		s.Start(ctx)
		return nil
	}
	return tea.Batch(start, listenSyncCmd(ctx, s), listenHostCmd(m.host))
}

// syncStatusMsg maps a syncer status to the status bar's (spec §4.3).
func syncStatusMsg(st syncer.Status) msgs.SyncStatusMsg {
	out := msgs.SyncStatusMsg{Pending: st.Pending, Conflicts: st.Conflicts}
	switch st.State {
	case syncer.LocalOnly:
		out.State = msgs.SyncLocalOnly
	case syncer.Synced:
		out.State = msgs.SyncSynced
	case syncer.Offline:
		out.State = msgs.SyncOffline
	case syncer.Conflict:
		out.State = msgs.SyncConflict
	case syncer.Error:
		out.State = msgs.SyncError
	default: // Idle, Committing, Pulling, Merging, Pushing
		out.State = msgs.SyncSyncing
	}
	return out
}

// isAuthError reports whether st is an authentication failure.
func isAuthError(st syncer.Status) bool {
	return st.State == syncer.Error && (st.Detail == "auth" || errors.Is(st.Err, gitsync.ErrAuth))
}

// handleSyncUpdate applies one syncer update and keeps listening.
func (m *Model) handleSyncUpdate(u syncer.Update) tea.Cmd {
	cmds := []tea.Cmd{listenSyncCmd(m.host.ctx, m.syncSvc)}
	m.syncStatus = u.Status
	m.sync = syncStatusMsg(u.Status)
	switch {
	case u.Status.State == syncer.Conflict:
		if u.Conflicted != nil {
			m.setConflicted(pathSet(u.Conflicted))
		}
	case len(m.conflicted) > 0:
		m.setConflicted(nil)
	}
	if len(u.Reindex) > 0 {
		cmds = append(cmds, m.reindexMerged(u.Reindex))
	}
	if len(u.Reload) > 0 {
		cmds = append(cmds, m.reloadNoteIf(u.Reload...))
	}
	for _, w := range u.TrashWarnings {
		cmds = append(cmds, m.trashWarning(w))
	}
	if isAuthError(u.Status) {
		if !m.authShown {
			m.authShown = true
			cmds = append(cmds, m.showAuthDialog())
		}
	} else if u.Status.State != syncer.Error {
		m.authShown = false
	}
	return tea.Batch(cmds...)
}

// pathSet turns a path list into a set (nil for none).
func pathSet(paths []string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	return set
}

// setConflicted records the conflicted paths: the sidebar's Conflicts count
// and the open note's read-only state follow.
func (m *Model) setConflicted(set map[string]bool) {
	if len(set) == 0 {
		set = nil
	}
	m.conflicted = set
	m.updateCounts()
	m.syncReadOnly()
}

// syncReadOnly makes the open note read-only while it is conflicted, and
// editable again once it is not.
func (m *Model) syncReadOnly() {
	p := m.editor.Path()
	if p == "" {
		return
	}
	ro := m.isConflicted(p)
	if ro != (m.editor.ModeName() == "READ-ONLY") {
		m.editor = m.editor.SetReadOnly(ro, editor.DefaultBanner)
	}
}

// reindexMerged follows a merge: re-index the changed paths, refresh the
// tree, forget deleted paths and reload the pins when they changed.
func (m *Model) reindexMerged(paths []string) tea.Cmd {
	m.queueReindex(paths...)
	cmds := []tea.Cmd{
		reindexCmd(m.opts.Vault, m.ix, paths),
		loadTreeCmd(m.opts.Vault),
		goneCmd(m.opts.Vault, paths),
		loadTrashCmd(m.opts.Vault),
	}
	if slices.Contains(paths, pinsPath) && m.opts.Vault != nil {
		root := m.opts.Vault.Root
		cmds = append(cmds, func() tea.Msg {
			st, err := meta.Load(root)
			if err != nil {
				return nil
			}
			return pinsReloadedMsg{pins: st.Pins}
		})
	}
	return tea.Batch(cmds...)
}

// pinsPath is the vault file holding the pins.
const pinsPath = ".notty/state.json"

// pinsReloadedMsg carries the pins read back after a merge changed them.
type pinsReloadedMsg struct{ pins []string }

// trashWarning shows a "deleted on <host>, but you edited it here" toast
// with a Restore action (spec §7).
func (m *Model) trashWarning(w syncer.TrashWarning) tea.Cmd {
	host := w.Host
	if host == "" {
		host = "another computer"
	}
	text := fmt.Sprintf("'%s' was deleted on %s, but you edited it here", displayName(w.Path), host) + restoreHint
	m.trashWarnings = append(m.trashWarnings, w)
	m.trashWarnText = text
	return m.pushToast(msgs.ToastWarn, text)
}

// trashWarningNewest reports whether the newest toast shown is the latest
// trash warning, so R restores its note.
func (m *Model) trashWarningNewest() bool {
	if len(m.trashWarnings) == 0 {
		return false
	}
	text, ok := m.toast.Newest()
	return ok && text == m.trashWarnText
}

// restoreDeletedRemotely restores the note of the newest trash warning.
func (m *Model) restoreDeletedRemotely() tea.Cmd {
	if len(m.trashWarnings) == 0 || m.opts.Vault == nil {
		return m.pushToast(msgs.ToastInfo, "No note deleted on another computer to restore")
	}
	w := m.trashWarnings[len(m.trashWarnings)-1]
	m.trashWarnings = m.trashWarnings[:len(m.trashWarnings)-1]
	m.trashWarnText = ""
	v, ix := m.opts.Vault, m.ix
	id := trashID(w.TrashPath)
	return func() tea.Msg {
		items, err := v.TrashItems()
		if err != nil {
			return restoredMsg{err: err}
		}
		for _, it := range items {
			if it.ID == id {
				return restoreCmd(v, ix, it)()
			}
		}
		return restoredMsg{err: fmt.Errorf("'%s' is no longer in the trash", displayName(w.Path))}
	}
}

// trashID returns the trash item id of a path inside .trash/<id>/.
func trashID(p string) string {
	rest, ok := strings.CutPrefix(p, ".trash/")
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, "/")
	return id
}

// showAuthDialog explains an authentication failure (plan amendment A8).
func (m *Model) showAuthDialog() tea.Cmd {
	d := dialog.NewChoice(dlgAuthFailed, "Sync: authentication failed",
		"GitHub refused the credentials. Check them in a terminal with\n"+
			"  ssh -T git@github.com\n  gh auth status\n"+
			"Notty retries on the next save or \"Sync now\".",
		[]string{"OK"}, m.opts.Styles)
	m.openDialog(d, pendingOp{})
	return nil
}

// noteChanged tells the syncer files changed (a save, a watcher event, a
// file operation), restarting its commit timer.
func (m *Model) noteChanged(paths ...string) {
	if m.syncSvc == nil {
		return
	}
	for _, p := range paths {
		if p != "" {
			m.syncSvc.NoteChanged(p)
			return
		}
	}
	m.syncSvc.NoteChanged("")
}

// syncNow runs a sync cycle now (palette "Sync now"); in Conflict it opens
// the resolver instead (spec §7).
func (m *Model) syncNow() tea.Cmd {
	if m.syncSvc == nil {
		if m.opts.GitMissing {
			return m.pushToast(msgs.ToastInfo, textGitMissing)
		}
		return m.pushToast(msgs.ToastInfo, textNoSyncSetup)
	}
	if m.syncStatus.State == syncer.Conflict {
		m.syncSvc.SyncNow() // leaves Conflict if the merge was ended outside Notty
		return m.openResolver("")
	}
	m.syncSvc.SyncNow()
	return nil
}

// handleHostMsg answers a syncer host request.
func (m *Model) handleHostMsg(msg tea.Msg) tea.Cmd {
	cmds := []tea.Cmd{listenHostCmd(m.host)}
	switch msg := msg.(type) {
	case flushRequestMsg:
		msg.reply <- m.flushNow()
	case lockMutationsMsg:
		m.mutLocked = true
		m.editor = m.editor.Lock()
	case unlockMutationsMsg:
		cmds = append(cmds, m.unlockMutations(msg.conflicted))
	}
	return tea.Batch(cmds...)
}

// flushNow saves the open buffer synchronously for the syncer (Host.Flush).
// A read-only (conflicted) note is never written.
func (m *Model) flushNow() error {
	if m.note.path == "" || !m.editor.Dirty() || m.extConflict == m.editor.Path() {
		return nil
	}
	save := m.saveEditorCmd()
	if save == nil {
		return nil
	}
	res, _ := save().(savedMsg)
	m.later(m.applySaved(res))
	return res.err
}

// unlockMutations ends the merge window (spec §7): queued keys are replayed
// unless the open note is now conflicted, in which case they are dropped
// with a toast; queued app actions run in order.
func (m *Model) unlockMutations(conflicted map[string]bool) tea.Cmd {
	m.mutLocked = false
	if len(conflicted) > 0 {
		merged := make(map[string]bool, len(conflicted)+len(m.conflicted))
		for p := range m.conflicted {
			merged[p] = true
		}
		for p := range conflicted {
			merged[p] = true
		}
		m.setConflicted(merged)
	}
	replay := !m.isConflicted(m.editor.Path())
	var edCmd tea.Cmd
	var dropped int
	m.editor, edCmd, dropped = m.editor.Unlock(replay)
	cmds := []tea.Cmd{edCmd}
	if dropped > 0 {
		cmds = append(cmds, m.pushToast(msgs.ToastWarn, fmt.Sprintf(textDroppedKeys, dropped)))
	}
	if len(m.mutQueue) > 0 {
		queued := make([]tea.Cmd, len(m.mutQueue))
		for i, q := range m.mutQueue {
			queued[i] = emit(q)
		}
		m.mutQueue = nil
		cmds = append(cmds, tea.Sequence(queued...))
	}
	return tea.Batch(cmds...)
}

// isMutation reports whether msg changes files, so it must wait for the end
// of the locked merge section (spec §7: queued, not refused).
func isMutation(msg tea.Msg) bool {
	switch msg.(type) {
	case dialog.ResultMsg, runPendingMsg, msgs.SaveRequestMsg,
		msgs.TogglePinMsg, msgs.ToggleTaskMsg, msgs.ImportImageMsg, imageImportedMsg,
		trash.RestoreMsg, history.RestoreVersionMsg,
		msgs.OpenFileExternalMsg, restoreDeletedRemotelyMsg:
		return true
	}
	return false
}

// beginExec marks the terminal as handed to another program, so the syncer
// defers its cycles (spec §7).
func (m *Model) beginExec() { m.host.editing.Store(true) }

// endExec runs the cycle deferred while the program ran.
func (m *Model) endExec() {
	m.host.editing.Store(false)
	if m.syncSvc != nil {
		m.syncSvc.ExternalEditDone()
	}
}

// syncQuitCmd syncs before exit (plan amendment A7): the buffer is already
// saved, so the syncer skips its flush; it commits and pushes within 5s,
// and the app quits whatever the outcome.
func syncQuitCmd(s *syncer.Syncer) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), quitSyncLimit)
		defer cancel()
		_ = s.Quit(ctx, true)
		return nil
	}
}

// Shutdown releases what the app still holds once the program has exited:
// the syncer's host fails fast, listeners stop, and a lock taken after the
// wizard is released. main calls it after Program.Run returns.
func (m *Model) Shutdown() {
	m.host.shutdown()
	m.closeWatcher()
	if m.releaseLock != nil {
		m.releaseLock()
		m.releaseLock = nil
	}
}
