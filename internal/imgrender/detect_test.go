package imgrender

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func envMap(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

// fakeTTY answers queries from a pre-filled reply buffer and records what
// was written to it.
type fakeTTY struct {
	replies *bytes.Reader
	written bytes.Buffer
}

func newFakeTTY(replies string) *fakeTTY {
	return &fakeTTY{replies: bytes.NewReader([]byte(replies))}
}

func (f *fakeTTY) Read(p []byte) (int, error) {
	if f.written.Len() == 0 {
		return 0, errors.New("read before the queries were written")
	}
	return f.replies.Read(p)
}

func (f *fakeTTY) Write(p []byte) (int, error) { return f.written.Write(p) }

// silentTTY never answers: Read blocks until closed.
type silentTTY struct{ closed chan struct{} }

func (s *silentTTY) Read([]byte) (int, error) { <-s.closed; return 0, io.EOF }
func (s *silentTTY) Write(p []byte) (int, error) {
	return len(p), nil
}

// deadlineTTY blocks like silentTTY but honours SetReadDeadline.
type deadlineTTY struct {
	deadline time.Time
	set      int
}

func (d *deadlineTTY) SetReadDeadline(t time.Time) error { d.deadline = t; d.set++; return nil }
func (d *deadlineTTY) Write(p []byte) (int, error)       { return len(p), nil }
func (d *deadlineTTY) Read([]byte) (int, error) {
	time.Sleep(time.Until(d.deadline))
	return 0, errors.New("i/o timeout")
}

func noRun(t *testing.T) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		t.Errorf("unexpected run(%s %v)", name, args)
		return nil, errors.New("not allowed")
	}
}

const (
	replyKittyOK = "\x1b_Gi=31;OK\x1b\\"
	replyCell    = "\x1b[6;20;10t"
	replyDA1     = "\x1b[?62;22c"
	replyDA1Six  = "\x1b[?62;4;22c"
)

func TestDetectEnv(t *testing.T) {
	tests := []struct {
		name   string
		cfg    string
		env    func(string) string
		inline Protocol
		viewer Protocol
	}{
		{"xterm-kitty", "auto", envMap("TERM", "xterm-kitty"), ProtoKitty, ProtoKitty},
		{"kitty window id", "", envMap("TERM", "xterm-256color", "KITTY_WINDOW_ID", "3"), ProtoKitty, ProtoKitty},
		{"ghostty", "auto", envMap("TERM_PROGRAM", "ghostty"), ProtoKitty, ProtoKitty},
		{"wezterm", "auto", envMap("TERM_PROGRAM", "WezTerm"), ProtoHalfBlocks, ProtoITerm},
		{"iterm", "auto", envMap("TERM_PROGRAM", "iTerm.app"), ProtoHalfBlocks, ProtoITerm},
		{"plain xterm", "auto", envMap("TERM", "xterm-256color"), ProtoHalfBlocks, ProtoHalfBlocks},
		{"empty env", "auto", envMap(), ProtoHalfBlocks, ProtoHalfBlocks},
		{"nil env", "auto", nil, ProtoHalfBlocks, ProtoHalfBlocks},
		{"config off beats env", "off", envMap("TERM", "xterm-kitty"), ProtoOff, ProtoOff},
		{"config halfblocks beats env", "halfblocks", envMap("TERM", "xterm-kitty"), ProtoHalfBlocks, ProtoHalfBlocks},
		{"config kitty", "kitty", envMap(), ProtoKitty, ProtoKitty},
		{"config sixel", "sixel", envMap("TERM", "xterm-kitty"), ProtoHalfBlocks, ProtoSixel},
		{"config iterm", "iterm", envMap(), ProtoHalfBlocks, ProtoITerm},
		{"config case and space", " Kitty ", envMap(), ProtoKitty, ProtoKitty},
		{"unknown config means auto", "bogus", envMap("TERM", "xterm-kitty"), ProtoKitty, ProtoKitty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.cfg, tt.env, nil, noRun(t))
			if got.Inline != tt.inline || got.Viewer != tt.viewer {
				t.Errorf("got inline=%v viewer=%v, want inline=%v viewer=%v", got.Inline, got.Viewer, tt.inline, tt.viewer)
			}
			if got.CellW != 8 || got.CellH != 16 {
				t.Errorf("default cell size = %dx%d, want 8x16", got.CellW, got.CellH)
			}
			if got.TmuxPassthrough {
				t.Error("TmuxPassthrough set outside tmux")
			}
		})
	}
}

