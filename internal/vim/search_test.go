package vim

import (
	"testing"
)

func TestSearch(t *testing.T) {
	tests := []struct {
		name, in, keys, want, msg string
	}{
		{"forward", "|foo bar foo", "/foo<cr>", "foo bar |foo", ""},
		{"forward next line", "|ab\ncd ab", "/ab<cr>", "ab\ncd |ab", ""},
		{"wraps", "foo |bar\nx", "/foo<cr>", "|foo bar\nx", msgWrapBottom},
		{"only match at cursor wraps to itself", "|foo", "/foo<cr>", "|foo", msgWrapBottom},
		{"not found", "|abc", "/zz<cr>", "|abc", "Pattern not found: zz"},
		{"backward", "foo bar |foo", "?foo<cr>", "|foo bar foo", ""},
		{"backward wraps", "|foo\nbar foo", "?foo<cr>", "foo\nbar |foo", msgWrapTop},
		{"n", "|a x a x a", "/a<cr>n", "a x a x |a", ""},
		{"n wraps", "|a x a", "/a<cr>n", "|a x a", msgWrapBottom},
		{"N", "a x a x |a", "/a<cr>N", "a x a x |a", msgWrapTop},
		{"N reverses", "a x |a x a", "/a<cr>NN", "|a x a x a", ""},
		{"? then n goes backward", "a x a x |a", "?a<cr>n", "|a x a x a", ""},
		{"? then N goes forward", "a x |a x a", "?a<cr>N", "a x |a x a", ""},
		{"2n", "|a a a a", "/a<cr>2n", "a a a |a", ""},
		{"count search", "|a a a a", "3/a<cr>", "a a a |a", ""},
		{"count search backward", "a a a |a", "2?a<cr>", "a |a a a", ""},
		{"count search wraps", "a |a a", "3/a<cr>", "a |a a", msgWrapBottom},
		{"count sets nothing for n", "|a a a a", "2/a<cr>n", "a a a |a", ""},
		{"visual count search", "|ab ab ab", "v2/a<cr>d", "|b", ""},
		{"smart case lower matches upper", "|x Foo", "/foo<cr>", "x |Foo", ""},
		{"smart case upper is exact", "|foo Foo", "/Foo<cr>", "foo |Foo", ""},
		{"smart case upper skips lower", "|x foo", "/Foo<cr>", "|x foo", "Pattern not found: Foo"},
		{"literal regex chars", "|a.b axb", "/x<cr>", "a.b a|xb", ""},
		{"literal dot", "|axb a.b", "/.<cr>", "axb a|.b", ""},
		{"unicode", "|é x é", "/é<cr>", "é x |é", ""},
		{"space in pattern", "|ab a b", "/a<space>b<cr>", "ab |a b", ""},
		{"esc cancels", "|foo foo", "/foo<esc>", "|foo foo", ""},
		{"backspace edits", "|ab ac", "/ab<bs>c<cr>", "ab |ac", ""},
		{"backspace on empty exits", "|ab", "/<bs>l", "a|b", ""},
		{"empty pattern reuses last", "|a a a", "/a<cr>/<cr>", "a a |a", ""},
		{"n without pattern", "|abc", "n", "|abc", msgNoPrevious},
		{"dn deletes to match", "|foo bar foo", "/bar<cr>0dn", "|bar foo", ""},
		{"visual search extends", "|ab cd ef", "v/e<cr>d", "|f", ""},
		{"search after multi-line", "|x\ny\nz foo", "/foo<cr>", "x\ny\nz |foo", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, b, eff := run(t, tt.in, tt.keys)
			if got := show(b); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if eff.Message != tt.msg {
				t.Errorf("Message = %q, want %q", eff.Message, tt.msg)
			}
		})
	}
}

func TestCommandLineText(t *testing.T) {
	m, b, _ := run(t, "|abc", ":wq")
	if m.Mode() != Command || m.CommandLine() != ":wq" || m.ModeName() != "COMMAND" {
		t.Errorf("mode %v cmdline %q", m.Mode(), m.CommandLine())
	}
	feed(m, b, "<bs>")
	if m.CommandLine() != ":w" {
		t.Errorf("after bs %q", m.CommandLine())
	}
	feed(m, b, "<esc>")
	if m.Mode() != Normal || m.CommandLine() != "" {
		t.Errorf("after esc mode %v cmdline %q", m.Mode(), m.CommandLine())
	}
	m, _, _ = run(t, "|abc", "?fo")
	if m.CommandLine() != "?fo" {
		t.Errorf("search cmdline %q", m.CommandLine())
	}
}

