package mdstyle

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
)

// sp is a compact span description used in expectations: text covered and kind.
type sp struct {
	text string
	kind Kind
}

func describe(line string, spans []Span) []sp {
	out := make([]sp, 0, len(spans))
	for _, s := range spans {
		out = append(out, sp{line[s.Start:s.End], s.Kind})
	}
	return out
}

func fmtSpans(s []sp) string {
	parts := make([]string, len(s))
	for i, x := range s {
		parts[i] = fmt.Sprintf("%s(%q)", x.kind, x.text)
	}
	return strings.Join(parts, " ")
}

func TestTokenizeLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []sp
	}{
		{"empty", "", nil},
		{"plain", "just text", nil},
		{"h1", "# Title", []sp{{"# ", Markup}, {"Title", H1}}},
		{"h2", "## Sub", []sp{{"## ", Markup}, {"Sub", H2}}},
		{"h3", "### Three", []sp{{"### ", Markup}, {"Three", H3}}},
		{"h4", "#### Four", []sp{{"#### ", Markup}, {"Four", H4}}},
		{"h5", "##### Five", []sp{{"##### ", Markup}, {"Five", H5}}},
		{"h6", "###### Six", []sp{{"###### ", Markup}, {"Six", H6}}},
		{"seven hashes is not heading", "####### no", nil},
		{"bare heading marker", "#", []sp{{"#", Markup}}},
		{"heading with tag", "# Meeting #work", []sp{{"# ", Markup}, {"Meeting ", H1}, {"#work", Tag}}},
		{"heading tag in middle", "## a #t b", []sp{{"## ", Markup}, {"a ", H2}, {"#t", Tag}, {" b", H2}}},
		{"heading bold keeps heading kind", "# a **b**", []sp{{"# ", Markup}, {"a ", H1}, {"**", Markup}, {"b", H1}, {"**", Markup}}},
		{"tag at line start is not heading", "#todo later", []sp{{"#todo", Tag}}},
		{"bold star", "a **b** c", []sp{{"**", Markup}, {"b", Bold}, {"**", Markup}}},
		{"bold underscore", "__b__", []sp{{"__", Markup}, {"b", Bold}, {"__", Markup}}},
		{"italic star", "*i*", []sp{{"*", Markup}, {"i", Italic}, {"*", Markup}}},
		{"italic underscore", "x _i_ y", []sp{{"_", Markup}, {"i", Italic}, {"_", Markup}}},
		{"snake_case is not italic", "snake_case_word", nil},
		{"bold italic", "***bi***", []sp{{"***", Markup}, {"bi", BoldItalic}, {"***", Markup}}},
		{"strike", "~~s~~", []sp{{"~~", Markup}, {"s", Strike}, {"~~", Markup}}},
		{"italic inside bold", "**a *b* c**", []sp{{"**", Markup}, {"a ", Bold}, {"*", Markup}, {"b", Italic}, {"*", Markup}, {" c", Bold}, {"**", Markup}}},
		{"bold inside italic sharing opener", "***a** b*", []sp{{"*", Markup}, {"**", Markup}, {"a", Bold}, {"**", Markup}, {" b", Italic}, {"*", Markup}}},
		{"bold closing with italic", "**a *b***", []sp{{"**", Markup}, {"a ", Bold}, {"*", Markup}, {"b", Italic}, {"*", Markup}, {"**", Markup}}},
		{"longer closer: first n chars close", "**bold***", []sp{{"**", Markup}, {"bold", Bold}, {"**", Markup}}},
		{"longer closer for italic", "*it** x", []sp{{"*", Markup}, {"it", Italic}, {"*", Markup}}},
		{"unclosed bold", "**nope", nil},
		{"spaced star not emphasis", "a * b * c", nil},
		{"code", "use `x := 1` here", []sp{{"`", Markup}, {"x := 1", Code}, {"`", Markup}}},
		{"double backtick code", "``a`b``", []sp{{"``", Markup}, {"a`b", Code}, {"``", Markup}}},
		{"no emphasis inside code", "`*a*`", []sp{{"`", Markup}, {"*a*", Code}, {"`", Markup}}},
		{"no tag inside code", "`#a` #b", []sp{{"`", Markup}, {"#a", Code}, {"`", Markup}, {"#b", Tag}}},
		{"link", "see [docs](http://x.io/#a) now", []sp{{"[", Markup}, {"docs", Link}, {"](", Markup}, {"http://x.io/#a", LinkURL}, {")", Markup}}},
		{"link with parens in url", "[w](a(b)c)", []sp{{"[", Markup}, {"w", Link}, {"](", Markup}, {"a(b)c", LinkURL}, {")", Markup}}},
		{"code span in link text", "[a `]` b](u)", []sp{{"[", Markup}, {"a `]` b", Link}, {"](", Markup}, {"u", LinkURL}, {")", Markup}}},
		{"brackets without url", "[not a link]", nil},
		{"image", "![alt](img/a.png)", []sp{{"![alt](img/a.png)", Image}}},
		{"image in text", "x ![a](b.png) y", []sp{{"![a](b.png)", Image}}},
		{"list dash", "- item", []sp{{"- ", ListMarker}}},
		{"list star indented", "  * item **b**", []sp{{"* ", ListMarker}, {"**", Markup}, {"b", Bold}, {"**", Markup}}},
		{"list plus", "+ x", []sp{{"+ ", ListMarker}}},
		{"list numbered", "12. item", []sp{{"12. ", ListMarker}}},
		{"dash without space not list", "-item", nil},
		{"task open", "- [ ] buy milk #home", []sp{{"- ", ListMarker}, {"[ ]", TaskOpen}, {"#home", Tag}}},
		{"task done", "- [x] done thing", []sp{{"- ", ListMarker}, {"[x]", TaskDone}, {"done thing", TaskDoneText}}},
		{"task done upper", "  - [X] ok", []sp{{"- ", ListMarker}, {"[X]", TaskDone}, {"ok", TaskDoneText}}},
		{"task done empty", "- [x]", []sp{{"- ", ListMarker}, {"[x]", TaskDone}}},
		{"quote", "> wise words", []sp{{"> ", Markup}, {"wise words", Quote}}},
		{"quote with bold", "> a **b**", []sp{{"> ", Markup}, {"a ", Quote}, {"**", Markup}, {"b", Bold}, {"**", Markup}}},
		{"rule dashes", "---", []sp{{"---", Rule}}},
		{"rule stars", "***", []sp{{"***", Rule}}},
		{"rule underscores", "___", []sp{{"___", Rule}}},
		{"rule spaced", "- - -", []sp{{"- - -", Rule}}},
		{"two dashes not rule", "--", nil},
		{"tag url fragment", "http://x.com/#frag", nil},
		{"escaped star", `\*not\*`, nil},
		{"no tag directly after paren", "(#a)", nil},
		{"anchor link has no tag", "[setup](#setup)", []sp{{"[", Markup}, {"setup", Link}, {"](", Markup}, {"#setup", LinkURL}, {")", Markup}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans, out := TokenizeLine(tt.line, State{})
			if out != (State{}) {
				t.Errorf("unexpected out state %+v", out)
			}
			got := describe(tt.line, spans)
			if fmtSpans(got) != fmtSpans(tt.want) {
				t.Errorf("TokenizeLine(%q)\n got: %s\nwant: %s", tt.line, fmtSpans(got), fmtSpans(tt.want))
			}
			checkSpans(t, tt.line, spans)
		})
	}
}

