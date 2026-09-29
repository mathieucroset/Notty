package editor

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/clipboard"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vim"
)

func init() { flashDuration = time.Millisecond }

// collect runs cmd and returns the messages it produces, flattening
// batches. Ticks run for real, so tests use short delays.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, collect(c)...)
		}
		return out
	default:
		// tea.Sequence returns an unexported []tea.Cmd type.
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
			var out []tea.Msg
			for i := range v.Len() {
				out = append(out, collect(v.Index(i).Interface().(tea.Cmd))...)
			}
			return out
		}
		return []tea.Msg{msg}
	}
}

// find returns the first message of type T in ms.
func find[T any](ms []tea.Msg) (T, bool) {
	for _, m := range ms {
		if v, ok := m.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// press builds a key press from a short spelling: a printable string, or
// "esc", "enter", "tab", "space", "backspace", "ctrl+x", "shift+tab"...
func press(s string) tea.KeyPressMsg {
	var mod tea.KeyMod
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+"):
			mod |= tea.ModCtrl
			s = s[5:]
			continue
		case strings.HasPrefix(s, "alt+"):
			mod |= tea.ModAlt
			s = s[4:]
			continue
		case strings.HasPrefix(s, "shift+"):
			mod |= tea.ModShift
			s = s[6:]
			continue
		}
		break
	}
	codes := map[string]rune{
		"esc": tea.KeyEscape, "enter": tea.KeyEnter, "tab": tea.KeyTab,
		"space": tea.KeySpace, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd,
	}
	if c, ok := codes[s]; ok {
		return tea.KeyPressMsg{Code: c, Mod: mod}
	}
	r := []rune(s)[0]
	if mod&(tea.ModCtrl|tea.ModAlt) != 0 {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}
	return tea.KeyPressMsg{Code: r, Text: s, Mod: mod}
}

// typeKeys feeds space-separated keys (single characters are split) and
// returns the model and all produced messages.
func typeKeys(m Model, keys ...string) (Model, []tea.Msg) {
	var out []tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(press(k))
		out = append(out, collect(cmd)...)
	}
	return m, out
}

