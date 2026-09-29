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
		{"ctrl+q quits the app", "\x11", []Key{QuitApp}},
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
		{"kitty keyboard ctrl+q", "\x1b[113;5u", []Key{QuitApp}},
		{"kitty keyboard ctrl+c", "\x1b[99;5u", []Key{Quit}},
		{"kitty keyboard ctrl+n is not n", "\x1b[110;5u", []Key{Unknown}},
		{"kitty keyboard alt+q is not q", "\x1b[113;3u", []Key{Unknown}},
		{"kitty keyboard shift+n", "\x1b[110;2u", []Key{Next}},
		{"kitty keyboard ctrl+q with lock keys", "\x1b[113;69u", []Key{QuitApp}},
		{"kitty keyboard plain c", "\x1b[99u", []Key{Unknown}},
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
	for k, want := range map[Key]string{Unknown: "unknown", Next: "next", Prev: "prev", Open: "open", Quit: "quit", QuitApp: "quit-app"} {
		if got := k.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(k), got, want)
		}
	}
}

func TestDecodeRest(t *testing.T) {
	tests := []struct {
		in   string
		keys []Key
		rest int
	}{
		{"\x1b", nil, 1},
		{"n\x1b", []Key{Next}, 1},
		{"\x1b\x1b", []Key{Quit}, 1},
		{"\x1b[", nil, 2},
		{"q\x1b[1;", []Key{Quit}, 4},
		{"\x1bO", nil, 2},
		{"\x1b_Gi=1;OK\x1b", nil, 10},
		{"\x1b]11;rgb", nil, 8},
		{"n\xc3", []Key{Next}, 1}, // first byte of é
		{"\x1b[C", []Key{Next}, 0},
		{"\x1b[1;\x1b", nil, 0}, // malformed CSI is dropped, not pending
		{"q", []Key{Quit}, 0},
	}
	for _, tt := range tests {
		keys, rest := decode([]byte(tt.in), false)
		if !reflect.DeepEqual(keys, tt.keys) || rest != tt.rest {
			t.Errorf("decode(%q, false) = %v, %d; want %v, %d", tt.in, keys, rest, tt.keys, tt.rest)
		}
		if final, _ := decode([]byte(tt.in), true); !reflect.DeepEqual(final, DecodeKeys([]byte(tt.in))) {
			t.Errorf("decode(%q, true) = %v, want the DecodeKeys result", tt.in, final)
		}
	}
}