func checkSpans(t *testing.T, line string, spans []Span) {
	t.Helper()
	prev := 0
	for i, s := range spans {
		if s.Start < prev || s.End <= s.Start || s.End > len(line) {
			t.Fatalf("line %q: bad span %d %+v (prev end %d)", line, i, s, prev)
		}
		prev = s.End
	}
}

func TestFenceAcrossLines(t *testing.T) {
	lines := []string{
		"text",
		"```go",
		"func main() {}",
		"# not a heading",
		"```",
		"# heading",
	}
	st := State{}
	var all [][]Span
	var states []State
	for _, l := range lines {
		var spans []Span
		spans, st = TokenizeLine(l, st)
		all = append(all, spans)
		states = append(states, st)
	}
	if want := (State{InFence: true, FenceLang: "go", FenceMarker: "```"}); states[1] != want {
		t.Fatalf("after opener state = %+v, want %+v", states[1], want)
	}
	if got := describe(lines[1], all[1]); fmtSpans(got) != fmtSpans([]sp{{"```go", CodeFence}}) {
		t.Errorf("opener spans = %s", fmtSpans(got))
	}
	if all[1][0].Lang != "go" {
		t.Errorf("opener Lang = %q", all[1][0].Lang)
	}
	for _, i := range []int{2, 3} {
		if !states[i].InFence {
			t.Errorf("line %d: state should still be in fence", i)
		}
		for _, s := range all[i] {
			if s.Kind != CodeBlock || s.Lang != "go" {
				t.Errorf("line %d: span %+v should be CodeBlock/go", i, s)
			}
		}
		checkSpans(t, lines[i], all[i])
	}
	if got := describe(lines[4], all[4]); fmtSpans(got) != fmtSpans([]sp{{"```", CodeFence}}) {
		t.Errorf("closer spans = %s", fmtSpans(got))
	}
	if states[4] != (State{}) {
		t.Errorf("after closer state = %+v", states[4])
	}
	if got := describe(lines[5], all[5]); fmtSpans(got) != fmtSpans([]sp{{"# ", Markup}, {"heading", H1}}) {
		t.Errorf("after fence spans = %s", fmtSpans(got))
	}
}

