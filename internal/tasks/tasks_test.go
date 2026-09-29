package tasks

import (
	"strings"
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		ok     bool
		indent int
		done   bool
		text   string
	}{
		{"dash unchecked", "- [ ] x", true, 0, false, "x"},
		{"star checked lower", "* [x] y", true, 0, true, "y"},
		{"plus checked upper", "+ [X] z", true, 0, true, "z"},
		{"numbered", "1. [ ] n", true, 0, false, "n"},
		{"indented spaces", "  - [ ] indented", true, 2, false, "indented"},
		{"indented tabs", "\t- [x] tabbed", true, 1, true, "tabbed"},
		{"no space between marker and bracket", "-[ ]x", false, 0, false, ""},
		{"empty text no trailing space", "- [ ]", true, 0, false, ""},
		{"empty text trailing space", "- [ ] ", true, 0, false, ""},
		{"not a task plain text", "just text", false, 0, false, ""},
		{"not a task heading", "## heading", false, 0, false, ""},
		{"numbered multi digit", "23. [x] done item", true, 0, true, "done item"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task, ok := ParseLine(c.line)
			if ok != c.ok {
				t.Fatalf("ParseLine(%q) ok = %v, want %v", c.line, ok, c.ok)
			}
			if !ok {
				return
			}
			if task.Indent != c.indent {
				t.Errorf("Indent = %d, want %d", task.Indent, c.indent)
			}
			if task.Done != c.done {
				t.Errorf("Done = %v, want %v", task.Done, c.done)
			}
			if task.Text != c.text {
				t.Errorf("Text = %q, want %q", task.Text, c.text)
			}
			if task.Raw != c.line {
				t.Errorf("Raw = %q, want %q", task.Raw, c.line)
			}
		})
	}
}

func TestToggleLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		out  string
	}{
		{"unchecked to checked", "- [ ] x", "- [x] x"},
		{"checked to unchecked", "- [x] x", "- [ ] x"},
		{"uppercase X normalizes to unchecked", "- [X] x", "- [ ] x"},
		{"indented task toggled", "  - [ ] sub", "  - [x] sub"},
		{"indented task toggled back", "  - [x] sub", "  - [ ] sub"},
		{"numbered list item without checkbox", "1. buy milk", "1. [ ] buy milk"},
		{"bullet item without checkbox", "- buy milk", "- [ ] buy milk"},
		{"plain line with indent", "  hello world", "  - [ ] hello world"},
		{"plain line no indent", "hello", "- [ ] hello"},
		{"blank line", "", "- [ ] "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ToggleLine(c.in)
			if got != c.out {
				t.Errorf("ToggleLine(%q) = %q, want %q", c.in, got, c.out)
			}
		})
	}
}

func TestDueDate(t *testing.T) {
	cases := []struct {
		name string
		text string
		want *time.Time
	}{
		{"valid date", "call mom @2026-10-05", dateOrNil(true, 2026, 10, 5)},
		{"no date", "no date here", nil},
		{"invalid month", "bad @2026-13-40", nil},
		{"multiple, first invalid second valid", "bad @2026-13-40 then good @2026-01-02", dateOrNil(true, 2026, 1, 2)},
		{"multiple valid takes first", "first @2026-01-01 second @2026-02-02", dateOrNil(true, 2026, 1, 1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DueDate(c.text)
			if (got == nil) != (c.want == nil) {
				t.Fatalf("DueDate(%q) = %v, want %v", c.text, got, c.want)
			}
			if got != nil && !got.Equal(*c.want) {
				t.Errorf("DueDate(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func dateOrNil(ok bool, y, m, d int) *time.Time {
	if !ok {
		return nil
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Local)
	return &t
}

func TestParseSkipsFencesAndRecordsLineNumbers(t *testing.T) {
	content := strings.Join([]string{
		"- [ ] before fence", // 0
		"```",                // 1
		"- [ ] inside fence", // 2
		"```",                // 3
		"- [x] after fence",  // 4
		"~~~",                // 5
		"- [ ] inside tilde", // 6
		"~~~",                // 7
		"- [ ] after tilde",  // 8
	}, "\n")

	got := Parse(content)
	wantLines := []int{0, 4, 8}
	if len(got) != len(wantLines) {
		t.Fatalf("Parse() returned %d tasks, want %d: %+v", len(got), len(wantLines), got)
	}
	for i, task := range got {
		if task.Line != wantLines[i] {
			t.Errorf("task[%d].Line = %d, want %d", i, task.Line, wantLines[i])
		}
	}
}

func TestProgressNestedHeadings(t *testing.T) {
	lines := []string{
		"# Top",      // 0
		"- [x] a",    // 1
		"## Sub",     // 2
		"- [ ] b",    // 3
		"- [x] c",    // 4
		"### Subsub", // 5
		"- [x] d",    // 6
		"## Sub2",    // 7
		"- [ ] e",    // 8
		"# Top2",     // 9
		"- [x] f",    // 10
	}

	done, total := Progress(lines, 0)
	if done != 3 || total != 5 {
		t.Errorf("Progress(top) = %d/%d, want 3/5", done, total)
	}

	done, total = Progress(lines, 2)
	if done != 2 || total != 3 {
		t.Errorf("Progress(sub) = %d/%d, want 2/3", done, total)
	}

	done, total = Progress(lines, 5)
	if done != 1 || total != 1 {
		t.Errorf("Progress(subsub) = %d/%d, want 1/1", done, total)
	}
}

func TestFindLine(t *testing.T) {
	lines := []string{
		"- [ ] alpha",
		"- [ ] beta",
		"- [x] gamma",
		"- [ ] beta",
	}

	if line, ok := FindLine(lines, 1, "- [ ] beta"); !ok || line != 1 {
		t.Errorf("exact match FindLine = %d,%v want 1,true", line, ok)
	}

	// drifted: text now at a different line
	if line, ok := FindLine(lines, 0, "- [x] gamma"); !ok || line != 2 {
		t.Errorf("drifted FindLine = %d,%v want 2,true", line, ok)
	}

	// nearest wins among duplicate Raw matches
	if line, ok := FindLine(lines, 3, "- [ ] beta"); !ok || line != 3 {
		t.Errorf("nearest FindLine = %d,%v want 3,true", line, ok)
	}
	if line, ok := FindLine(lines, 0, "- [ ] beta"); !ok || line != 1 {
		t.Errorf("nearest FindLine(from 0) = %d,%v want 1,true", line, ok)
	}

	if _, ok := FindLine(lines, 0, "- [ ] missing"); ok {
		t.Errorf("missing FindLine should not be found")
	}

	// trimmed equal at the given line
	if line, ok := FindLine([]string{"  - [ ] beta  "}, 0, "- [ ] beta"); !ok || line != 0 {
		t.Errorf("trimmed-equal FindLine = %d,%v want 0,true", line, ok)
	}
}