func TestExCommands(t *testing.T) {
	tests := []struct {
		name, keys string
		want       Effect
	}{
		{"w", ":w<cr>", Effect{Save: true}},
		{"q", ":q<cr>", Effect{Quit: true}},
		{"q!", ":q!<cr>", Effect{Quit: true}},
		{"wq", ":wq<cr>", Effect{Save: true, Quit: true}},
		{"x", ":x<cr>", Effect{Save: true, Quit: true}},
		{"e note", ":e my note<cr>", Effect{OpenNote: true, NoteArg: "my note"}},
		{"e no name", ":e<cr>", Effect{Message: "E32: No file name"}},
		{"img", ":img ~/a.png<cr>", Effect{ImgArg: "~/a.png"}},
		{"img no arg", ":img<cr>", Effect{Message: "E471: Argument required"}},
		{"help", ":help<cr>", Effect{Help: true}},
		{"h", ":h<cr>", Effect{Help: true}},
		{"unknown", ":xyz<cr>", Effect{Message: "Not an editor command: xyz"}},
		{"empty", ":<cr>", Effect{}},
		{"leading spaces", ": w <cr>", Effect{Save: true}},
		{"esc cancels", ":w<esc>", Effect{}},
		{"c-c cancels", ":w<c-c>", Effect{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _, eff := run(t, "|abc", tt.keys)
			if eff != tt.want {
				t.Errorf("Effect = %+v, want %+v", eff, tt.want)
			}
			if m.Mode() != Normal {
				t.Errorf("mode = %v", m.Mode())
			}
		})
	}
}

func TestExGotoLine(t *testing.T) {
	runEditCases(t, []editCase{
		{":3", "|a\nb\n  c\nd", ":3<cr>", "a\nb\n  |c\nd"},
		{":0", "a\n|b", ":0<cr>", "|a\nb"},
		{":99", "|a\nb", ":99<cr>", "a\n|b"},
		{":$", "|a\nb", ":$<cr>", "a\n|b"},
	})
}

func TestFocusKeys(t *testing.T) {
	tests := []struct {
		keys          string
		sidebar, main bool
	}{
		{"<tab>", true, false},
		{"<c-w>h", true, false},
		{"<c-w>l", false, true},
		{"<c-w><c-h>", true, false},
		{"<c-w>x", false, false},
	}
	for _, tt := range tests {
		_, _, eff := run(t, "|abc", tt.keys)
		if eff.FocusSidebar != tt.sidebar || eff.FocusMain != tt.main {
			t.Errorf("%q: sidebar %v main %v", tt.keys, eff.FocusSidebar, eff.FocusMain)
		}
	}
}

func TestReadOnly(t *testing.T) {
	tests := []struct {
		name, in, keys, want string
		blocked              bool
	}{
		{"motion ok", "|abc def", "w", "abc |def", false},
		{"search ok", "|abc def", "/d<cr>", "abc |def", false},
		{"yank ok", "|abc", "yy", "|abc", false},
		{"visual yank ok", "|abc", "vly", "|abc", false},
		{"x blocked", "|abc", "x", "|abc", true},
		{"dd blocked", "|abc\nd", "dd", "|abc\nd", true},
		{"dw blocked", "|abc d", "dw", "|abc d", true},
		{"cw blocked", "|abc d", "cw", "|abc d", true},
		{"i blocked", "|abc", "iX", "|abc", true},
		{"o blocked", "|abc", "o", "|abc", true},
		{"p blocked", "|abc", "yyp", "|abc", true},
		{"\"+p blocked", "|abc", `"+p`, "|abc", true},
		{"J blocked", "|a\nb", "J", "|a\nb", true},
		{">> blocked", "|a", ">>", "|a", true},
		{"r blocked", "|a", "rx", "|a", true},
		{"u blocked", "|a", "u", "|a", true},
		{"dot blocked", "|a", ".", "|a", true},
		{"visual d blocked", "|abc", "vld", "a|bc", true},
		{"visual > blocked", "|abc", "V>", "|abc", true},
		{"visual r blocked", "|abc", "vlrx", "a|bc", true},
		{"tab ok", "|abc", "<tab>", "|abc", false},
		{"ex ok", "|abc", ":w<cr>", "|abc", false},
		{"count then blocked", "|abc", "2x", "|abc", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			m.SetReadOnly(true)
			b := newBuf(tt.in)
			eff := feed(m, b, tt.keys)
			if got := show(b); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if eff.Blocked != tt.blocked {
				t.Errorf("Blocked = %v, want %v", eff.Blocked, tt.blocked)
			}
			if m.Mode() == Insert {
				t.Errorf("entered insert mode")
			}
			if b.Dirty() {
				t.Errorf("buffer modified")
			}
		})
	}
}

func TestReadOnlyModeNameAndSwitch(t *testing.T) {
	m := New()
	b := newBuf("|abc")
	feed(m, b, "iX")
	m.SetReadOnly(true)
	if m.Mode() != Normal || m.ModeName() != "READ-ONLY" {
		t.Errorf("mode %v %q", m.Mode(), m.ModeName())
	}
	m.PasteClipboard(b, "zzz", false)
	if got := b.String(); got != "Xabc" {
		t.Errorf("text %q", got)
	}
	b.Undo()
	if got := b.String(); got != "abc" {
		t.Errorf("insert session not closed: after undo %q", got)
	}
	m.SetReadOnly(false)
	feed(m, b, "x")
	if got := b.String(); got != "bc" {
		t.Errorf("after leaving read-only x: %q", got)
	}
}
