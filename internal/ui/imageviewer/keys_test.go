package imageviewer

import (
	"reflect"
	"testing"
)

func TestDecodeKeys(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Key
	}{
		{"empty", "", nil},
		{"n", "n", []Key{Next}},
		{"p", "p", []Key{Prev}},
		{"o", "o", []Key{Open}},
		{"q", "q", []Key{Quit}},
		{"space is next", " ", []Key{Next}},
		{"backspace DEL is prev", "\x7f", []Key{Prev}},
		{"backspace BS is prev", "\x08", []Key{Prev}},
		{"ctrl+c closes", "\x03", []Key{Quit}},
		{"ctrl+q closes", "\x11", []Key{Quit}},
		{"lone esc", "\x1b", []Key{Quit}},
		{"double esc", "\x1b\x1b", []Key{Quit, Quit}},
		{"unknown letter", "x", []Key{Unknown}},
		{"unknown utf8 rune", "é", []Key{Unknown}},
		{"several keys", "nnpq", []Key{Next, Next, Prev, Quit}},
		{"csi right", "\x1b[C", []Key{Next}},
		{"csi left", "\x1b[D", []Key{Prev}},
		{"csi down", "\x1b[B", []Key{Next}},
		{"csi up", "\x1b[A", []Key{Prev}},
		{"ss3 right", "\x1bOC", []Key{Next}},
		{"ss3 left", "\x1bOD", []Key{Prev}},
		{"ss3 down", "\x1bOB", []Key{Next}},
		{"ss3 up", "\x1bOA", []Key{Prev}},
		{"modified arrow", "\x1b[1;2C", []Key{Next}},
		{"arrows mixed with keys", "\x1b[Cn\x1bODq", []Key{Next, Next, Prev, Quit}},
		{"esc then key", "\x1bq", []Key{Unknown}}, // alt+q: not a lone esc
		{"esc sequence is not a lone esc", "\x1b[2~", nil},
		{"csi DA reply ignored", "\x1b[?62;4c", nil},
		{"csi cell size reply ignored", "\x1b[6;16;8t", nil},
		{"kitty apc reply ignored", "\x1b_Gi=31;OK\x1b\\", nil},
		{"kitty apc reply between keys", "n\x1b_Gi=1;EINVAL:bad\x1b\\q", []Key{Next, Quit}},
		{"osc reply ignored", "\x1b]11;rgb:0000/0000/0000\x07n", []Key{Next}},
		{"dcs reply ignored", "\x1bP1$r0m\x1b\\p", []Key{Prev}},
		{"unterminated apc swallowed", "\x1b_Gi=31;O", nil},
		{"incomplete csi swallowed", "\x1b[1;", nil},
		{"ss3 other ignored", "\x1bOP", nil},
		{"kitty keyboard q", "\x1b[113u", []Key{Quit}},
		{"kitty keyboard esc", "\x1b[27u", []Key{Quit}},
		{"kitty keyboard n with mods", "\x1b[110;1u", []Key{Next}},
		{"kitty keyboard release ignored", "\x1b[113;1:3u", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeKeys([]byte(tt.in))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecodeKeys(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestKeyString(t *testing.T) {
	for k, want := range map[Key]string{Unknown: "unknown", Next: "next", Prev: "prev", Open: "open", Quit: "quit"} {
		if got := k.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(k), got, want)
		}
	}
}
