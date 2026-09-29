package vim

import (
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/buffer"
)

// specialKeys maps the angle-bracket names used in test key strings.
var specialKeys = map[string]Key{
	"esc":     {Name: "esc"},
	"cr":      {Name: "enter"},
	"bs":      {Name: "backspace"},
	"del":     {Name: "delete"},
	"tab":     {Name: "tab"},
	"s-tab":   {Name: "tab", Shift: true},
	"space":   {Name: "space", Text: " ", Code: ' '},
	"up":      {Name: "up"},
	"down":    {Name: "down"},
	"left":    {Name: "left"},
	"right":   {Name: "right"},
	"home":    {Name: "home"},
	"end":     {Name: "end"},
	"s-up":    {Name: "up", Shift: true},
	"s-down":  {Name: "down", Shift: true},
	"s-left":  {Name: "left", Shift: true},
	"s-right": {Name: "right", Shift: true},
	"s-home":  {Name: "home", Shift: true},
	"s-end":   {Name: "end", Shift: true},
	"lt":      {Text: "<", Code: '<'},
}

// parseKeys turns "3dw<esc>" into keys. "<c-x>" is ctrl+x; an unknown
// bracket sequence is typed literally.
func parseKeys(s string) []Key {
	var keys []Key
	for s != "" {
		if s[0] == '<' {
			if end := strings.IndexByte(s, '>'); end > 1 {
				name := s[1:end]
				if k, ok := specialKeys[name]; ok {
					keys = append(keys, k)
					s = s[end+1:]
					continue
				}
				if len(name) == 3 && strings.HasPrefix(name, "c-") {
					keys = append(keys, Key{Code: rune(name[2]), Ctrl: true})
					s = s[end+1:]
					continue
				}
			}
		}
		g := buffer.Graphemes(s)[0]
		keys = append(keys, Key{Text: g, Code: []rune(g)[0]})
		s = s[len(g):]
	}
	return keys
}

// newBuf builds a buffer from text with the cursor marked by "|" (default:
// start of the buffer).
func newBuf(text string) *buffer.Buffer {
	cur := pos(0, 0)
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if j := strings.IndexByte(l, '|'); j >= 0 {
			cur = pos(i, buffer.ByteToCol(l, j))
			lines[i] = l[:j] + l[j+1:]
			break
		}
	}
	b := buffer.New(strings.Join(lines, "\n"))
	b.SetCursor(cur)
	return b
}

// show renders the buffer with "|" at the cursor.
func show(b *buffer.Buffer) string {
	var sb strings.Builder
	cur := b.Cursor()
	for i := 0; i < b.LineCount(); i++ {
		if i > 0 {
			sb.WriteByte('\n')
		}
		l := b.Line(i)
		if i == cur.Line {
			off := buffer.ColToByte(l, cur.Col)
			l = l[:off] + "|" + l[off:]
		}
		sb.WriteString(l)
	}
	return sb.String()
}

// mergeEffects combines the effects of a key sequence.
func mergeEffects(a, b Effect) Effect {
	a.Save = a.Save || b.Save
	a.Quit = a.Quit || b.Quit
	a.OpenNote = a.OpenNote || b.OpenNote
	a.Help = a.Help || b.Help
	a.NeedClipboard = a.NeedClipboard || b.NeedClipboard
	a.PasteBefore = a.PasteBefore || b.PasteBefore
	a.ToggleTask = a.ToggleTask || b.ToggleTask
	a.FocusSidebar = a.FocusSidebar || b.FocusSidebar
	a.FocusMain = a.FocusMain || b.FocusMain
	a.Blocked = a.Blocked || b.Blocked
	if b.NoteArg != "" {
		a.NoteArg = b.NoteArg
	}
	if b.ImgArg != "" {
		a.ImgArg = b.ImgArg
	}
	if b.Clipboard != nil {
		a.Clipboard = b.Clipboard
	}
	if b.Message != "" {
		a.Message = b.Message
	}
	return a
}

// feed sends keys to an editor and merges the effects.
func feed(e Editor, b *buffer.Buffer, keys string) Effect {
	var eff Effect
	for _, k := range parseKeys(keys) {
		eff = mergeEffects(eff, e.Handle(b, k))
	}
	return eff
}

// run creates a Machine and a buffer from text ("|" marks the cursor) and
// feeds it keys.
func run(t *testing.T, text, keys string) (*Machine, *buffer.Buffer, Effect) {
	t.Helper()
	m := New()
	b := newBuf(text)
	eff := feed(m, b, keys)
	return m, b, eff
}

// editCase is the common table row: text and cursor before and after.
type editCase struct {
	name, in, keys, want string
}

func runEditCases(t *testing.T, cases []editCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, b, _ := run(t, tc.in, tc.keys)
			if got := show(b); got != tc.want {
				t.Errorf("%q + %q:\n got %q\nwant %q", tc.in, tc.keys, got, tc.want)
			}
		})
	}
}

func TestParseKeys(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"3dw", []string{"3", "d", "w"}},
		{"<esc>", []string{"<esc>"}},
		{"<c-r>", []string{"<c-r>"}},
		{"<<", []string{"<", "<"}},
		{"<s-tab>", []string{"<s-tab>"}},
		{"a<space>b", []string{"a", "<space>", "b"}},
		{"<lt>", []string{"<"}},
		{"<cr><bs><del>", []string{"<cr>", "<bs>", "<del>"}},
	}
	for _, tt := range tests {
		var got []string
		for _, k := range parseKeys(tt.in) {
			got = append(got, keyToken(k))
		}
		if strings.Join(got, " ") != strings.Join(tt.want, " ") {
			t.Errorf("parseKeys(%q) tokens = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestKeyTokensSplitsText(t *testing.T) {
	got := keyTokens(Key{Text: "ab c"})
	want := []string{"a", "b", "<space>", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("keyTokens = %q, want %q", got, want)
	}
	if got := keyToken(Key{Code: 'R', Ctrl: true}); got != "<c-r>" {
		t.Errorf("ctrl+R token = %q", got)
	}
}
