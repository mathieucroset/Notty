package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/syncer"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/palette"
	"github.com/mathieucroset/notty/internal/vault"
)

// fastClock runs the syncer's short timers (the commit delay) after 20ms
// and never fires the long ones (the fetch interval, offline backoff).
type fastClock struct{ commit time.Duration }

func (fastClock) Now() time.Time { return time.Now() }

func (c fastClock) AfterFunc(d time.Duration, f func()) syncer.Timer {
	if d > 10*time.Second || c.commit < 0 {
		return time.AfterFunc(24*time.Hour, f)
	}
	return time.AfterFunc(20*time.Millisecond, f)
}

// syncOptions returns options for the vault in repo with a syncer on clock.
func syncOptions(t *testing.T, repo *gitsync.Repo, clock syncer.Clock) Options {
	t.Helper()
	opts := testOptions(t)
	v, err := vault.Open(repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	opts.Vault = v
	opts.NewSyncer = func(_ string, cfg config.Config, host syncer.Host) (*syncer.Syncer, *gitsync.Repo) {
		return syncer.New(repo, cfg, host, clock), repo
	}
	return opts
}

// startSync starts the app on opts and waits for the first sync to settle.
func startSync(t *testing.T, opts Options, want msgs.SyncState) *Model {
	t.Helper()
	m := start(t, opts, 120, 30)
	t.Cleanup(m.Shutdown)
	waitFor(t, m, func() bool { return m.sync.State == want })
	return m
}

// remoteLog returns the subjects of the remote's main branch.
func remoteLog(t *testing.T, remote string) string {
	t.Helper()
	return gittest.Git(t, remote, "log", "--format=%s", "main")
}

func TestSyncStatusMapping(t *testing.T) {
	tests := []struct {
		st   syncer.Status
		want msgs.SyncStatusMsg
	}{
		{syncer.Status{State: syncer.LocalOnly}, msgs.SyncStatusMsg{State: msgs.SyncLocalOnly}},
		{syncer.Status{State: syncer.Idle}, msgs.SyncStatusMsg{State: msgs.SyncSyncing}},
		{syncer.Status{State: syncer.Committing}, msgs.SyncStatusMsg{State: msgs.SyncSyncing}},
		{syncer.Status{State: syncer.Pulling}, msgs.SyncStatusMsg{State: msgs.SyncSyncing}},
		{syncer.Status{State: syncer.Merging}, msgs.SyncStatusMsg{State: msgs.SyncSyncing}},
		{syncer.Status{State: syncer.Pushing}, msgs.SyncStatusMsg{State: msgs.SyncSyncing}},
		{syncer.Status{State: syncer.Synced}, msgs.SyncStatusMsg{State: msgs.SyncSynced}},
		{syncer.Status{State: syncer.Offline, Pending: 3}, msgs.SyncStatusMsg{State: msgs.SyncOffline, Pending: 3}},
		{syncer.Status{State: syncer.Conflict, Conflicts: 2}, msgs.SyncStatusMsg{State: msgs.SyncConflict, Conflicts: 2}},
		{syncer.Status{State: syncer.Error, Err: errors.New("x")}, msgs.SyncStatusMsg{State: msgs.SyncError}},
	}
	for _, tt := range tests {
		if got := syncStatusMsg(tt.st); got != tt.want {
			t.Errorf("%v: got %+v, want %+v", tt.st.State, got, tt.want)
		}
	}
}

func TestSyncSaveCommitsAndPushes(t *testing.T) {
	env := gittest.New(t)
	gittest.Write(t, env.Laptop, "ideas.md", "# Ideas\n\nfirst\n")
	gittest.CommitAll(t, env.Laptop, "Add ideas · laptop")
	gittest.Push(t, env.Laptop)
	opts := syncOptions(t, env.Laptop, fastClock{})
	m := startSync(t, opts, msgs.SyncSynced)
	if !strings.Contains(screen(m), "✓ synced") {
		t.Errorf("status bar does not show synced:\n%s", screen(m))
	}

	run(t, m, msgs.OpenNoteMsg{Path: "ideas.md", Line: 2})
	insertText(t, m, "typed ")
	run(t, m, keyMsg("ctrl+s"))
	waitFor(t, m, func() bool { return strings.Contains(remoteLog(t, env.Remote), "Update ideas.md") })
	waitFor(t, m, func() bool { return m.sync.State == msgs.SyncSynced })
	if got := gittest.Git(t, env.Remote, "show", "main:ideas.md"); !strings.Contains(got, "typed first") {
		t.Errorf("remote ideas.md = %q", got)
	}
	// .gitignore was fixed at startup and committed by the first cycle.
	if got := gittest.Git(t, env.Remote, "show", "main:.gitignore"); !strings.Contains(got, ".notty/lock") {
		t.Errorf("remote .gitignore = %q", got)
	}
}

func TestSyncFlushSavesDirtyBufferBeforeCommit(t *testing.T) {
	env := gittest.New(t)
	opts := syncOptions(t, env.Laptop, fastClock{})
	m := startSync(t, opts, msgs.SyncSynced)
	run(t, m, msgs.OpenNoteMsg{Path: "README.md", Line: 0})
	insertText(t, m, "unsaved ")
	if !m.editor.Dirty() {
		t.Fatal("buffer should be dirty")
	}
	run(t, m, palette.SyncNowMsg{})
	waitFor(t, m, func() bool { return strings.Contains(remoteLog(t, env.Remote), "Update README.md") })
	if got := gittest.Git(t, env.Remote, "show", "main:README.md"); got != "unsaved # Notes" {
		t.Errorf("remote README.md = %q", got)
	}
	if m.editor.Dirty() {
		t.Error("flushed buffer still dirty")
	}
}

func TestSyncRemoteChangeReloadsOpenCleanNote(t *testing.T) {
	env := gittest.New(t)
	opts := syncOptions(t, env.Laptop, fastClock{})
	m := startSync(t, opts, msgs.SyncSynced)
	run(t, m, msgs.OpenNoteMsg{Path: "README.md", Line: 0})

	gittest.Sync(t, env.Desktop) // takes the laptop's .gitignore fix
	gittest.Write(t, env.Desktop, "README.md", "# Notes\n\nfrom desktop\n")
	gittest.Write(t, env.Desktop, "new.md", "# New #fromdesk\n")
	gittest.CommitAll(t, env.Desktop, "Update README.md · desktop")
	gittest.Push(t, env.Desktop)

	run(t, m, palette.SyncNowMsg{})
	waitFor(t, m, func() bool { return strings.Contains(m.editor.Content(), "from desktop") })
	waitFor(t, m, func() bool {
		n, ok := m.ix.Get("new.md")
		return ok && strings.Contains(n.Content, "#fromdesk")
	})
	if m.editor.Dirty() {
		t.Error("reloaded note is dirty")
	}
}

func TestSyncOfflineStatus(t *testing.T) {
	env := gittest.New(t)
	env.MakeRemoteUnreachable(t)
	opts := syncOptions(t, env.Laptop, fastClock{})
	m := startSync(t, opts, msgs.SyncOffline)
	if !strings.Contains(screen(m), "⊘ offline (") {
		t.Errorf("status bar:\n%s", screen(m))
	}
}

func TestQuitSyncsBeforeExit(t *testing.T) {
	env := gittest.New(t)
	// The commit timer never fires: only the quit sequence commits.
	opts := syncOptions(t, env.Laptop, fastClock{commit: -1})
	m := startSync(t, opts, msgs.SyncSynced)
	run(t, m, msgs.OpenNoteMsg{Path: "README.md", Line: 0})
	insertText(t, m, "bye ")
	out := run(t, m, keyMsg("ctrl+q"))
	if !hasQuit(out) {
		t.Fatal("did not quit")
	}
	if m.quitStatus != textQuitSyncing {
		t.Errorf("quit status = %q", m.quitStatus)
	}
	if got := gittest.Git(t, env.Remote, "show", "main:README.md"); got != "bye # Notes" {
		t.Errorf("remote README.md after quit = %q", got)
	}
}

func TestGitMissingNotice(t *testing.T) {
	opts := testOptions(t)
	opts.GitMissing = true
	m := start(t, opts, 120, 30)
	if !hasToast(m, msgs.ToastWarn, textGitMissing) {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}

func TestFlushRequestSavesBuffer(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "flush ")
	reply := make(chan error, 1)
	run(t, m, hostMsg{msg: flushRequestMsg{reply: reply}})
	if err := <-reply; err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, opts.Vault, "ideas.md"); !strings.HasPrefix(got, "flush # Ideas") {
		t.Errorf("file after flush = %q", got)
	}
	if m.editor.Dirty() {
		t.Error("buffer still dirty after flush")
	}
}