func TestFenceCloserRules(t *testing.T) {
	in := State{InFence: true, FenceMarker: "````", FenceLang: ""}
	if _, out := TokenizeLine("```", in); !out.InFence {
		t.Error("shorter closer must not close fence")
	}
	if _, out := TokenizeLine("~~~~", in); !out.InFence {
		t.Error("different marker char must not close fence")
	}
	if _, out := TokenizeLine("`````", in); out.InFence {
		t.Error("longer closer must close fence")
	}
	tilde := State{InFence: true, FenceMarker: "~~~"}
	if _, out := TokenizeLine("~~~", tilde); out.InFence {
		t.Error("tilde closer must close tilde fence")
	}
}

func TestHighlightCodeGoKeyword(t *testing.T) {
	line := "func main() { return }"
	spans := HighlightCode("go", line)
	checkSpans(t, line, spans)
	found := false
	for _, s := range spans {
		if s.Kind != CodeBlock {
			t.Errorf("span %+v kind = %s, want CodeBlock", s, s.Kind)
		}
		if line[s.Start:s.End] == "func" {
			found = true
			if s.Token == 0 || s.Token.Category() != chroma.Keyword {
				t.Errorf("func token = %v, want a keyword", s.Token)
			}
		}
	}
	if !found {
		t.Fatalf("no span for 'func' in %+v", spans)
	}
}

func TestHighlightCodeFallback(t *testing.T) {
	for _, lang := range []string{"", "no-such-language-xyz"} {
		line := "some code"
		spans := HighlightCode(lang, line)
		checkSpans(t, line, spans)
		if len(spans) == 0 {
			t.Fatalf("lang %q: expected spans", lang)
		}
		covered := 0
		for _, s := range spans {
			covered += s.End - s.Start
		}
		if covered != len(line) {
			t.Errorf("lang %q: covered %d of %d bytes", lang, covered, len(line))
		}
	}
	// Invalid UTF-8 may be rewritten by chroma; the result must still map
	// onto the original bytes.
	bad := "x := \"\xff\xfe\" // \xc3"
	spans := HighlightCode("go", bad)
	checkSpans(t, bad, spans)
	if len(spans) != 1 || spans[0].Start != 0 || spans[0].End != len(bad) || spans[0].Token != chroma.Text {
		t.Errorf("invalid UTF-8 spans = %+v, want one Text span over the line", spans)
	}
	if spans := HighlightCode("go", ""); spans != nil {
		t.Errorf("empty line spans = %+v", spans)
	}
}

