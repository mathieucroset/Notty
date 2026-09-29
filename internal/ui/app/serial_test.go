package app

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestOrderedSaverSkipsOlderSnapshots(t *testing.T) {
	var s orderedSaver
	first, second := s.ticket(), s.ticket()
	var wrote []uint64
	write := func(n uint64) func() error {
		return func() error { wrote = append(wrote, n); return nil }
	}
	_ = s.save(second, write(second))
	_ = s.save(first, write(first))
	if !reflect.DeepEqual(wrote, []uint64{second}) {
		t.Errorf("writes = %v, want only the newer snapshot", wrote)
	}
}

func TestLocalStateSnapshotsLandInOrder(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	opts.Local.Touch("old.md")
	older := m.saveLocalCmd()
	opts.Local.Touch("new.md")
	newer := m.saveLocalCmd()
	newer()
	older() // finishes last, but is older
	saved, err := localstate.Load(opts.LocalPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.LastNote != "new.md" {
		t.Errorf("LastNote = %q, want the newer snapshot", saved.LastNote)
	}
}

func TestPinSnapshotsLandInOrder(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	opts.Pins.Toggle("ideas.md")
	older := m.savePinsCmd()
	opts.Pins.Toggle("ideas.md")
	opts.Pins.Toggle("Work/Standup notes.md")
	newer := m.savePinsCmd()
	newer()
	older()
	saved, err := meta.Load(opts.Vault.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Pins, []string{"Work/Standup notes.md"}) {
		t.Errorf("pins = %v, want the newer snapshot", saved.Pins)
	}
}

func TestConcurrentTaskTogglesAllLand(t *testing.T) {
	opts := testOptions(t)
	const n = 20
	var b strings.Builder
	b.WriteString("# Many\n\n")
	for i := range n {
		fmt.Fprintf(&b, "- [ ] task %d\n", i)
	}
	writeFile(t, opts.Vault, "many.md", b.String())
	m := start(t, opts, 120, 30)

	var wg sync.WaitGroup
	for i := range n {
		cmd := toggleTaskCmd(opts.Vault, nil, m.ix, msgs.ToggleTaskMsg{Path: "many.md", Line: i + 2, Text: fmt.Sprintf("- [ ] task %d", i)})
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd()
		}()
	}
	wg.Wait()
	if got := strings.Count(readFile(t, opts.Vault, "many.md"), "- [x]"); got != n {
		t.Errorf("%d of %d concurrent toggles landed", got, n)
	}
}
