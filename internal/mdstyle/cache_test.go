package mdstyle

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

func resetCounters(c *Cache) { c.stateSteps, c.tokenizeCalls, c.highlightCalls = 0, 0, 0 }

func touchAll(c *Cache, n int) {
	for i := range n {
		c.Spans(i)
	}
}

// fencedDoc returns a document whose first line opens a fence with lang,
// followed by n lines of Go code and no closer.
func fencedDoc(lang string, n int) []string {
	lines := make([]string, 0, n+1)
	lines = append(lines, "```"+lang)
	for i := range n {
		lines = append(lines, fmt.Sprintf("func f%d() int { return %d } // comment", i, i))
	}
	return lines
}

func TestCacheFenceLangToggleIsLazy(t *testing.T) {
	const n = 10000
	goDoc, pyDoc := fencedDoc("go", n), fencedDoc("python", n)
	c := NewCache()
	c.Update(goDoc, 0)
	touchAll(c, 50)
	resetCounters(c)
	c.Update(pyDoc, 0)
	if c.stateSteps != n+1 {
		t.Errorf("stateSteps = %d, want %d", c.stateSteps, n+1)
	}
	if c.tokenizeCalls != 0 || c.highlightCalls != 0 {
		t.Errorf("Update computed spans: tokenizeCalls = %d, highlightCalls = %d", c.tokenizeCalls, c.highlightCalls)
	}
	touchAll(c, 50)
	if c.highlightCalls != 49 {
		t.Errorf("highlightCalls = %d, want 49", c.highlightCalls)
	}
	if s := c.Spans(10); len(s) == 0 || s[0].Lang != "python" {
		t.Errorf("spans after toggle = %+v, want python code spans", s)
	}
}

// fullTokenize tokenizes lines from scratch, independently of Cache.
func fullTokenize(lines []string) [][]Span {
	out := make([][]Span, len(lines))
	st := State{}
	for i, l := range lines {
		out[i], st = TokenizeLine(l, st)
	}
	return out
}

func TestCacheMatchesFullRetokenizeRandomEdits(t *testing.T) {
	pool := []string{
		"", "```", "```go", "```python", "~~~", "````", "   ```", "# heading #tag",
		"para *it* and **bold**", "- [ ] task", "- [x] done", "> quote", "---",
		"func main() {}", "`code` [l](u)", "x := 1 // ```", "``` not closer",
	}
	rng := rand.New(rand.NewPCG(7, 11))
	pick := func() string { return pool[rng.IntN(len(pool))] }

	for doc := range 50 {
		lines := make([]string, rng.IntN(30))
		for i := range lines {
			lines[i] = pick()
		}
		c := NewCache()
		c.Update(lines, 0)
		for step := range 40 {
			// Render a random subset so some spans are memoized.
			for range rng.IntN(len(lines) + 1) {
				c.Spans(rng.IntN(len(lines) + 1))
			}
			k := 0
			if len(lines) > 0 {
				k = rng.IntN(len(lines) + 1)
			}
			next := append([]string(nil), lines[:k]...)
			switch op := rng.IntN(4); {
			case op == 0 && k < len(lines): // replace one line
				next = append(next, pick())
				next = append(next, lines[k+1:]...)
			case op == 1: // insert 1-3 lines
				for range 1 + rng.IntN(3) {
					next = append(next, pick())
				}
				next = append(next, lines[k:]...)
			case op == 2 && k < len(lines): // delete 1-3 lines
				next = append(next, lines[min(len(lines), k+1+rng.IntN(3)):]...)
			default: // replace a range with a different number of lines
				end := min(len(lines), k+rng.IntN(4))
				for range rng.IntN(4) {
					next = append(next, pick())
				}
				next = append(next, lines[end:]...)
			}
			lines = next
			c.Update(lines, k)

			want := fullTokenize(lines)
			for i := range lines {
				got := fmtSpans(describe(lines[i], c.Spans(i)))
				exp := fmtSpans(describe(lines[i], want[i]))
				if got != exp {
					t.Fatalf("doc %d step %d line %d %q:\n got: %s\nwant: %s", doc, step, i, lines[i], got, exp)
				}
			}
			if c.Spans(len(lines)) != nil {
				t.Fatalf("doc %d step %d: stale spans past end", doc, step)
			}
		}
	}
}

// BenchmarkCacheFenceLangToggle toggles the language of a fence at the top of
// a 10k-line document: every line's incoming state changes.
func BenchmarkCacheFenceLangToggle(b *testing.B) {
	docs := [2][]string{fencedDoc("go", 10000), fencedDoc("python", 10000)}
	c := NewCache()
	c.Update(docs[0], 0)
	i := 0
	for b.Loop() {
		i++
		c.Update(docs[i%2], 0)
	}
}

// BenchmarkCacheFenceLangToggleViewport also renders a 50-line viewport
// after each toggle, as the editor does.
func BenchmarkCacheFenceLangToggleViewport(b *testing.B) {
	docs := [2][]string{fencedDoc("go", 10000), fencedDoc("python", 10000)}
	c := NewCache()
	c.Update(docs[0], 0)
	i := 0
	for b.Loop() {
		i++
		c.Update(docs[i%2], 0)
		touchAll(c, 50)
	}
}

// BenchmarkCacheLineEdit edits one paragraph line in a 10k-line document.
func BenchmarkCacheLineEdit(b *testing.B) {
	lines := make([]string, 10000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d with **bold** and #tag", i)
	}
	c := NewCache()
	c.Update(lines, 0)
	i := 0
	for b.Loop() {
		i++
		lines[5000] = fmt.Sprintf("edited %d", i)
		c.Update(lines, 5000)
		c.Spans(5000)
	}
}