func TestFlushFailsFastAfterShutdown(t *testing.T) {
	m := New(testOptions(t))
	m.host.flushTimeout = 50 * time.Millisecond
	if err := m.host.Flush(); !errors.Is(err, errFlushTimeout) {
		t.Errorf("unanswered Flush = %v, want timeout", err)
	}
	m.Shutdown()
	begin := time.Now()
	if err := m.host.Flush(); !errors.Is(err, errProgramDone) {
		t.Errorf("Flush after shutdown = %v", err)
	}
	if d := time.Since(begin); d > 20*time.Millisecond {
		t.Errorf("Flush after shutdown took %v", d)
	}
}

func TestMergeWindowReplaysKeys(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	run(t, m, hostMsg{msg: lockMutationsMsg{}})
	insertText(t, m, "later ")
	run(t, m, msgs.TogglePinMsg{Path: "ideas.md"})
	if strings.Contains(m.editor.Content(), "later") {
		t.Fatal("keys applied while locked")
	}
	if len(m.opts.Pins.Pins) != 0 {
		t.Fatal("pin toggled while locked")
	}
	run(t, m, hostMsg{msg: unlockMutationsMsg{}})
	if !strings.Contains(m.editor.Content(), "later ") {
		t.Errorf("keys not replayed: %q", m.editor.Content())
	}
	if len(m.opts.Pins.Pins) != 1 {
		t.Errorf("queued pin toggle not run: %v", m.opts.Pins.Pins)
	}
}

