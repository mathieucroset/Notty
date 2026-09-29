package mdstyle

import (
	"fmt"
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
