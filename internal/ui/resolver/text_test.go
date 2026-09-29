package resolver

import (
	"strings"
	"testing"
)

func TestTextChoicesProduceResolvedContent(t *testing.T) {
	tests := []struct {
		name               string
		base, ours, theirs string
		keys               []string
		want               string
	}{
		{"yours", base1, ours1, theirs1, []string{"o"}, "a\nX\nc\n"},
		{"theirs", base1, ours1, theirs1, []string{"t"}, "a\nY\nc\n"},
		{"both is yours then theirs", base1, ours1, theirs1, []string{"b"}, "a\nX\nY\nc\n"},
		{"change of mind", base1, ours1, theirs1, []string{"o", "t"}, "a\nY\nc\n"},
		{"two conflicts, auto-advance", base2, ours2, theirs2, []string{"o", "t"}, "1\nA\n3\nD\n5\n"},
		{"clean blocks pre-filled", "a\nb\nc\nd\n", "Z\nb\nX\nd\n", "a\nb\nY\nd\nW\n", []string{"t"}, "Z\nb\nY\nd\nW\n"},
		{"no trailing newline on either side", "a\nb", "a\nX", "a\nY", []string{"o"}, "a\nX"},
		{"trailing newline when one side has it", "a\nb", "a\nX", "a\nY\n", []string{"o"}, "a\nX\n"},
		{"deletion chosen", base1, "a\nc\n", theirs1, []string{"o"}, "a\nc\n"},
		{"two-way diff without base", noBase, "a\nb\n", "a\nc\n", []string{"b"}, "a\nb\nc\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, 120, 40, textFile("n.md", tt.base, tt.ours, tt.theirs))
			m, _ = press(t, m, tt.keys...)
			m, out := press(t, m, "enter")
			got := only[ResolveTextMsg](out)
			if len(got) != 1 {
				t.Fatalf("enter produced %v, want one ResolveTextMsg", out)
			}
			if got[0].Path != "n.md" || string(got[0].Content) != tt.want {
				t.Fatalf("ResolveTextMsg = %q %q, want %q", got[0].Path, got[0].Content, tt.want)
			}
			if !m.items[0].pending {
				t.Error("file not marked pending after the request")
			}
		})
	}
}

func TestTwoWayDiffWhenNoBase(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", noBase, "a\nb\n", "a\nc\n"))
	tx := m.items[0].text
	if !tx.twoWay || len(tx.conflicts) != 1 {
		t.Fatalf("twoWay=%v conflicts=%d, want two-way with 1 conflict", tx.twoWay, len(tx.conflicts))
	}
	if !strings.Contains(plain(m), "created on both sides") {
		t.Error("header does not mention the two-way diff")
	}
}

func TestConflictNavigation(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base2, ours2, theirs2))
	cur := func(m Model) int { return m.items[0].text.cur }
	steps := []struct {
		keys []string
		want int
	}{
		{nil, 0},
		{[]string{"]", "c"}, 1},
		{[]string{"]", "c"}, 1}, // stays on the last conflict
		{[]string{"[", "c"}, 0},
		{[]string{"[", "c"}, 0},
		{[]string{"]", "x", "c"}, 0}, // another key cancels the chord
	}
	for i, s := range steps {
		m, _ = press(t, m, s.keys...)
		if got := cur(m); got != s.want {
			t.Fatalf("step %d (%v): current conflict %d, want %d", i, s.keys, got, s.want)
		}
	}
	// u unsets the current block.
	m, _ = press(t, m, "o")
	if m.items[0].text.choices[0] != chooseOurs || cur(m) != 1 {
		t.Fatalf("o: choices %v cur %d", m.items[0].text.choices, cur(m))
	}
	m, _ = press(t, m, "[", "c", "u")
	if m.items[0].text.choices[0] != unset {
		t.Fatalf("u did not unset: %v", m.items[0].text.choices)
	}
}

func TestJumpScrollsColumns(t *testing.T) {
	var b, o, th strings.Builder
	for i := range 100 {
		line := strings.Repeat("x", i%7) + "\n"
		b.WriteString(line)
		o.WriteString(line)
		th.WriteString(line)
		if i == 10 || i == 80 {
			b.WriteString("base\n")
			o.WriteString("ours\n")
			th.WriteString("theirs\n")
		}
	}
	m := newModel(t, 120, 30, textFile("n.md", b.String(), o.String(), th.String()))
	if len(m.items[0].text.conflicts) != 2 {
		t.Fatalf("want 2 conflicts, got %d", len(m.items[0].text.conflicts))
	}
	m, _ = press(t, m, "]", "c")
	for c, s := range m.items[0].text.scroll {
		if s == 0 {
			t.Errorf("column %d not scrolled", c)
		}
	}
	if v := plain(m); !strings.Contains(v, "ours") || !strings.Contains(v, "theirs") {
		t.Errorf("second conflict not visible:\n%s", v)
	}
}

func TestEnterBlockedUntilAllResolved(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base2, ours2, theirs2))
	m, out := press(t, m, "enter")
	if len(only[ResolveTextMsg](out)) != 0 {
		t.Fatal("enter emitted ResolveTextMsg with unresolved blocks")
	}
	if !strings.Contains(plain(m), "Resolve every conflict first") {
		t.Error("no hint shown")
	}
	m, _ = press(t, m, "o")
	_, out = press(t, m, "enter")
	if len(only[ResolveTextMsg](out)) != 0 {
		t.Fatal("enter emitted ResolveTextMsg with one block unresolved")
	}
	m, _ = press(t, m, "t")
	if !strings.Contains(plain(m), "2 of 2 conflicts resolved") {
		t.Errorf("header count wrong:\n%s", plain(m))
	}
	_, out = press(t, m, "enter")
	if len(only[ResolveTextMsg](out)) != 1 {
		t.Fatal("enter did not emit ResolveTextMsg once all blocks were resolved")
	}
}

func TestTOMLResultMustParse(t *testing.T) {
	m := newModel(t, 120, 40, textFile(".notty/settings.toml", "a = 1\n", "a = 2\n", "a = [\n"))
	m, out := press(t, m, "t", "enter")
	if len(only[ResolveTextMsg](out)) != 0 {
		t.Fatal("invalid TOML was sent for writing")
	}
	if m.items[0].err == "" || !strings.Contains(plain(m), "Invalid TOML") {
		t.Fatalf("no TOML error shown: %q", m.items[0].err)
	}
	m, out = press(t, m, "o", "enter")
	if got := only[ResolveTextMsg](out); len(got) != 1 || string(got[0].Content) != "a = 2\n" {
		t.Fatalf("valid TOML not sent: %v", out)
	}
	if m.items[0].err != "" {
		t.Error("error not cleared by a new request")
	}
}

func TestResolverResultColumn(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base2, ours2, theirs2))
	v := plain(m)
	for _, want := range []string{"Yours", "Theirs", "Result", unresolvedPlaceholder, "0 of 2 conflicts resolved"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	m, _ = press(t, m, "o", "o")
	if strings.Contains(plain(m), unresolvedPlaceholder) {
		t.Error("placeholder still shown after resolving every block")
	}
}

func TestChoicesCopyOnWrite(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m2, _ := press(t, m, "o")
	if m.items[0].text.choices[0] != unset || m2.items[0].text.choices[0] != chooseOurs {
		t.Error("choice leaked into an earlier model copy")
	}
}
