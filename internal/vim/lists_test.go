package vim

import "testing"

func TestContinueList(t *testing.T) {
	tests := []struct {
		line, prefix string
		end          bool
	}{
		{"- [ ] buy milk", "- [ ] ", false},
		{"- [x] done", "- [ ] ", false},
		{"- [X] done", "- [ ] ", false},
		{"  - [ ] nested", "  - [ ] ", false},
		{"\t* [ ] tab", "\t* [ ] ", false},
		{"- item", "- ", false},
		{"* item", "* ", false},
		{"+ item", "+ ", false},
		{"    - deep", "    - ", false},
		{"3. third", "4. ", false},
		{"3) third", "4) ", false},
		{"9. nine", "10. ", false},
		{"  1. nested", "  2. ", false},
		{"1. [ ] numbered task", "2. [ ] ", false},
		{"-  two spaces", "-  ", false},
		{"- [ ] ", "", true},
		{"- [ ]", "", true},
		{"- [x]   ", "", true},
		{"- ", "", true},
		{"  - ", "", true},
		{"4. ", "", true},
		{"plain text", "", false},
		{"", "", false},
		{"-", "", false},
		{"---", "", false},
		{"-item", "", false},
		{"3.x", "", false},
		{"# heading", "", false},
		{"- [x]done", "- ", false},
	}
	for _, tt := range tests {
		prefix, end := ContinueList(tt.line)
		if prefix != tt.prefix || end != tt.end {
			t.Errorf("ContinueList(%q) = %q, %v; want %q, %v", tt.line, prefix, end, tt.prefix, tt.end)
		}
	}
}

func TestListEditing(t *testing.T) {
	tests := []struct {
		name, in, keys, want string
		mode                 Mode
	}{
		{"enter continues task", "- [ ] a|", "A<cr>b", "- [ ] a\n- [ ] b|", Insert},
		{"enter on done task gives open task", "- [x] a|", "A<cr>", "- [x] a\n- [ ] |", Insert},
		{"enter continues bullet", "* a", "A<cr>", "* a\n* |", Insert},
		{"enter increments number", "1. a", "A<cr>", "1. a\n2. |", Insert},
		{"enter splits item", "- ab", "0lllli<cr>", "- a\n- |b", Insert},
		{"enter before marker is a plain split", "  - a", "0i<cr>", "\n|  - a", Insert},
		{"enter on empty item ends list", "- [ ] a\n- [ ] ", "GA<cr>", "- [ ] a\n|", Insert},
		{"enter on empty nested item keeps indent", "  - ", "A<cr>", "  |", Insert},
		{"enter on empty number ends list", "4. ", "A<cr>", "|", Insert},
		{"enter twice ends list", "- a", "A<cr><cr>x", "- a\nx|", Insert},
		{"o continues list", "- [ ] a", "o", "- [ ] a\n- [ ] |", Insert},
		{"o increments", "2) a", "ob<esc>", "2) a\n3) |b", Normal},
		{"O does not continue", "x\n- a", "jO", "x\n|\n- a", Insert},
		{"o on empty item keeps indent", "  - ", "o", "  - \n  |", Insert},
		{"space toggles task", "- [ ] |a", "<space>", "- [x] |a", Normal},
		{"space unchecks", "- [x] a|b", "<space>", "- [ ] a|b", Normal},
		{"space converts plain line", "fo|o", "<space>", "- [ ] fo|o", Normal},
		{"space converts bullet", "- fo|o", "<space>", "- [ ] fo|o", Normal},
		{"space keeps indent", "  |foo", "<space>", "  - [ ] |foo", Normal},
		{"space on blank line", "|", "<space>", "- [ ]| ", Normal},
		{"space cursor in indent stays", " | foo", "<space>", " | - [ ] foo", Normal},
		{"space dot repeat", "|a\nb", "<space>j.", "- [ ] a\n- [ ] |b", Normal},
		{"insert c-t toggles", "- [ ] a", "A<c-t>", "- [x] a|", Insert},
		{"insert c-t on blank", "", "i<c-t>x", "- [ ] x|", Insert},
		{"insert tab indents", "- a", "A<tab>", "  - a|", Insert},
		{"insert tab tabbed line", "\t- a", "A<tab>", "\t\t- a|", Insert},
		{"insert shift-tab outdents", "    - a", "A<s-tab>", "  - a|", Insert},
		{"insert shift-tab cursor in indent", " |- a", "i<s-tab>", "|- a", Insert},
		{"subtask flow", "- [ ] a", "A<cr><tab>b<esc>", "- [ ] a\n  - [ ] |b", Normal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, b, _ := run(t, tt.in, tt.keys)
			if got := show(b); got != tt.want {
				t.Errorf("%q + %q: got %q, want %q", tt.in, tt.keys, got, tt.want)
			}
			if m.Mode() != tt.mode {
				t.Errorf("mode = %v, want %v", m.Mode(), tt.mode)
			}
		})
	}
}

func TestToggleTaskEffectAndUndo(t *testing.T) {
	m, b, eff := run(t, "- [ ] a|b", "<space>")
	if !eff.ToggleTask {
		t.Error("ToggleTask not set")
	}
	if b.String() != "- [x] ab" {
		t.Fatalf("text %q", b.String())
	}
	feed(m, b, "u")
	if got := show(b); got != "- [ ] a|b" {
		t.Errorf("after undo %q", got)
	}
	_, _, eff = run(t, "- [ ] a", "A<c-t>")
	if !eff.ToggleTask {
		t.Error("insert c-t: ToggleTask not set")
	}
	m, b, _ = run(t, "|a", "<space><space>")
	feed(m, b, "u")
	if got := b.String(); got != "- [ ] a" {
		t.Errorf("each toggle should be one undo step, got %q", got)
	}
}

func TestPlainLists(t *testing.T) {
	tests := []struct {
		name, in, keys, want string
		toggle               bool
	}{
		{"enter continues", "- [ ] a|", "<cr>b", "- [ ] a\n- [ ] b|", false},
		{"enter increments", "1. a|", "<cr>", "1. a\n2. |", false},
		{"enter ends list", "- a\n- |", "<cr>", "- a\n|", false},
		{"c-t toggles", "- [ ] a|", "<c-t>", "- [x] a|", true},
		{"c-t converts", "a|", "<c-t>", "- [ ] a|", true},
		{"c-t undo", "a|", "<c-t><c-z>", "|a", true},
		{"tab then type", "- a|", "<tab>b", "  - ab|", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, b, eff := runPlain(t, tt.in, tt.keys)
			if got := show(b); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if eff.ToggleTask != tt.toggle {
				t.Errorf("ToggleTask = %v", eff.ToggleTask)
			}
		})
	}
}

func TestToggleReadOnly(t *testing.T) {
	m := New()
	m.SetReadOnly(true)
	b := newBuf("|a")
	eff := feed(m, b, "<space>")
	if !eff.Blocked || eff.ToggleTask || b.String() != "a" {
		t.Errorf("read-only toggle: %+v %q", eff, b.String())
	}
	p := NewPlain()
	p.SetReadOnly(true)
	eff = feed(p, b, "<c-t>")
	if !eff.Blocked || b.String() != "a" {
		t.Errorf("plain read-only toggle: %+v %q", eff, b.String())
	}
}
