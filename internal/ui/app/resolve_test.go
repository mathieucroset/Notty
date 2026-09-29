package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/ui/history"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/palette"
)

// conflictEnv makes the laptop's first sync conflict on README.md: the
// desktop pushed one edit, the laptop has another, uncommitted.
func conflictEnv(t *testing.T) (*gittest.Env, *Model, Options) {
	t.Helper()
	env := gittest.New(t)
	gittest.Write(t, env.Desktop, "README.md", "# Notes\n\ndesktop line\n")
	gittest.CommitAll(t, env.Desktop, "Update README.md · desktop")
	gittest.Push(t, env.Desktop)
	gittest.Write(t, env.Laptop, "README.md", "# Notes\n\nlaptop line\n")
	opts := syncOptions(t, env.Laptop, fastClock{})
	m := startSync(t, opts, msgs.SyncConflict)
	return env, m, opts
}

func TestConflictFlowResolvesAndCommitsMerge(t *testing.T) {
	env, m, _ := conflictEnv(t)
	if !m.isConflicted("README.md") {
		t.Fatalf("conflicted = %v", m.conflicted)
	}
	if s := screen(m); !strings.Contains(s, "Conflicts (1)") || !strings.Contains(s, "⚠ 1 conflict") {
		t.Errorf("sidebar or status bar missing the conflict:\n%s", s)
	}

	run(t, m, msgs.OpenNoteMsg{Path: "README.md", Line: 0})
	if m.editor.ModeName() != "READ-ONLY" {
		t.Errorf("conflicted note mode = %s", m.editor.ModeName())
	}
	if !strings.Contains(screen(m), "This note has a sync conflict. Press c to resolve.") {
		t.Errorf("banner missing:\n%s", screen(m))
	}

	// c in the read-only editor opens the resolver on the conflicted file.
	run(t, m, keyMsg("c"))
	if m.resolver == nil {
		t.Fatalf("resolver not open; toasts %v", toastTexts(m))
	}
	if s := screen(m); !strings.Contains(s, "README.md") || !strings.Contains(s, "Yours") {
		t.Errorf("resolver screen:\n%s", s)
	}

	run(t, m, keyMsg("o"))
	run(t, m, keyMsg("enter"))
	waitFor(t, m, func() bool { return m.resolver == nil })
	if !hasToast(m, msgs.ToastInfo, textConflictsFix) {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if log := gittest.Git(t, env.Laptop.Dir, "log", "--format=%s"); !strings.Contains(log, "Merge · laptop") {
		t.Errorf("no merge commit:\n%s", log)
	}
	if got := gittest.Git(t, env.Laptop.Dir, "show", "HEAD:README.md"); !strings.Contains(got, "laptop line") {
		t.Errorf("merged README.md = %q", got)
	}
	// The syncer was told: it leaves Conflict and pushes the merge.
	waitFor(t, m, func() bool { return m.sync.State == msgs.SyncSynced })
	if !strings.Contains(remoteLog(t, env.Remote), "Merge · laptop") {
		t.Errorf("merge not pushed:\n%s", remoteLog(t, env.Remote))
	}
	waitFor(t, m, func() bool { return m.editor.ModeName() != "READ-ONLY" })
	if strings.Contains(m.editor.Content(), "<<<<<<<") || !strings.Contains(m.editor.Content(), "laptop line") {
		t.Errorf("open note not reloaded: %q", m.editor.Content())
	}
}

func TestSyncNowInConflictOpensResolver(t *testing.T) {
	_, m, _ := conflictEnv(t)
	run(t, m, palette.SyncNowMsg{})
	if m.resolver == nil {
		t.Fatal("Sync now in Conflict did not open the resolver")
	}
	run(t, m, keyMsg("esc"))
	if m.resolver != nil {
		t.Error("esc did not close the resolver")
	}
	if !m.isConflicted("README.md") {
		t.Error("closing the resolver dropped the conflict")
	}
	// The sidebar's Conflicts entry reopens it.
	run(t, m, msgs.ActivateEntryMsg{Entry: msgs.EntryConflicts})
	if m.resolver == nil {
		t.Error("Conflicts entry did not open the resolver")
	}
}

func TestModifyDeleteKeepEdited(t *testing.T) {
	env := gittest.New(t)
	gittest.Write(t, env.Desktop, "README.md", "# Notes\n\nedited on desktop\n")
	gittest.CommitAll(t, env.Desktop, "Update README.md · desktop")
	gittest.Push(t, env.Desktop)
	if err := os.Remove(filepath.Join(env.Laptop.Dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	opts := syncOptions(t, env.Laptop, fastClock{})
	m := startSync(t, opts, msgs.SyncConflict)
	run(t, m, msgs.OpenResolverMsg{})
	if m.resolver == nil {
		t.Fatalf("resolver not open; toasts %v", toastTexts(m))
	}
	if !strings.Contains(screen(m), "Keep edited") {
		t.Fatalf("choice screen:\n%s", screen(m))
	}
	run(t, m, keyMsg("1"))
	run(t, m, keyMsg("enter"))
	waitFor(t, m, func() bool { return m.resolver == nil })
	if got := readFile(t, opts.Vault, "README.md"); !strings.Contains(got, "edited on desktop") {
		t.Errorf("README.md = %q", got)
	}
	waitFor(t, m, func() bool { return m.sync.State == msgs.SyncSynced })
}

func TestResolveTextRefusesInvalidTOML(t *testing.T) {
	if err := validateResult(".notty/settings.toml", []byte("theme = \n")); err == nil {
		t.Error("invalid TOML accepted")
	}
	if err := validateResult(".notty/settings.toml", []byte("theme = \"nord\"\n")); err != nil {
		t.Errorf("valid TOML refused: %v", err)
	}
	if err := validateResult("note.md", []byte("theme = \n")); err != nil {
		t.Errorf("markdown validated as TOML: %v", err)
	}
}

func TestWritePathsRefuseConflictedFiles(t *testing.T) {
	const want = "'ideas' has a sync conflict — press c to resolve"
	tests := []struct {
		name string
		msg  any
	}{
		{"rename", msgs.RequestRename{Path: "ideas.md"}},
		{"move", msgs.RequestMove{Path: "ideas.md"}},
		{"trash", msgs.RequestTrash{Path: "ideas.md"}},
		{"pin", msgs.TogglePinMsg{Path: "ideas.md"}},
		{"task toggle", msgs.ToggleTaskMsg{Path: "ideas.md", Line: 2, Text: "- [ ] x"}},
		{"history restore", history.RestoreVersionMsg{Path: "ideas.md", Rev: "abc1234", Content: "old"}},
		{"external editor", msgs.OpenFileExternalMsg{Path: "ideas.md"}},
		{"image import", msgs.ImportImageMsg{Data: []byte("png"), Ext: "png"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			m := openNote(t, opts, "ideas.md")
			m.setConflicted(map[string]bool{"ideas.md": true})
			before := readFile(t, opts.Vault, "ideas.md")
			run(t, m, tt.msg)
			if !hasToast(m, msgs.ToastWarn, want) {
				t.Errorf("toasts = %v", toastTexts(m))
			}
			if m.overlayOpen() {
				t.Error("a dialog opened for a conflicted file")
			}
			if len(m.opts.Pins.Pins) != 0 {
				t.Error("conflicted note pinned")
			}
			if got := readFile(t, opts.Vault, "ideas.md"); got != before {
				t.Errorf("conflicted file written: %q", got)
			}
		})
	}
}

func TestWritePathsRefuseFolderWithConflict(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	m.setConflicted(map[string]bool{"Work/Standup notes.md": true})
	run(t, m, msgs.RequestTrash{Path: "Work"})
	if !hasToast(m, msgs.ToastWarn, "'Work' has a sync conflict — press c to resolve") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if m.overlayOpen() {
		t.Error("trash dialog opened for a folder holding a conflict")
	}
}

func TestCtrlEOnConflictedNoteRefuses(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	m.setConflicted(map[string]bool{"ideas.md": true})
	run(t, m, keyMsg("ctrl+e"))
	if !hasToast(m, msgs.ToastWarn, "'ideas' has a sync conflict") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if m.host.ExternalEditing() {
		t.Error("the external editor was started")
	}
}
