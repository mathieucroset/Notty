package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

func readFile(t *testing.T, v *vault.Vault, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(v.Root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(v *vault.Vault, rel string) bool {
	_, err := os.Stat(filepath.Join(v.Root, filepath.FromSlash(rel)))
	return err == nil
}

func toastTexts(m *Model) []string {
	var out []string
	for _, e := range m.toast.Log() {
		out = append(out, e.Text)
	}
	return out
}

func hasToast(m *Model, level msgs.ToastLevel, substr string) bool {
	for _, e := range m.toast.Log() {
		if e.Level == level && strings.Contains(e.Text, substr) {
			return true
		}
	}
	return false
}

// TestCreateFolderThenNoteFlow drives the sidebar in a real program: N
// creates a folder (which gets selected), n creates a note inside it, and
// the note opens.
func TestCreateFolderThenNoteFlow(t *testing.T) {
	opts := testOptions(t)
	tm := teatest.NewTestModel(t, New(opts), teatest.WithInitialTermSize(120, 30))
	waitScreen(t, tm, "N O T E S", "ideas")

	// Rows: Work, ideas.md. On ideas, N creates the folder at the root.
	tm.Send(keyMsg("j"))
	tm.Send(keyMsg("N"))
	waitScreen(t, tm, "New folder")
	for _, r := range "Projects" {
		tm.Send(keyMsg(string(r)))
	}
	tm.Send(keyMsg("enter"))
	waitScreen(t, tm, "▸ Projects")

	tm.Send(keyMsg("n"))
	waitScreen(t, tm, "New note in Projects")
	for _, r := range "Standup" {
		tm.Send(keyMsg(string(r)))
	}
	tm.Send(keyMsg("enter"))
	waitScreen(t, tm, "Projects / Standup")

	tm.Send(keyMsg("ctrl+q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	final := tm.FinalModel(t).(*Model)
	if final.NotePath() != "Projects/Standup.md" {
		t.Errorf("NotePath = %q, want Projects/Standup.md", final.NotePath())
	}
	if got := readFile(t, opts.Vault, "Projects/Standup.md"); got != "# Standup\n\n" {
		t.Errorf("new note content = %q", got)
	}
	if _, ok := final.ix.Get("Projects/Standup.md"); !ok {
		t.Error("new note not indexed")
	}
}

func TestNewFolderSelected(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.RequestNewFolder{Parent: "Work"})
	run(t, m, dialog.ResultMsg{ID: dlgNewFolder, OK: true, Value: "Clients"})
	if !exists(opts.Vault, "Work/Clients") {
		t.Fatal("folder not created")
	}
	// The new folder is selected: n now targets it.
	run(t, m, keyMsg("n"))
	if m.topOverlay() == nil || m.topOverlay().pending.path != "Work/Clients" {
		t.Errorf("n after N targets %+v, want Work/Clients", m.topOverlay())
	}
}

func TestRenameUpdatesEverything(t *testing.T) {
	opts := testOptions(t)
	opts.Pins = &meta.State{Pins: []string{"ideas.md"}}
	m := start(t, opts, 120, 30)
	run(t, m, msgs.OpenNoteMsg{Path: "ideas.md", Line: -1})

	run(t, m, msgs.RequestRename{Path: "ideas.md"})
	if m.topOverlay() == nil || !strings.Contains(m.topOverlay().dialog.View(), "ideas") {
		t.Fatalf("rename dialog not prefilled with the current name")
	}
	run(t, m, dialog.ResultMsg{ID: dlgRename, OK: true, Value: "Thoughts"})

	if exists(opts.Vault, "ideas.md") || !exists(opts.Vault, "Thoughts.md") {
		t.Fatal("file not renamed on disk")
	}
	if _, ok := m.ix.Get("Thoughts.md"); !ok {
		t.Error("index not renamed")
	}
	if _, ok := m.ix.Get("ideas.md"); ok {
		t.Error("old path still indexed")
	}
	if m.NotePath() != "Thoughts.md" {
		t.Errorf("open note path = %q", m.NotePath())
	}
	saved, _ := meta.Load(opts.Vault.Root)
	if !reflect.DeepEqual(saved.Pins, []string{"Thoughts.md"}) {
		t.Errorf("saved pins = %v", saved.Pins)
	}
	local, _ := localstate.Load(opts.LocalPath)
	if local.LastNote != "Thoughts.md" {
		t.Errorf("local LastNote = %q", local.LastNote)
	}
	if s := screen(m); !strings.Contains(s, "Thoughts") {
		t.Errorf("tree not refreshed:\n%s", s)
	}
}

func TestMoveWithFolderSuggestions(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.RequestMove{Path: "ideas.md"})
	run(t, m, keyMsg("W"))
	if !strings.Contains(m.topOverlay().dialog.View(), "Work") {
		t.Fatalf("folder suggestion missing:\n%s", m.topOverlay().dialog.View())
	}
	run(t, m, keyMsg("enter")) // takes the highlighted suggestion

	if !exists(opts.Vault, "Work/ideas.md") {
		t.Fatal("note not moved")
	}
	if _, ok := m.ix.Get("Work/ideas.md"); !ok {
		t.Error("index not updated after the move")
	}
	if m.overlayOpen() {
		t.Error("dialog still open")
	}
}

func TestMoveRejectsUnknownFolder(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.RequestMove{Path: "ideas.md"})
	typeText(t, m, "Nope")
	run(t, m, keyMsg("enter"))
	if !m.overlayOpen() {
		t.Fatal("move to an unknown folder was accepted")
	}
	if s := screen(m); !strings.Contains(s, "No such folder") {
		t.Errorf("validation error missing:\n%s", s)
	}
	if exists(opts.Vault, "Nope") || !exists(opts.Vault, "ideas.md") {
		t.Error("the note moved or a folder was created")
	}
}

func TestMoveToRootWithEmptyField(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.RequestMove{Path: "Work/Standup notes.md"})
	for range len("Work") {
		run(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	run(t, m, keyMsg("enter"))
	if !exists(opts.Vault, "Standup notes.md") {
		t.Errorf("note not moved to the root; toasts = %v", toastTexts(m))
	}
}

func TestMoveTargetsExcludeTheFolderItself(t *testing.T) {
	opts := testOptions(t)
	writeFile(t, opts.Vault, "Work/Sub/a.md", "# A\n")
	m := start(t, opts, 120, 30)
	writeFile(t, opts.Vault, "Other/b.md", "# B\n")
	run(t, m, treeLoadedMsg{root: mustTree(t, opts.Vault)})
	if got := m.moveTargets("Work"); !reflect.DeepEqual(got, []string{"Other"}) {
		t.Errorf("moveTargets(Work) = %v, want [Other]", got)
	}
}

func mustTree(t *testing.T, v *vault.Vault) *vault.Node {
	t.Helper()
	root, err := v.Tree()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestMoveLinkRewriteFailureIsAWarning(t *testing.T) {
	opts := testOptions(t)
	opts.Pins = &meta.State{Pins: []string{"ideas.md"}}
	m := start(t, opts, 120, 30)
	run(t, m, fileOpMsg{op: opMove, old: "ideas.md", path: "Work/ideas.md", err: errors.New("link rewrite failed")})
	if !hasToast(m, msgs.ToastWarn, "image links could not be updated") {
		t.Errorf("toasts = %v, want a link warning", toastTexts(m))
	}
	if !reflect.DeepEqual(opts.Pins.Pins, []string{"Work/ideas.md"}) {
		t.Errorf("pins = %v, want the move followed", opts.Pins.Pins)
	}
}

func TestTrashFlow(t *testing.T) {
	opts := testOptions(t)
	opts.Pins = &meta.State{Pins: []string{"ideas.md"}}
	m := start(t, opts, 120, 30)
	run(t, m, msgs.OpenNoteMsg{Path: "ideas.md", Line: -1})

	run(t, m, msgs.RequestTrash{Path: "ideas.md"})
	if s := screen(m); !strings.Contains(s, "Move 'ideas' to trash?") {
		t.Fatalf("confirmation missing:\n%s", s)
	}
	// Danger confirmations default to "no": enter cancels.
	run(t, m, keyMsg("enter"))
	if !exists(opts.Vault, "ideas.md") {
		t.Fatal("enter on the default button trashed the note")
	}
	run(t, m, msgs.RequestTrash{Path: "ideas.md"})
	run(t, m, keyMsg("y"))

	if exists(opts.Vault, "ideas.md") {
		t.Fatal("note still on disk")
	}
	items, err := opts.Vault.TrashItems()
	if err != nil || len(items) != 1 || items[0].OriginalPath != "ideas.md" {
		t.Errorf("trash = %v, %v", items, err)
	}
	if _, ok := m.ix.Get("ideas.md"); ok {
		t.Error("trashed note still indexed")
	}
	if len(opts.Pins.Pins) != 0 {
		t.Errorf("pins = %v, want the trashed pin removed", opts.Pins.Pins)
	}
	if m.NotePath() != "" {
		t.Errorf("trashed note still open: %q", m.NotePath())
	}
	if !hasToast(m, msgs.ToastInfo, "Moved 'ideas' to trash") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}

func TestVaultErrorsToastFriendly(t *testing.T) {
	opts := testOptions(t)
	writeFile(t, opts.Vault, "Thoughts.md", "# Thoughts\n")
	m := start(t, opts, 120, 30)
	run(t, m, msgs.RequestRename{Path: "ideas.md"})
	run(t, m, dialog.ResultMsg{ID: dlgRename, OK: true, Value: "Thoughts"})
	if !hasToast(m, msgs.ToastError, "Could not rename: a note or folder with that name already exists") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
	if s := screen(m); !strings.Contains(s, "already exists") {
		t.Errorf("error toast not drawn:\n%s", s)
	}
	if !exists(opts.Vault, "ideas.md") {
		t.Error("failed rename touched the file")
	}
}

func TestEditorCommand(t *testing.T) {
	opts := testOptions(t)
	opts.Config.Editor = "code -w"
	m := start(t, opts, 120, 30)
	cmd := m.editorCommand("/v/a.png")
	if !reflect.DeepEqual(cmd.Args, []string{"code", "-w", "/v/a.png"}) {
		t.Errorf("editor args = %v", cmd.Args)
	}
	if _, c := m.Update(msgs.OpenFileExternalMsg{Path: "a.png"}); c == nil {
		t.Error("OpenFileExternalMsg returned no command")
	}
}
