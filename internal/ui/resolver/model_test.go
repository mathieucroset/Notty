package resolver

import (
	"slices"
	"testing"

	"github.com/mathieucroset/notty/internal/ui/keys"
)

const (
	base1   = "a\nb\nc\n"
	ours1   = "a\nX\nc\n"
	theirs1 = "a\nY\nc\n"

	base2   = "1\n2\n3\n4\n5\n"
	ours2   = "1\nA\n3\nC\n5\n"
	theirs2 = "1\nB\n3\nD\n5\n"
)

func TestTextStateContent(t *testing.T) {
	tests := []struct {
		name               string
		base, ours, theirs string
		choices            []choice
		want               string
	}{
		{"yours", base1, ours1, theirs1, []choice{chooseOurs}, "a\nX\nc\n"},
		{"theirs", base1, ours1, theirs1, []choice{chooseTheirs}, "a\nY\nc\n"},
		{"both", base1, ours1, theirs1, []choice{chooseBoth}, "a\nX\nY\nc\n"},
		{"two conflicts", base2, ours2, theirs2, []choice{chooseTheirs, chooseOurs}, "1\nB\n3\nC\n5\n"},
		{"trailing newline from theirs", "a\nb", "a\nX", "a\nY\n", []choice{chooseOurs}, "a\nX\n"},
		{"no trailing newline", "a\nb", "a\nX", "a\nY", []choice{chooseTheirs}, "a\nY"},
		{"two-way", noBase, "a\nb\n", "a\nc\n", []choice{chooseOurs}, "a\nb\n"},
		{"clean merge, no conflicts", base2, "1\nA\n3\n4\n5\n", "1\n2\n3\nD\n5\n", nil, "1\nA\n3\nD\n5\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTextState(textFile("n.md", tt.base, tt.ours, tt.theirs))
			if len(ts.conflicts) != len(tt.choices) {
				t.Fatalf("%d conflicts, want %d", len(ts.conflicts), len(tt.choices))
			}
			for i, c := range tt.choices {
				if ts.allResolved() {
					t.Fatal("allResolved before every choice was made")
				}
				ts.cur = i
				ts.setChoice(c)
			}
			if !ts.allResolved() || ts.resolvedCount() != len(tt.choices) {
				t.Fatalf("resolved %d of %d", ts.resolvedCount(), len(tt.choices))
			}
			if got := ts.content(); got != tt.want {
				t.Fatalf("content = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTextStateColumnLines(t *testing.T) {
	ts := newTextState(textFile("n.md", base1, "a\nc\n", theirs1))
	texts := func(c column) []string {
		var out []string
		for _, l := range ts.columnLines(c) {
			out = append(out, l.text)
		}
		return out
	}
	if got := texts(colOurs); !slices.Equal(got, []string{"a", emptySide, "c"}) {
		t.Errorf("yours = %q", got)
	}
	if got := texts(colTheirs); !slices.Equal(got, []string{"a", "Y", "c"}) {
		t.Errorf("theirs = %q", got)
	}
	if got := texts(colResult); !slices.Equal(got, []string{"a", unresolvedPlaceholder, "c"}) {
		t.Errorf("result = %q", got)
	}
	ts.setChoice(chooseBoth)
	if got := texts(colResult); !slices.Equal(got, []string{"a", "Y", "c"}) {
		t.Errorf("result after both = %q", got)
	}
	if s, e := conflictSpan(ts.columnLines(colResult), 0); s != 1 || e != 2 {
		t.Errorf("span = %d,%d", s, e)
	}
	ts.edited, ts.editedText = true, "x\ny\n"
	if got := texts(colResult); !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("edited result = %q", got)
	}
}

func TestTextStateEditText(t *testing.T) {
	ts := newTextState(textFile("n.md", base2, ours2, theirs2))
	ts.cur = 1
	ts.setChoice(chooseTheirs)
	if got := ts.editText(); got != "1\nA\n3\nD\n5\n" {
		t.Errorf("editText = %q, want unresolved blocks pre-filled with yours", got)
	}
	ts.hasDraft, ts.draft = true, "draft"
	if got := ts.editText(); got != "draft" {
		t.Errorf("editText with a draft = %q", got)
	}
	ts.setChoice(chooseOurs)
	if ts.hasDraft {
		t.Error("a new choice should drop the draft")
	}
}

func TestTextStateNavigationHelpers(t *testing.T) {
	ts := newTextState(textFile("n.md", "1\n2\n3\n4\n5\n6\n", "A\n2\nC\n4\nE\n6\n", "a\n2\nc\n4\ne\n6\n"))
	if len(ts.conflicts) != 3 {
		t.Fatalf("%d conflicts, want 3", len(ts.conflicts))
	}
	c := ts.clone()
	c.setChoice(chooseOurs)
	if ts.choices[0] != unset {
		t.Fatal("clone shares its choices")
	}
	if got := c.nextUnresolved(); got != 1 {
		t.Errorf("nextUnresolved = %d, want 1", got)
	}
	c.cur = 2
	if got := c.nextUnresolved(); got != 1 {
		t.Errorf("nextUnresolved wraps to %d, want 1", got)
	}
	c.cur = 1
	c.setChoice(chooseOurs)
	c.cur = 2
	c.setChoice(chooseOurs)
	if got := c.nextUnresolved(); got != 2 {
		t.Errorf("nextUnresolved with nothing left = %d, want the current 2", got)
	}
	// Scrolling: the third conflict sits on line 4 of 6.
	ts.cur = 2
	ts.scrollTo(2)
	for col, s := range ts.scroll {
		if s != 2 || clampScroll(s, 6, 2) != s {
			t.Errorf("column %d scroll %d, want 2", col, s)
		}
	}
	ts.scrollBy(10, 2)
	if ts.scroll[colOurs] != 4 {
		t.Errorf("scrollBy clamps to %d, want 4", ts.scroll[colOurs])
	}
	ts.scrollBy(-10, 2)
	if ts.scroll[colOurs] != 0 {
		t.Errorf("scrollBy clamps to %d, want 0", ts.scroll[colOurs])
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		path, content string
		bad           bool
	}{
		{"n.md", "a = [", false},
		{".notty/settings.toml", "a = 1\n", false},
		{".notty/settings.toml", "a = [\n", true},
		{"X.TOML", "= nope", true},
	}
	for _, tt := range tests {
		if got := validate(tt.path, tt.content); (got != "") != tt.bad {
			t.Errorf("validate(%q, %q) = %q", tt.path, tt.content, got)
		}
	}
}

func TestCloseAndFileNavigation(t *testing.T) {
	files := []File{
		textFile("a.md", base1, ours1, theirs1),
		{Path: "b.png", Kind: Binary, Ours: []byte{1}, Theirs: []byte{2}},
		{Path: "c.md", Kind: ModifyDelete, Ours: []byte("x\n"), Deleted: "theirs"},
	}
	m := newModel(t, 120, 40, files...)
	for _, k := range []string{"esc", "q"} {
		_, out := press(t, m, k)
		if len(only[CloseMsg](out)) != 1 {
			t.Errorf("%s did not emit CloseMsg", k)
		}
	}
	steps := []struct {
		key  string
		want string
	}{{"j", "b.png"}, {"down", "c.md"}, {"j", "c.md"}, {"k", "b.png"}, {"up", "a.md"}, {"k", "a.md"}}
	for _, s := range steps {
		m, _ = press(t, m, s.key)
		if got := m.Selected(); got != s.want {
			t.Fatalf("after %s selected %q, want %q", s.key, got, s.want)
		}
	}
	if m.KeyContext() != keys.Resolver {
		t.Error("KeyContext should be Resolver in navigation")
	}
	if m = m.Select("c.md"); m.Selected() != "c.md" {
		t.Error("Select did not select")
	}
	if m = m.Select("nope"); m.Selected() != "c.md" {
		t.Error("Select of an unknown path changed the selection")
	}
}

func TestMarkResolvedAllResolvedSetError(t *testing.T) {
	files := []File{
		textFile("a.md", base1, ours1, theirs1),
		{Path: "b.png", Kind: Binary, Ours: []byte{1}, Theirs: []byte{2}, Resolved: true},
		textFile("c.md", base1, ours1, theirs1),
	}
	m := newModel(t, 120, 40, files...)
	if m.AllResolved() {
		t.Fatal("AllResolved with unresolved files")
	}
	m = m.SetError("a.md", "write failed")
	if m.items[0].err != "write failed" {
		t.Error("error not stored")
	}
	m = m.MarkResolved("a.md")
	if m.Selected() != "c.md" {
		t.Errorf("selection after MarkResolved = %q, want next unresolved c.md", m.Selected())
	}
	if m.items[0].err != "" || !m.items[0].file.Resolved {
		t.Error("MarkResolved did not clear the error / mark resolved")
	}
	old := m
	m = m.MarkResolved("c.md")
	if !m.AllResolved() {
		t.Error("AllResolved false after resolving everything")
	}
	if old.AllResolved() {
		t.Error("MarkResolved modified an earlier copy")
	}
	m = m.MarkResolved("nope.md").SetError("nope.md", "x")
	if !m.AllResolved() {
		t.Error("unknown paths should be ignored")
	}
	if _, out := press(t, m, "o", "enter", "1"); len(out) != 0 {
		t.Errorf("keys on a resolved file produced %v", out)
	}
}

func TestNewSelectsFirstUnresolved(t *testing.T) {
	m := newModel(t, 120, 40,
		File{Path: "a.png", Kind: Binary, Resolved: true},
		textFile("b.md", base1, ours1, theirs1))
	if m.Selected() != "b.md" {
		t.Errorf("selected %q, want b.md", m.Selected())
	}
	empty := newModel(t, 120, 40)
	if !empty.AllResolved() || empty.Selected() != "" {
		t.Error("an empty resolver should be all resolved with no selection")
	}
	if _, out := press(t, empty, "j", "o", "enter"); len(out) != 0 {
		t.Errorf("empty resolver produced %v", out)
	}
}