func TestCacheIncremental(t *testing.T) {
	doc := []string{
		"# Title",
		"",
		"para one",
		"para two",
		"para three",
		"",
		"- [ ] task",
		"end",
	}

	t.Run("initial computes state only, spans lazily and once", func(t *testing.T) {
		c := NewCache()
		c.Update(doc, 0)
		if c.stateSteps != len(doc) || c.tokenizeCalls != 0 {
			t.Fatalf("after Update: stateSteps = %d, tokenizeCalls = %d; want %d, 0", c.stateSteps, c.tokenizeCalls, len(doc))
		}
		if got := describe(doc[0], c.Spans(0)); fmtSpans(got) != fmtSpans([]sp{{"# ", Markup}, {"Title", H1}}) {
			t.Errorf("spans(0) = %s", fmtSpans(got))
		}
		touchAll(c, len(doc))
		touchAll(c, len(doc))
		if c.tokenizeCalls != len(doc) {
			t.Fatalf("tokenizeCalls = %d, want %d (memoized)", c.tokenizeCalls, len(doc))
		}
		if c.Spans(-1) != nil || c.Spans(len(doc)) != nil {
			t.Error("out-of-range Spans should be nil")
		}
	})

	t.Run("paragraph edit re-tokenizes one line", func(t *testing.T) {
		c := NewCache()
		c.Update(doc, 0)
		touchAll(c, len(doc))
		resetCounters(c)
		edited := append([]string(nil), doc...)
		edited[3] = "para **two**"
		c.Update(edited, 3)
		touchAll(c, len(edited))
		if c.stateSteps != 1 || c.tokenizeCalls != 1 {
			t.Fatalf("stateSteps = %d, tokenizeCalls = %d; want 1, 1", c.stateSteps, c.tokenizeCalls)
		}
		assertCacheMatchesFresh(t, c, edited)
	})

	t.Run("fence opener steps state to end, highlights lazily", func(t *testing.T) {
		c := NewCache()
		c.Update(doc, 0)
		touchAll(c, len(doc))
		resetCounters(c)
		edited := append([]string(nil), doc...)
		edited[2] = "```"
		c.Update(edited, 2)
		if want := len(doc) - 2; c.stateSteps != want {
			t.Fatalf("stateSteps = %d, want %d", c.stateSteps, want)
		}
		if c.tokenizeCalls != 0 || c.highlightCalls != 0 {
			t.Fatalf("Update computed spans: tokenizeCalls = %d, highlightCalls = %d", c.tokenizeCalls, c.highlightCalls)
		}
		c.Spans(5)
		if c.highlightCalls != 1 {
			t.Fatalf("highlightCalls after one access = %d, want 1", c.highlightCalls)
		}
		touchAll(c, len(edited))
		if want := len(doc) - 3; c.highlightCalls != want {
			t.Fatalf("highlightCalls = %d, want %d", c.highlightCalls, want)
		}
		if want := len(doc) - 2; c.tokenizeCalls != want {
			t.Fatalf("tokenizeCalls = %d, want %d", c.tokenizeCalls, want)
		}
		for i := 3; i < len(edited); i++ {
			for _, s := range c.Spans(i) {
				if s.Kind != CodeBlock {
					t.Errorf("line %d span %+v should be CodeBlock", i, s)
				}
			}
		}
		assertCacheMatchesFresh(t, c, edited)

		// Closing the fence again stabilizes after the closer.
		resetCounters(c)
		closed := append([]string(nil), edited...)
		closed[4] = "```"
		c.Update(closed, 4)
		if want := len(doc) - 4; c.stateSteps != want {
			t.Fatalf("after close: stateSteps = %d, want %d", c.stateSteps, want)
		}
		assertCacheMatchesFresh(t, c, closed)
	})

	t.Run("insert line in middle", func(t *testing.T) {
		c := NewCache()
		c.Update(doc, 0)
		touchAll(c, len(doc))
		resetCounters(c)
		inserted := append([]string(nil), doc[:3]...)
		inserted = append(inserted, "new **line**")
		inserted = append(inserted, doc[3:]...)
		c.Update(inserted, 3)
		touchAll(c, len(inserted))
		if c.stateSteps != 1 || c.tokenizeCalls != 1 {
			t.Fatalf("stateSteps = %d, tokenizeCalls = %d; want 1, 1", c.stateSteps, c.tokenizeCalls)
		}
		assertCacheMatchesFresh(t, c, inserted)
	})

	t.Run("delete line in middle", func(t *testing.T) {
		c := NewCache()
		c.Update(doc, 0)
		touchAll(c, len(doc))
		resetCounters(c)
		deleted := append([]string(nil), doc[:3]...)
		deleted = append(deleted, doc[4:]...)
		c.Update(deleted, 3)
		touchAll(c, len(deleted))
		if c.stateSteps > 1 || c.tokenizeCalls > 1 {
			t.Fatalf("stateSteps = %d, tokenizeCalls = %d; want <= 1", c.stateSteps, c.tokenizeCalls)
		}
		assertCacheMatchesFresh(t, c, deleted)
	})

	t.Run("join lines", func(t *testing.T) {
		c := NewCache()
		c.Update(doc, 0)
		joined := append([]string(nil), doc[:2]...)
		joined = append(joined, "para onepara two")
		joined = append(joined, doc[4:]...)
		c.Update(joined, 2)
		assertCacheMatchesFresh(t, c, joined)
	})

	t.Run("delete fence opener", func(t *testing.T) {
		src := []string{"a", "```", "# x", "```", "# y"}
		c := NewCache()
		c.Update(src, 0)
		after := []string{"a", "# x", "```", "# y"}
		c.Update(after, 1)
		assertCacheMatchesFresh(t, c, after)
	})

	t.Run("truncate to empty", func(t *testing.T) {
		c := NewCache()
		c.Update(doc, 0)
		c.Update(nil, 0)
		if c.Spans(0) != nil {
			t.Error("expected no spans after truncation")
		}
	})

	t.Run("firstChanged beyond cache", func(t *testing.T) {
		c := NewCache()
		c.Update(doc[:2], 0)
		c.Update(doc, 99)
		assertCacheMatchesFresh(t, c, doc)
	})
}