// chars splits s into one key per rune.
func chars(s string) []string {
	var out []string
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func TestTranslateKey(t *testing.T) {
	tests := []struct {
		name string
		in   tea.KeyPressMsg
		want vim.Key
	}{
		{"letter", tea.KeyPressMsg{Code: 'a', Text: "a"}, vim.Key{Code: 'a', Text: "a"}},
		{"shifted letter", tea.KeyPressMsg{Code: 'a', Text: "A", Mod: tea.ModShift}, vim.Key{Code: 'a', Text: "A", Shift: true}},
		{"cjk", tea.KeyPressMsg{Code: '日', Text: "日"}, vim.Key{Code: '日', Text: "日"}},
		{"no text printable", tea.KeyPressMsg{Code: 'x'}, vim.Key{Code: 'x', Text: "x"}},
		{"ctrl+r", tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, vim.Key{Code: 'r', Ctrl: true}},
		{"alt+x", tea.KeyPressMsg{Code: 'x', Text: "x", Mod: tea.ModAlt}, vim.Key{Code: 'x', Alt: true}},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}, vim.Key{Name: "esc"}},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, vim.Key{Name: "enter"}},
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, vim.Key{Name: "backspace"}},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}, vim.Key{Name: "delete"}},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}, vim.Key{Name: "tab"}},
		{"shift+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, vim.Key{Name: "tab", Shift: true}},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, vim.Key{Name: "space", Text: " "}},
		{"ctrl+space", tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}, vim.Key{Name: "space", Ctrl: true}},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, vim.Key{Name: "up"}},
		{"shift+down", tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, vim.Key{Name: "down", Shift: true}},
		{"left", tea.KeyPressMsg{Code: tea.KeyLeft}, vim.Key{Name: "left"}},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}, vim.Key{Name: "right"}},
		{"home", tea.KeyPressMsg{Code: tea.KeyHome}, vim.Key{Name: "home"}},
		{"end", tea.KeyPressMsg{Code: tea.KeyEnd}, vim.Key{Name: "end"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := translateKey(tt.in); got != tt.want {
				t.Errorf("translateKey = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTypingEditsAndEmitsChanged(t *testing.T) {
	m := newModel(t, testOptions(t), "world", buffer.Pos{}, 40, 5)
	m, out := typeKeys(m, append([]string{"i"}, chars("hello ")...)...)
	m, _ = typeKeys(m, "esc")
	if got := m.Content(); got != "hello world" {
		t.Fatalf("content = %q", got)
	}
	ch, ok := find[ChangedMsg](out)
	if !ok || ch.Path != "notes/test.md" {
		t.Errorf("no ChangedMsg for the note: %v", out)
	}
	if !m.Dirty() {
		t.Error("buffer not dirty after typing")
	}
	if rows := plainView(m); rows[0] != " hello world" {
		t.Errorf("view not updated: %q", rows[0])
	}
}

func TestEffectsToMessages(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want tea.Msg
	}{
		{":w", append(chars(":w"), "enter"), msgs.SaveRequestMsg{}},
		{":q", append(chars(":q"), "enter"), msgs.QuitMsg{}},
		{":wq quits", append(chars(":wq"), "enter"), msgs.QuitMsg{}},
		{":e adds .md", append(chars(":e work/ideas"), "enter"), msgs.OpenNoteMsg{Path: "work/ideas.md", Line: -1}},
		{":e keeps .md", append(chars(":e /a/b.md"), "enter"), msgs.OpenNoteMsg{Path: "a/b.md", Line: -1}},
		{":help", append(chars(":help"), "enter"), msgs.OpenHelpMsg{}},
		{":img", append(chars(":img /tmp/x.png"), "enter"), msgs.ImportImageMsg{Path: "/tmp/x.png"}},
		{"tab focuses sidebar", []string{"tab"}, msgs.FocusSidebarMsg{}},
		{"ctrl+w l focuses main", []string{"ctrl+w", "l"}, msgs.FocusMainMsg{}},
		{"message becomes status", append(chars(":bogus"), "enter"), StatusMsg{Text: "Not an editor command: bogus"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, testOptions(t), "text", buffer.Pos{}, 40, 5)
			_, out := typeKeys(m, tt.keys...)
			found := false
			for _, msg := range out {
				if reflect.DeepEqual(msg, tt.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("messages %v do not contain %#v", out, tt.want)
			}
		})
	}
}

func TestCommandLineHidesCursor(t *testing.T) {
	m := newModel(t, testOptions(t), "text", buffer.Pos{}, 40, 5)
	m, _ = typeKeys(m, ":", "w")
	if got := m.CommandLine(); got != ":w" {
		t.Errorf("CommandLine = %q", got)
	}
	if m.CursorPosition() != nil {
		t.Error("cursor shown in command mode")
	}
	if m.ModeName() != "COMMAND" {
		t.Errorf("ModeName = %q", m.ModeName())
	}
}

func TestCursorShapes(t *testing.T) {
	m := newModel(t, testOptions(t), "text", buffer.Pos{}, 40, 5)
	if c := m.CursorPosition(); c == nil || c.Shape != tea.CursorBlock {
		t.Fatalf("normal cursor = %+v", c)
	}
	m, _ = typeKeys(m, "i")
	if c := m.CursorPosition(); c == nil || c.Shape != tea.CursorBar {
		t.Fatalf("insert cursor = %+v", c)
	}
	m, _ = typeKeys(m, "esc", "v")
	if c := m.CursorPosition(); c == nil || c.Shape != tea.CursorBlock {
		t.Fatalf("visual cursor = %+v", c)
	}
	opts := testOptions(t)
	opts.Vim = false
	p := newModel(t, opts, "text", buffer.Pos{}, 40, 5)
	if c := p.CursorPosition(); c == nil || c.Shape != tea.CursorBar {
		t.Fatalf("plain cursor = %+v", c)
	}
}

func TestGjUsesWrapLayout(t *testing.T) {
	// Width 12: text 11, wrap 10: "0123456789|abcdefghij".
	m := newModel(t, testOptions(t), "0123456789abcdefghij\nx", buffer.Pos{Col: 2}, 12, 5)
	m, _ = typeKeys(m, "g", "j")
	if got := m.Cursor(); got != (buffer.Pos{Line: 0, Col: 12}) {
		t.Errorf("gj moved to %+v, want {0 12}", got)
	}
	m, _ = typeKeys(m, "g", "k")
	if got := m.Cursor(); got != (buffer.Pos{Line: 0, Col: 2}) {
		t.Errorf("gk moved to %+v, want {0 2}", got)
	}
}

func TestVisualSelectionHighlighted(t *testing.T) {
	m := newModel(t, testOptions(t), "hello world", buffer.Pos{}, 40, 3)
	before := m.View()
	m, _ = typeKeys(m, "v", "e")
	after := m.View()
	if before == after {
		t.Error("selection does not change the rendering")
	}
	bg := fmt.Sprint(m.sty.style(sty{sel: true}).Render("hello"))
	if !strings.Contains(after, bg) {
		t.Errorf("selected text not rendered with the selection style:\n%q\nwant substring %q", after, bg)
	}
}

// fakeClipboard is an in-memory Clipboard.
type fakeClipboard struct {
	image    []byte
	imageErr error
	text     string
	textErr  error
	written  []string
	writeErr error
}

func (f *fakeClipboard) ReadImage() ([]byte, error) { return f.image, f.imageErr }
func (f *fakeClipboard) ReadText() (string, error)  { return f.text, f.textErr }
func (f *fakeClipboard) WriteText(s string) error {
	f.written = append(f.written, s)
	return f.writeErr
}

func TestClipboardCopy(t *testing.T) {
	cb := &fakeClipboard{}
	opts := testOptions(t)
	opts.Clipboard = cb
	m := newModel(t, opts, "hello", buffer.Pos{}, 40, 3)
	typeKeys(m, `"`, "+", "y", "y")
	if len(cb.written) != 1 || cb.written[0] != "hello\n" {
		t.Errorf("clipboard got %q", cb.written)
	}

	// A failing tool falls back to OSC52.
	cb2 := &fakeClipboard{writeErr: errors.New("no tool")}
	opts.Clipboard = cb2
	m = newModel(t, opts, "hello", buffer.Pos{}, 40, 3)
	_, out := typeKeys(m, `"`, "+", "y", "y")
	if len(out) != 1 || !strings.Contains(fmt.Sprintf("%T %v", out[0], out[0]), "hello") {
		t.Errorf("fallback messages = %#v, want the OSC52 clipboard message", out)
	}
}

func TestNeedClipboard(t *testing.T) {
	t.Run("image imports", func(t *testing.T) {
		opts := testOptions(t)
		opts.Clipboard = &fakeClipboard{image: []byte("PNG")}
		m := newModel(t, opts, "x", buffer.Pos{}, 40, 3)
		m, out := typeKeys(m, `"`, "+", "p")
		img, ok := find[clipboardImageMsg](out)
		if !ok {
			t.Fatalf("messages = %#v, want the clipboard image", out)
		}
		_, cmd := m.Update(img)
		imp, ok := find[msgs.ImportImageMsg](collect(cmd))
		if !ok || string(imp.Data) != "PNG" || imp.Ext != "png" {
			t.Errorf("got %#v, want ImportImageMsg with data", imp)
		}
		// An image read for a note that is no longer open is dropped.
		other := m.Load("other.md", "", buffer.Pos{})
		if _, cmd := other.Update(img); cmd != nil {
			t.Error("stale clipboard image imported into another note")
		}
	})
	t.Run("text pastes", func(t *testing.T) {
		opts := testOptions(t)
		opts.Clipboard = &fakeClipboard{imageErr: clipboard.ErrNoImage, text: "dG"}
		m := newModel(t, opts, "x", buffer.Pos{}, 40, 3)
		m, out := typeKeys(m, "i", "ctrl+v")
		txt, ok := find[clipboardTextMsg](out)
		if !ok {
			t.Fatalf("messages = %#v, want clipboard text", out)
		}
		m, _ = m.Update(txt)
		if got := m.Content(); got != "dGx" {
			t.Errorf("content = %q, want the text inserted literally", got)
		}
	})
	t.Run("missing tool falls back to text", func(t *testing.T) {
		opts := testOptions(t)
		opts.Clipboard = &fakeClipboard{imageErr: clipboard.ErrNoTool{Tool: "wl-paste"}, text: "abc"}
		m := newModel(t, opts, "x", buffer.Pos{}, 40, 3)
		_, out := typeKeys(m, "i", "ctrl+v")
		if _, ok := find[clipboardTextMsg](out); !ok {
			t.Errorf("messages = %#v, want clipboard text", out)
		}
	})
	t.Run("missing tool and no text toasts", func(t *testing.T) {
		opts := testOptions(t)
		opts.Clipboard = &fakeClipboard{imageErr: clipboard.ErrNoTool{Tool: "wl-paste"}}
		m := newModel(t, opts, "x", buffer.Pos{}, 40, 3)
		_, out := typeKeys(m, "i", "ctrl+v")
		toast, ok := find[msgs.ToastMsg](out)
		if !ok || !strings.Contains(toast.Text, "wl-paste") {
			t.Errorf("messages = %#v, want a toast naming wl-paste", out)
		}
	})
	t.Run("stale text for another note is dropped", func(t *testing.T) {
		m := newModel(t, testOptions(t), "x", buffer.Pos{}, 40, 3)
		m, _ = m.Update(clipboardTextMsg{path: "other.md", text: "zzz"})
		if m.Content() != "x" {
			t.Errorf("content = %q", m.Content())
		}
	})
}

func TestWriteQuitOrder(t *testing.T) {
	m := newModel(t, testOptions(t), "text", buffer.Pos{}, 40, 5)
	_, out := typeKeys(m, append(chars(":wq"), "enter")...)
	var order []string
	for _, msg := range out {
		switch msg.(type) {
		case msgs.SaveRequestMsg:
			order = append(order, "save")
		case msgs.QuitMsg:
			order = append(order, "quit")
		}
	}
	if strings.Join(order, ",") != "save,quit" {
		t.Errorf(":wq order = %v, want save then quit", order)
	}
}
