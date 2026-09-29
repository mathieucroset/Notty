package imgrender

import (
	"strings"
	"testing"
)

func TestDetectTmuxWrapsKittyQuery(t *testing.T) {
	run := func(string, ...string) ([]byte, error) { return []byte("on\n"), nil }
	tests := []struct {
		name string
		env  func(string) string
		want string
	}{
		{"inside tmux", envMap("TMUX", "/tmp/tmux-1000/default,1,0"), WrapTmux(kittyQuery) + cellQuery + da1Query},
		{"outside tmux", envMap(), kittyQuery + cellQuery + da1Query},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tty := newFakeTTY(replyKittyOK + replyDA1)
			got := Detect("auto", tt.env, tty, run)
			if w := tty.written.String(); w != tt.want {
				t.Errorf("wrote %q\nwant  %q", w, tt.want)
			}
			if !strings.HasPrefix(WrapTmux(kittyQuery), "\x1bPtmux;\x1b\x1b_G") {
				t.Error("WrapTmux did not wrap the query")
			}
			if got.Inline != ProtoKitty {
				t.Errorf("inline = %v, want kitty", got.Inline)
			}
		})
	}
}