func assertCacheMatchesFresh(t *testing.T, c *Cache, lines []string) {
	t.Helper()
	fresh := NewCache()
	fresh.Update(lines, 0)
	for i := range lines {
		got := fmtSpans(describe(lines[i], c.Spans(i)))
		want := fmtSpans(describe(lines[i], fresh.Spans(i)))
		if got != want {
			t.Errorf("line %d %q:\n got: %s\nwant: %s", i, lines[i], got, want)
		}
	}
	if c.Spans(len(lines)) != nil {
		t.Errorf("stale spans beyond line count")
	}
}

func TestSpansSortedNonOverlapping(t *testing.T) {
	doc := `# Notty #project/notes
Some **bold**, *italic*, ***both***, ~~gone~~ and ` + "`code`" + ` text.
A [link](https://example.com/a_(b)#frag) and ![img](a b.png) and #tag.
> quote with _emph_ and #tag
- [ ] open task **important** #todo
- [x] done task *meh*
  1. numbered [x](y)
---
***
` + "```go" + `
package main // #not-a-tag
func main() { fmt.Println("hi **there**") }
` + "```" + `
~~~
plain ~~~ text
~~~
**unclosed *mixed _stuff
` + "`unclosed code" + `
[broken](link
![broken
\*escaped\* \` + "`" + `x` + "`" + `
__a__b__ *a**b* **a*b** ***a** b* _a_b_ a_b_ _ _ ** **
#ünïcödé ## ###### #1 a#b
`
	st := State{}
	for i, line := range strings.Split(doc, "\n") {
		var spans []Span
		spans, st = TokenizeLine(line, st)
		t.Run(fmt.Sprintf("line%d", i), func(t *testing.T) {
			checkSpans(t, line, spans)
			for _, s := range spans {
				if s.Kind == Text {
					t.Errorf("Text spans should not be emitted: %+v", s)
				}
			}
		})
	}
}

func TestSpansRandomLines(t *testing.T) {
	alphabet := []string{"*", "_", "~", "`", "[", "]", "(", ")", "!", "#", " ", "\\", "-", ">", "a", "é", "1", ".", "x", "/", "\t"}
	rng := rand.New(rand.NewPCG(1, 2))
	st := State{}
	for i := 0; i < 5000; i++ {
		var b strings.Builder
		for n := rng.IntN(24); n > 0; n-- {
			b.WriteString(alphabet[rng.IntN(len(alphabet))])
		}
		line := b.String()
		var spans []Span
		spans, st = TokenizeLine(line, st)
		checkSpans(t, line, spans)
	}
}

func TestKindString(t *testing.T) {
	if H1.String() != "H1" || TaskDoneText.String() != "TaskDoneText" || Kind(999).String() != "Kind(999)" {
		t.Errorf("unexpected Kind strings: %s %s %s", H1, TaskDoneText, Kind(999))
	}
}
