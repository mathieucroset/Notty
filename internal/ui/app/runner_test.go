package app

import (
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/watcher"
)

// The synchronous command loop used by the tests.
//
// Commands run in the test goroutine, one at a time; batches and sequences
// are flattened. Two kinds of commands are recognized by their function
// and run in the background instead:
//
//   - tea.Tick timers. When the loop runs out of messages it waits up to
//     tickWindow for the pending ticks: short ones (the preview's 150ms
//     debounce, the finder's 50ms debounce, the 100ms Kitty ready tick)
//     fire and are processed; longer ones (autosave, toast expiry) are
//     dropped, so tests drive them explicitly.
//   - watcher listeners (listenWatcherCmd, listenThemesCmd, ...). They
//     stay pending across run calls until the watcher reports something;
//     waitFor delivers them.
//
// Any other command still running after cmdLimit fails the test.
//
// waitFor gives up after waitLimit. It returns as soon as its condition
// holds, so the limit only matters for a test about to fail; it is
// generous because what it waits on may be real git work in the
// background (a first sync with a merge), which a busy Windows runner has
// taken more than 5s to finish.
const (
	tickWindow = 200 * time.Millisecond
	cmdLimit   = 30 * time.Second // generous for real git on slow CI runners
	waitLimit  = 30 * time.Second
)

// bgCmd is a command running in the background.
type bgCmd struct {
	ch   chan tea.Msg
	tick bool
}

var (
	bgMu      sync.Mutex
	bgPending = map[*Model][]*bgCmd{}
)

// funcName is the name of the function behind cmd.
func funcName(cmd tea.Cmd) string {
	if f := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()); f != nil {
		return f.Name()
	}
	return ""
}

func isTick(name string) bool {
	return strings.Contains(name, "bubbletea/v2.Tick.func") || strings.Contains(name, "bubbletea/v2.Every.func")
}

func isListener(name string) bool {
	return strings.Contains(name, "listenWatcherCmd.func") || strings.Contains(name, "listenSyncCmd.func") ||
		strings.Contains(name, "listenHostCmd.func") || strings.Contains(name, "listenThemesCmd.func")
}

func startBackground(m *Model, cmd tea.Cmd, tick bool) {
	b := &bgCmd{ch: make(chan tea.Msg, 1), tick: tick}
	go func() { b.ch <- cmd() }()
	bgMu.Lock()
	bgPending[m] = append(bgPending[m], b)
	bgMu.Unlock()
}

// execOne runs cmd for m and returns the messages it produced right away.
func execOne(t *testing.T, m *Model, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	name := funcName(cmd)
	switch {
	case isTick(name):
		startBackground(m, cmd, true)
		return nil
	case isListener(name):
		startBackground(m, cmd, false)
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var res tea.Msg
	select {
	case res = <-done:
	case <-time.After(cmdLimit):
		t.Fatalf("command %s still running after %v", name, cmdLimit)
	}
	switch r := res.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range r {
			out = append(out, execOne(t, m, c)...)
		}
		return out
	}
	// tea.Sequence returns an unexported []tea.Cmd type.
	if v := reflect.ValueOf(res); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		var out []tea.Msg
		for i := range v.Len() {
			out = append(out, execOne(t, m, v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	return []tea.Msg{res}
}

// collect gathers finished background commands. With waitTicks it waits
// up to tickWindow for the pending ticks and drops those that do not fire;
// watcher listeners are only polled and stay pending.
func collect(m *Model, waitTicks bool) []tea.Msg {
	bgMu.Lock()
	pending := bgPending[m]
	bgPending[m] = nil
	bgMu.Unlock()
	deadline := time.Now().Add(tickWindow)
	var out []tea.Msg
	var still []*bgCmd
	for _, b := range pending {
		if b.tick && waitTicks {
			select {
			case msg := <-b.ch:
				out = append(out, msg)
			case <-time.After(time.Until(deadline)):
				// A long timer: dropped.
			}
			continue
		}
		select {
		case msg := <-b.ch:
			out = append(out, msg)
		default:
			still = append(still, b)
		}
	}
	bgMu.Lock()
	bgPending[m] = append(still, bgPending[m]...)
	bgMu.Unlock()
	var msgs []tea.Msg
	for _, msg := range out {
		if msg != nil {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

// hasPendingTicks reports whether m has ticks waiting.
func hasPendingTicks(m *Model) bool {
	bgMu.Lock()
	defer bgMu.Unlock()
	for _, b := range bgPending[m] {
		if b.tick {
			return true
		}
	}
	return false
}

// run feeds msg to m and executes every resulting command, feeding their
// messages back in, until nothing is left. It returns every message the
// commands produced (including tea.QuitMsg, which stops processing).
func run(t *testing.T, m *Model, msg tea.Msg) []tea.Msg {
	t.Helper()
	return drive(t, m, []tea.Msg{msg})
}

// drive processes queue like run.
func drive(t *testing.T, m *Model, queue []tea.Msg) []tea.Msg {
	t.Helper()
	var produced []tea.Msg
	for steps := 0; ; steps++ {
		if len(queue) == 0 {
			queue = collect(m, hasPendingTicks(m))
			produced = append(produced, queue...)
			if len(queue) == 0 {
				return produced
			}
		}
		if steps > 200 {
			t.Fatal("too many message steps")
		}
		cur := queue[0]
		queue = queue[1:]
		if _, quit := cur.(tea.QuitMsg); quit {
			return produced
		}
		_, cmd := m.Update(cur)
		for _, res := range execOne(t, m, cmd) {
			produced = append(produced, res)
			if _, quit := res.(tea.QuitMsg); quit {
				return produced
			}
			queue = append(queue, res)
		}
	}
}

// execCmd runs cmd for m without feeding the results back, and returns
// them (short ticks included).
func execCmd(t *testing.T, m *Model, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	out := execOne(t, m, cmd)
	return append(out, collect(m, true)...)
}

// waitFor processes background messages (watcher events) until cond holds,
// failing after waitLimit.
func waitFor(t *testing.T, m *Model, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met after %v", waitLimit)
		}
		if msgs := collect(m, false); len(msgs) > 0 {
			drive(t, m, msgs)
			continue
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// realWatcher attaches a real watcher on the vault to opts.
func realWatcher(t *testing.T, opts *Options) {
	t.Helper()
	w, err := watcher.New(opts.Vault.Root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	opts.Watcher = w
}

func TestRunnerDeliversWatcherEvents(t *testing.T) {
	opts := testOptions(t)
	realWatcher(t, &opts)
	m := start(t, opts, 120, 30)
	writeFile(t, opts.Vault, "later.md", "# Later #fresh\n")
	waitFor(t, m, func() bool {
		n, ok := m.ix.Get("later.md")
		return ok && strings.Contains(n.Content, "#fresh")
	})
}

func TestRunnerRecognizesTicks(t *testing.T) {
	if !isTick(funcName(tea.Tick(time.Hour, func(time.Time) tea.Msg { return nil }))) {
		t.Error("tea.Tick not recognized")
	}
	if isTick(funcName(func() tea.Msg { return nil })) {
		t.Error("a plain command taken for a tick")
	}
	w, err := watcher.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if !isListener(funcName(listenWatcherCmd(w))) {
		t.Error("watcher listener not recognized")
	}
	tw, err := newThemeWatcher(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tw.Close() }()
	if !isListener(funcName(listenThemesCmd(tw))) {
		t.Error("theme watcher listener not recognized")
	}
}