func TestDetectTTY(t *testing.T) {
	tests := []struct {
		name         string
		cfg          string
		env          func(string) string
		replies      string
		inline       Protocol
		viewer       Protocol
		cellW, cellH int
	}{
		{"kitty reply", "auto", envMap(), replyKittyOK + replyCell + replyDA1, ProtoKitty, ProtoKitty, 10, 20},
		{"sixel in DA1", "auto", envMap(), replyCell + replyDA1Six, ProtoHalfBlocks, ProtoSixel, 10, 20},
		{"kitty and sixel", "auto", envMap(), replyKittyOK + replyDA1Six, ProtoKitty, ProtoKitty, 8, 16},
		{"nothing special", "auto", envMap(), replyDA1, ProtoHalfBlocks, ProtoHalfBlocks, 8, 16},
		{"no replies at all", "auto", envMap(), "", ProtoHalfBlocks, ProtoHalfBlocks, 8, 16},
		{"wezterm env plus sixel reply prefers iterm", "auto", envMap("TERM_PROGRAM", "WezTerm"), replyDA1Six, ProtoHalfBlocks, ProtoITerm, 8, 16},
		{"kitty error reply is not kitty", "auto", envMap(), "\x1b_Gi=31;ENOENT:no such\x1b\\" + replyDA1, ProtoHalfBlocks, ProtoHalfBlocks, 8, 16},
		{"4 inside another number is not sixel", "auto", envMap(), "\x1b[?64;42c", ProtoHalfBlocks, ProtoHalfBlocks, 8, 16},
		{"replies split and preceded by noise", "auto", envMap(), "x\x1b[6;18;9t\x1b_Gi=31;OK\x1b\\\x1b[?1;4c", ProtoKitty, ProtoKitty, 9, 18},
		{"configured protocol keeps tty cell size", "halfblocks", envMap(), replyKittyOK + replyCell + replyDA1Six, ProtoHalfBlocks, ProtoHalfBlocks, 10, 20},
		{"zero cell size ignored", "auto", envMap(), "\x1b[6;0;0t" + replyDA1, ProtoHalfBlocks, ProtoHalfBlocks, 8, 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tty := newFakeTTY(tt.replies)
			got := Detect(tt.cfg, tt.env, tty, noRun(t))
			if got.Inline != tt.inline || got.Viewer != tt.viewer {
				t.Errorf("got inline=%v viewer=%v, want inline=%v viewer=%v", got.Inline, got.Viewer, tt.inline, tt.viewer)
			}
			if got.CellW != tt.cellW || got.CellH != tt.cellH {
				t.Errorf("cell = %dx%d, want %dx%d", got.CellW, got.CellH, tt.cellW, tt.cellH)
			}
			w := tty.written.String()
			for _, q := range []string{kittyQuery, "\x1b[16t", "\x1b[c"} {
				if !strings.Contains(w, q) {
					t.Errorf("query %q not written; wrote %q", q, w)
				}
			}
			if !strings.HasSuffix(w, "\x1b[c") {
				t.Errorf("DA1 must be the last query (sentinel), wrote %q", w)
			}
		})
	}
}

func TestDetectTTYTimeout(t *testing.T) {
	t.Run("goroutine fallback", func(t *testing.T) {
		tty := &silentTTY{closed: make(chan struct{})}
		defer close(tty.closed)
		start := time.Now()
		got := Detect("auto", envMap(), tty, noRun(t))
		if el := time.Since(start); el > 500*time.Millisecond {
			t.Errorf("Detect took %v, want about 100ms", el)
		}
		if got.Inline != ProtoHalfBlocks || got.CellW != 8 {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("read deadline", func(t *testing.T) {
		tty := &deadlineTTY{}
		start := time.Now()
		Detect("auto", envMap(), tty, noRun(t))
		if el := time.Since(start); el > 500*time.Millisecond {
			t.Errorf("Detect took %v, want about 100ms", el)
		}
		if tty.set < 2 || !tty.deadline.IsZero() {
			t.Errorf("deadline set %d times, final %v; want set then cleared", tty.set, tty.deadline)
		}
	})
}

func TestDetectTmux(t *testing.T) {
	tests := []struct {
		name        string
		out         string
		err         error
		passthrough bool
		inline      Protocol
		viewer      Protocol
	}{
		{"passthrough on", "on\n", nil, true, ProtoKitty, ProtoKitty},
		{"passthrough all", "all\n", nil, true, ProtoKitty, ProtoKitty},
		{"passthrough off", "off\n", nil, false, ProtoHalfBlocks, ProtoHalfBlocks},
		{"tmux fails", "", errors.New("no server"), false, ProtoHalfBlocks, ProtoHalfBlocks},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			run := func(name string, args ...string) ([]byte, error) {
				calls = append(calls, name+" "+strings.Join(args, " "))
				return []byte(tt.out), tt.err
			}
			env := envMap("TMUX", "/tmp/tmux-1000/default,1,0", "TERM", "tmux-256color", "KITTY_WINDOW_ID", "1")
			got := Detect("auto", env, nil, run)
			if len(calls) != 1 || calls[0] != "tmux show -gv allow-passthrough" {
				t.Errorf("run calls = %q", calls)
			}
			if got.TmuxPassthrough != tt.passthrough || got.Inline != tt.inline || got.Viewer != tt.viewer {
				t.Errorf("got %+v, want passthrough=%v inline=%v viewer=%v", got, tt.passthrough, tt.inline, tt.viewer)
			}
		})
	}
	t.Run("off stays off", func(t *testing.T) {
		run := func(string, ...string) ([]byte, error) { return []byte("off"), nil }
		got := Detect("off", envMap("TMUX", "x"), nil, run)
		if got.Inline != ProtoOff || got.Viewer != ProtoOff {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("nil run", func(t *testing.T) {
		got := Detect("auto", envMap("TMUX", "x", "TERM", "xterm-kitty"), nil, nil)
		if got.TmuxPassthrough || got.Inline != ProtoHalfBlocks {
			t.Errorf("got %+v", got)
		}
	})
}

func TestProtocolString(t *testing.T) {
	want := map[Protocol]string{ProtoOff: "off", ProtoHalfBlocks: "halfblocks", ProtoKitty: "kitty", ProtoSixel: "sixel", ProtoITerm: "iterm", Protocol(99): "Protocol(99)"}
	for p, s := range want {
		if p.String() != s {
			t.Errorf("%d.String() = %q, want %q", int(p), p.String(), s)
		}
	}
}
