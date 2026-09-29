package resolver

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func testOpts(t *testing.T, vim bool) (theme.Styles, theme.Palette, editor.Options) {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	s := theme.NewStyles(p)
	return s, p, editor.Options{Vim: vim, Styles: s, Palette: p}
}

// newModel returns a resolver over files at w×h, with vim mode off.
func newModel(t *testing.T, w, h int, files ...File) Model {
	t.Helper()
	return newModelVim(t, false, w, h, files...)
}

func newModelVim(t *testing.T, vim bool, w, h int, files ...File) Model {
	t.Helper()
	s, p, opts := testOpts(t, vim)
	caps := imgrender.Caps{Inline: imgrender.ProtoHalfBlocks, CellW: 8, CellH: 16}
	return New(files, s, p, caps, opts).SetSize(w, h)
}

// key builds a key press from its String() form.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if rest, ok := strings.CutPrefix(s, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: []rune(rest)[0], Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

// press feeds keys one by one (each key string is one key; use "]", "c"
// for chords) and returns the model and every message the commands
// produced, after feeding the resolver's own messages (Owns) back, as the
// app does.
func press(t *testing.T, m Model, keys ...string) (Model, []tea.Msg) {
	t.Helper()
	var out []tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		var got []tea.Msg
		m, got = drain(m, cmd)
		out = append(out, got...)
	}
	return m, out
}

// drain runs cmd, feeds the resolver's own messages back into m and returns
// the other messages. Timer commands (tea.Tick) are skipped.
func drain(m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	var out []tea.Msg
	for _, msg := range collect(cmd) {
		if !Owns(msg) {
			out = append(out, msg)
			continue
		}
		var next tea.Cmd
		m, next = m.Update(msg)
		var more []tea.Msg
		m, more = drain(m, next)
		out = append(out, more...)
	}
	return m, out
}

var cmdType = reflect.TypeFor[tea.Cmd]()

// collect runs cmd and flattens batches and sequences into their messages.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := runQuick(cmd)
	if msg == nil {
		return nil
	}
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, collect(c)...)
		}
		return out
	}
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Slice && v.Type().Elem() == cmdType {
		var out []tea.Msg
		for i := range v.Len() {
			c, _ := v.Index(i).Interface().(tea.Cmd)
			out = append(out, collect(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// runQuick runs cmd but gives up after a short wait on commands that block
// (timers such as the editor's flash), which the tests never need. The
// goroutine running such a command is left behind until its timer fires;
// the timers are short, so this is harmless in tests.
func runQuick(cmd tea.Cmd) tea.Msg {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-timeAfter():
		return nil
	}
}

// only returns the messages of type T.
func only[T any](msgs []tea.Msg) []T {
	var out []T
	for _, m := range msgs {
		if v, ok := m.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

func textFile(path, base, ours, theirs string) File {
	f := File{Path: path, Kind: Text, Ours: []byte(ours), Theirs: []byte(theirs)}
	if base != "\x00" {
		f.Base = []byte(base)
	}
	return f
}

// noBase marks a text file created on both sides.
const noBase = "\x00"

func timeAfter() <-chan time.Time { return time.After(50 * time.Millisecond) }
