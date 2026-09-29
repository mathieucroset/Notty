package tags

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"empty", "", nil},
		{"no tags", "just some text\nand more", nil},
		{"single", "hello #work", []string{"work"}},
		{"start of line", "#work is here", []string{"work"}},
		{"space inside paren", "(see #work)", []string{"work"}},
		{"directly after paren", "(#work)", nil},
		{"anchor link", "[setup](#setup)", nil},
		{"nested", "#work/client stuff", []string{"work/client"}},
		{"trailing slash dropped", "#work/ stuff", []string{"work"}},
		{"heading line counts", "# Meeting #work", []string{"work"}},
		{"heading marker not a tag", "## Title\n### Sub", nil},
		{"heading without space not heading marker", "#Title", []string{"Title"}},
		{"unicode", "voir #réunion et #日本", []string{"réunion", "日本"}},
		{"digits first rejected", "issue #123", nil},
		{"digits allowed after letter", "#v2 #a1_b-c", []string{"v2", "a1_b-c"}},
		{"trailing period", "done #tag.", []string{"tag"}},
		{"trailing comma", "#tag, other", []string{"tag"}},
		{"url fragment", "see http://x.com/#frag", nil},
		{"preceded by non-space", "a#b", nil},
		{"double hash", "##tag", nil},
		{"inline code", "use `#notatag` but #real", []string{"real"}},
		{"double backtick code", "``a ` #nope`` #yes", []string{"yes"}},
		{"unclosed backtick is literal", "a ` #yes", []string{"yes"}},
		{"backtick fence", "#before\n```go\n#inside\n```\n#after", []string{"before", "after"}},
		{"tilde fence", "~~~\n#inside\n~~~\n#after", []string{"after"}},
		{"fence closer must match char", "```\n~~~\n#inside\n```\n#after", []string{"after"}},
		{"fence closer length", "````\n```\n#inside\n````\n#after", []string{"after"}},
		{"indented fence up to 3 spaces", "   ```\n#inside\n   ```\n#after", []string{"after"}},
		{"unclosed fence", "```\n#inside", nil},
		{"dedupe case-insensitive keeps first", "#Work #work #WORK #other", []string{"Work", "other"}},
		{"first-seen order", "#b\n#a\n#b", []string{"b", "a"}},
		{"crlf", "#one\r\n#two\r\n", []string{"one", "two"}},
		{"tab before", "x\t#tag", []string{"tag"}},
		{"lone hash", "# ", nil},
		{"hash then punctuation", "#!nope #-nope", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.content)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q) = %#v, want %#v", tt.content, got, tt.want)
			}
		})
	}
}

func TestCodeSpanScanIsBounded(t *testing.T) {
	// A code span whose closer lies beyond MaxScan bytes is not recognized,
	// which keeps FindTags linear on huge lines.
	long := "`" + strings.Repeat("a ", MaxScan) + "#x`"
	if got := FindTags(long); len(got) != 1 {
		t.Errorf("FindTags(long unclosed-within-bound span) = %v, want one tag", got)
	}
	short := "`" + strings.Repeat("a ", 10) + "#x`"
	if got := FindTags(short); got != nil {
		t.Errorf("FindTags(short span) = %v, want nil", got)
	}
}

func BenchmarkFindTagsPathological(b *testing.B) {
	// Backtick runs of increasing length never find a closer.
	var sb strings.Builder
	for n := 1; sb.Len() < 1<<20; n++ {
		sb.WriteString(strings.Repeat("`", n%500+1))
		sb.WriteString(" #t ")
	}
	line := sb.String()
	b.ResetTimer()
	for b.Loop() {
		FindTags(line)
	}
}

func TestFenceOpen(t *testing.T) {
	tests := []struct {
		line, marker, lang string
		ok                 bool
	}{
		{"```", "```", "", true},
		{"```go", "```", "go", true},
		{"~~~~ python extra", "~~~~", "python", true},
		{"   ```js", "```", "js", true},
		{"    ```", "", "", false},
		{"``", "", "", false},
		{"```a`b", "", "", false},
		{"~~~a`b", "~~~", "a`b", true},
		{"text", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			m, l, ok := FenceOpen(tt.line)
			if m != tt.marker || l != tt.lang || ok != tt.ok {
				t.Errorf("FenceOpen(%q) = %q, %q, %v; want %q, %q, %v", tt.line, m, l, ok, tt.marker, tt.lang, tt.ok)
			}
		})
	}
}

func TestIsFenceClose(t *testing.T) {
	tests := []struct {
		line, marker string
		want         bool
	}{
		{"```", "```", true},
		{"````", "```", true},
		{"```", "````", false},
		{"~~~", "```", false},
		{"``` ", "```", true},
		{"```go", "```", false},
		{"   ```", "```", true},
		{"    ```", "```", false},
		{"```", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.line+"|"+tt.marker, func(t *testing.T) {
			if got := IsFenceClose(tt.line, tt.marker); got != tt.want {
				t.Errorf("IsFenceClose(%q, %q) = %v, want %v", tt.line, tt.marker, got, tt.want)
			}
		})
	}
}

func TestFindTags(t *testing.T) {
	tests := []struct {
		name string
		line string
		want [][2]int
	}{
		{"none", "plain text", nil},
		{"single", "hi #tag", [][2]int{{3, 7}}},
		{"heading", "# Meeting #work", [][2]int{{10, 15}}},
		{"multiple", "#a and #b/c", [][2]int{{0, 2}, {7, 11}}},
		{"trailing punct", "#tag.", [][2]int{{0, 4}}},
		{"trailing slash", "#a/b/", [][2]int{{0, 4}}},
		{"unicode byte range", "#été", [][2]int{{0, 6}}},
		{"in code span", "`#x` #y", [][2]int{{5, 7}}},
		{"url fragment", "http://x.com/#frag", nil},
		{"paren", "(#x)", nil},
		{"anchor link", "see [setup](#setup) #t", [][2]int{{20, 22}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindTags(tt.line)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindTags(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}