func TestMergeWindowDropsKeysOnConflictedNote(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	run(t, m, hostMsg{msg: lockMutationsMsg{}})
	pressKeys(t, m, "i", "a", "b")
	run(t, m, hostMsg{msg: unlockMutationsMsg{conflicted: map[string]bool{"ideas.md": true}}})
	if m.editor.ModeName() != "READ-ONLY" {
		t.Errorf("conflicted open note mode = %s", m.editor.ModeName())
	}
	if strings.Contains(m.editor.Content(), "ab") {
		t.Error("keys replayed onto a conflicted note")
	}
	if !hasToast(m, msgs.ToastWarn, "3 keystrokes were dropped (sync conflict)") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if !strings.Contains(screen(m), "This note has a sync conflict. Press c to resolve.") {
		t.Errorf("banner missing:\n%s", screen(m))
	}
}

func TestAuthErrorShowsDialog(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	st := syncer.Status{State: syncer.Error, Detail: "auth", Err: fmt.Errorf("push: %w", gitsync.ErrAuth)}
	run(t, m, syncUpdateMsg{u: syncer.Update{Status: st}})
	o := m.topOverlay()
	if o == nil || o.kind != overlayDialog || o.dialog.ID() != dlgAuthFailed {
		t.Fatalf("no auth dialog: %+v", o)
	}
	for _, want := range []string{"ssh -T git@github.com", "gh auth status"} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("dialog missing %q:\n%s", want, screen(m))
		}
	}
	if !strings.Contains(screen(m), "✗ sync error") {
		t.Errorf("status bar:\n%s", screen(m))
	}
}

func TestTrashWarningRestore(t *testing.T) {
	for _, via := range []string{"R", "R on the emptied pane", "palette"} {
		t.Run(via, func(t *testing.T) {
			opts := testOptions(t)
			m := start(t, opts, 120, 30)
			if via == "R on the emptied pane" {
				run(t, m, msgs.OpenNoteMsg{Path: "ideas.md", Line: 0})
			}
			it, err := opts.Vault.Trash("ideas.md")
			if err != nil {
				t.Fatal(err)
			}
			if via == "R on the emptied pane" {
				// The merge removed the open note: the main pane is empty.
				run(t, m, pathsGoneMsg{paths: []string{"ideas.md"}})
				if m.NotePath() != "" || m.Focus() != FocusMain {
					t.Fatalf("note %q, focus %v", m.NotePath(), m.Focus())
				}
			}
			w := syncer.TrashWarning{Path: "ideas.md", TrashPath: ".trash/" + it.ID + "/ideas.md", Host: "desktop"}
			run(t, m, syncUpdateMsg{u: syncer.Update{Status: syncer.Status{State: syncer.Synced}, TrashWarnings: []syncer.TrashWarning{w}}})
			if !hasToast(m, msgs.ToastWarn, "'ideas' was deleted on desktop, but you edited it here") {
				t.Fatalf("toasts = %v", toastTexts(m))
			}
			switch via {
			case "R":
				m.focusSidebar()
				run(t, m, keyMsg("R"))
			case "R on the emptied pane":
				run(t, m, keyMsg("R"))
			default:
				run(t, m, restoreDeletedRemotelyMsg{})
			}
			if _, err := os.Stat(filepath.Join(opts.Vault.Root, "ideas.md")); err != nil {
				t.Errorf("note not restored: %v (toasts %v)", err, toastTexts(m))
			}
		})
	}
}

func TestPaletteListsRestoreOnlyAfterWarning(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	listed := func() bool {
		run(t, m, msgs.OpenPaletteMsg{})
		defer m.closeOverlayKind(overlayPalette)
		for _, r := range "restore last" {
			run(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		return strings.Contains(screen(m), "Restore last deleted")
	}
	if listed() {
		t.Error("restore listed without a warning")
	}
	run(t, m, syncUpdateMsg{u: syncer.Update{TrashWarnings: []syncer.TrashWarning{{Path: "x.md", TrashPath: ".trash/1/x.md"}}}})
	if !listed() {
		t.Errorf("restore not listed after a warning")
	}
}
