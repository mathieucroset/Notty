package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// writeFile writes content at the vault-relative path rel.
func writeFile(t *testing.T, v *vault.Vault, rel, content string) {
	t.Helper()
	abs := filepath.Join(v.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// taggedOptions is testOptions with a #work tag on the standup note.
func taggedOptions(t *testing.T) Options {
	t.Helper()
	opts := testOptions(t)
	writeFile(t, opts.Vault, "Work/Standup notes.md", "# Standup notes #work\n\n- shipped auth flow\n")
	writeFile(t, opts.Vault, "Work/Retro.md", "# Retro\n\nnothing tagged\n")
	return opts
}

func TestIndexBuiltAtStartup(t *testing.T) {
	m := New(taggedOptions(t))
	run(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	cmd := m.Init()
	if !strings.Contains(screen(m), "indexing…") {
		t.Errorf("status bar does not show indexing:\n%s", screen(m))
	}
	for _, msg := range execCmd(t, m, cmd) {
		run(t, m, msg)
	}
	s := screen(m)
	if strings.Contains(s, "indexing…") {
		t.Errorf("indexing still shown after the build:\n%s", s)
	}
	if !strings.Contains(s, "T A G S") || !strings.Contains(s, "#work") {
		t.Errorf("tag chips missing:\n%s", s)
	}
	if m.ix == nil || m.ix.Len() != 3 {
		t.Fatalf("index not installed: %v", m.ix)
	}
}

// TestChangesDuringIndexBuildReplayed changes files after the startup
// build has read the vault but before its result lands: the changes are
// queued and re-read once the index is installed.
func TestChangesDuringIndexBuildReplayed(t *testing.T) {
	opts := testOptions(t)
	m := New(opts)
	run(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	startup := execCmd(t, m, m.Init()) // the build has read the vault

	writeFile(t, opts.Vault, "external.md", "# External\n")
	run(t, m, watchEventMsg{paths: []string{"external.md"}})
	writeFile(t, opts.Vault, "created.md", "# Created\n")
	run(t, m, fileOpMsg{op: opNewNote, path: "created.md"})

	for _, msg := range startup {
		run(t, m, msg)
	}
	for _, p := range []string{"external.md", "created.md"} {
		if _, ok := m.ix.Get(p); !ok {
			t.Errorf("%s changed during the build is not indexed", p)
		}
	}
	if len(m.pendingIndex) != 0 {
		t.Errorf("queue not drained: %v", m.pendingIndex)
	}
}

func TestIndexProblemsToast(t *testing.T) {
	opts := testOptions(t)
	writeFile(t, opts.Vault, "bad.md", "# Bad\n\xff\xfe\n")
	m := start(t, opts, 120, 30)
	s := screen(m)
	if !strings.Contains(s, "1 note has a problem") {
		t.Errorf("problem toast missing:\n%s", s)
	}
}

func TestTagFilter(t *testing.T) {
	opts := taggedOptions(t)
	opts.Local.Expanded = []string{"Work"}
	m := start(t, opts, 120, 30)
	run(t, m, msgs.FilterTagMsg{Tag: "work"})
	s := screen(m)
	if !strings.Contains(s, "Standup notes") {
		t.Errorf("tagged note hidden by the filter:\n%s", s)
	}
	for _, hidden := range []string{"Retro", "ideas"} {
		if strings.Contains(s, hidden) {
			t.Errorf("untagged %q shown under the filter:\n%s", hidden, s)
		}
	}
	run(t, m, msgs.ClearFilterMsg{})
	if s := screen(m); !strings.Contains(s, "Retro") || !strings.Contains(s, "ideas") {
		t.Errorf("clearing the filter did not restore the tree:\n%s", s)
	}
}

func TestPinTogglePersists(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.TogglePinMsg{Path: "ideas.md"})
	if !strings.Contains(screen(m), "P I N N E D") {
		t.Errorf("PINNED missing after pinning:\n%s", screen(m))
	}
	saved, err := meta.Load(opts.Vault.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Pins, []string{"ideas.md"}) {
		t.Errorf("saved pins = %v", saved.Pins)
	}
	run(t, m, msgs.TogglePinMsg{Path: "ideas.md"})
	if strings.Contains(screen(m), "P I N N E D") {
		t.Errorf("PINNED still shown after unpinning:\n%s", screen(m))
	}
	if saved, _ = meta.Load(opts.Vault.Root); len(saved.Pins) != 0 {
		t.Errorf("saved pins after unpin = %v", saved.Pins)
	}
}

// TestPinAndFilterFlow drives the sidebar keys in a real program: p pins
// the selected note, # opens the tag picker and enter filters by the tag.
func TestPinAndFilterFlow(t *testing.T) {
	opts := taggedOptions(t)
	tm := newProgram(t, opts)
	waitScreen(t, tm, "#work")

	// Rows: Work, ideas.md. Pin ideas.
	tm.Send(keyMsg("j"))
	tm.Send(keyMsg("p"))
	waitScreen(t, tm, "P I N N E D")

	tm.Send(keyMsg("#"))
	for _, r := range "work" {
		tm.Send(keyMsg(string(r)))
	}
	tm.Send(keyMsg("enter"))
	waitScreen(t, tm, "filtered")

	tm.Send(keyMsg("ctrl+q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	final := finalModel(t, tm)
	if final.filterTag != "work" {
		t.Errorf("filterTag = %q, want work", final.filterTag)
	}
	saved, err := meta.Load(opts.Vault.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Pins, []string{"ideas.md"}) {
		t.Errorf("saved pins = %v", saved.Pins)
	}
}

// TestRebuildRequestsDoNotPileUp: lost events reported while a rebuild
// runs ask for one more rebuild once it lands, not a full build each.
func TestRebuildRequestsDoNotPileUp(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	first := m.rebuildIndex()
	if first == nil {
		t.Fatal("no rebuild started")
	}
	for range 3 {
		if m.rebuildIndex() != nil {
			t.Fatal("a second full build started while one runs")
		}
	}
	run(t, m, first())
	if m.indexGen != 2 {
		t.Errorf("builds after the first = %d, want 1 more", m.indexGen-1)
	}
	if m.indexing || m.rebuildAgain {
		t.Errorf("indexing %v, rebuild again %v after the builds landed", m.indexing, m.rebuildAgain)
	}
}

// TestRebuildDuringStartupKeepsStartupIndex: a rebuild asked for while the
// startup build runs waits for it, so a failed rebuild never leaves the app
// without an index.
func TestRebuildDuringStartupKeepsStartupIndex(t *testing.T) {
	opts := testOptions(t)
	m := New(opts)
	run(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	startup := execCmd(t, m, m.Init())
	if cmd := m.rebuildIndex(); cmd != nil {
		t.Fatal("rebuild started while the startup build runs")
	}
	var built indexBuiltMsg
	for _, msg := range startup {
		if b, ok := msg.(indexBuiltMsg); ok {
			built = b
		}
	}
	if built.ix == nil {
		t.Fatal("startup build produced no index")
	}
	next := m.handleIndexBuilt(built)
	if m.ix != built.ix {
		t.Fatal("startup index dropped")
	}
	if next == nil || !m.indexing {
		t.Fatal("the asked-for rebuild did not start after the startup build")
	}
	// The rebuild fails: the startup index stays.
	m.handleIndexBuilt(indexBuiltMsg{err: os.ErrPermission, gen: m.indexGen})
	if m.ix != built.ix || m.indexing {
		t.Errorf("after a failed rebuild: index kept %v, indexing %v", m.ix == built.ix, m.indexing)
	}
}

// TestRebuildDefersGoneNotesDuringMerge: notes missing from a rebuilt index
// are forgotten only once a sync merge stops rewriting the vault.
func TestRebuildDefersGoneNotesDuringMerge(t *testing.T) {
	opts := testOptions(t)
	opts.Pins = &meta.State{Pins: []string{"ideas.md"}}
	m := start(t, opts, 120, 30)
	if err := os.Remove(filepath.Join(opts.Vault.Root, "ideas.md")); err != nil {
		t.Fatal(err)
	}
	run(t, m, hostMsg{msg: lockMutationsMsg{}})
	run(t, m, m.rebuildIndex()())
	if !reflect.DeepEqual(opts.Pins.Pins, []string{"ideas.md"}) {
		t.Fatalf("pins changed during the merge: %v", opts.Pins.Pins)
	}
	run(t, m, hostMsg{msg: unlockMutationsMsg{}})
	if len(opts.Pins.Pins) != 0 {
		t.Errorf("pins = %v after the merge, want the deleted note's pin gone", opts.Pins.Pins)
	}
}
