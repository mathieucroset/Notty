package editor

import (
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/clipboard"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// wrapped is a test envelope.
type wrapped struct{ inner tea.Msg }

func TestMapMsgWrapsEveryMessage(t *testing.T) {
	var saves atomic.Int32
	opts := testOptions(t)
	opts.Clipboard = &fakeClipboard{imageErr: clipboard.ErrNoImage, text: "P"}
	opts.MapMsg = func(msg tea.Msg) tea.Msg {
		switch msg.(type) {
		case msgs.SaveRequestMsg:
			saves.Add(1)
		case ChangedMsg:
			return nil // dropped
		}
		return wrapped{msg}
	}
	m := newModel(t, opts, "text", buffer.Pos{}, 40, 5)

	// Typing: the change notification is dropped, the autosave tick is
	// wrapped when it fires.
	m, out := typeKeys(m, "x")
	if len(out) != 1 {
		t.Fatalf("typing produced %#v, want only the wrapped tick", out)
	}
	w, ok := out[0].(wrapped)
	tick, isTick := w.inner.(AutosaveTickMsg)
	if !ok || !isTick {
		t.Fatalf("got %#v, want a wrapped AutosaveTickMsg", out[0])
	}
	_, cmd := m.Update(tick)
	if got := collect(cmd); len(got) != 1 || got[0] != (wrapped{msgs.SaveRequestMsg{}}) {
		t.Errorf("autosave produced %#v", got)
	}

	// :w is mapped synchronously, inside Update, before the command runs.
	saves.Store(0)
	m, cmd = m.Update(press(":"))
	collect(cmd)
	m, _ = m.Update(press("w"))
	_, cmd = m.Update(press("enter"))
	if saves.Load() != 1 {
		t.Errorf("save request mapped %d times inside Update, want 1", saves.Load())
	}
	for _, msg := range collect(cmd) {
		if _, ok := msg.(wrapped); !ok {
			t.Errorf("unwrapped message %#v", msg)
		}
	}

	// Clipboard reads come back wrapped.
	_, out = typeKeys(m, `"`, "+", "p")
	if len(out) != 1 {
		t.Fatalf("paste produced %#v", out)
	}
	if w, ok := out[0].(wrapped); !ok {
		t.Errorf("clipboard message not wrapped: %#v", out[0])
	} else if _, ok := w.inner.(clipboardTextMsg); !ok {
		t.Errorf("wrapped %#v, want clipboardTextMsg", w.inner)
	}
}

func TestMapMsgFlashTick(t *testing.T) {
	opts := testOptions(t)
	opts.MapMsg = func(msg tea.Msg) tea.Msg { return wrapped{msg} }
	m := newModel(t, opts, "text", buffer.Pos{}, 40, 5).SetReadOnly(true, "ro")
	_, out := typeKeys(m, "x")
	w, ok := find[wrapped](out)
	if !ok {
		t.Fatalf("flash produced %#v", out)
	}
	if _, ok := w.inner.(flashEndMsg); !ok {
		t.Errorf("wrapped %#v, want flashEndMsg", w.inner)
	}
}

func TestCanLeave(t *testing.T) {
	plainOpts := testOptions(t)
	plainOpts.Vim = false
	p := newModel(t, plainOpts, "text", buffer.Pos{}, 40, 5)
	if !p.CanLeave() {
		t.Error("plain editor should always be leavable")
	}
	tests := []struct {
		keys []string
		want bool
	}{
		{nil, true},
		{[]string{"i"}, false},
		{[]string{"i", "esc"}, true},
		{[]string{"v"}, false},
		{[]string{":"}, false},
		{[]string{"d"}, false},
		{[]string{"2"}, false},
		{[]string{"d", "esc"}, true},
	}
	for _, tt := range tests {
		m := newModel(t, testOptions(t), "text", buffer.Pos{}, 40, 5)
		m, _ = typeKeys(m, tt.keys...)
		if got := m.CanLeave(); got != tt.want {
			t.Errorf("after %v CanLeave = %v, want %v", tt.keys, got, tt.want)
		}
	}
}
