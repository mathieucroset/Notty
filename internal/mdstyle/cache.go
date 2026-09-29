package mdstyle

// entry is the cached state of one line. Spans are computed lazily.
type entry struct {
	text     string
	in, out  State
	spans    []Span
	computed bool // spans is valid
}

// Cache holds per-line state and spans for a document. Update only
// propagates line states (cheap); spans are computed on first access by
// Spans and memoized, so only the lines the editor actually renders are
// tokenized or syntax-highlighted.
//
// A Cache is not safe for concurrent use: both Update and Spans mutate it.
type Cache struct {
	lines []entry

	// Counters for tests.
	stateSteps     int // nextState calls made by Update
	tokenizeCalls  int // span computations made by Spans (all line kinds)
	highlightCalls int // the subset of tokenizeCalls for code lines inside a fence
}

// NewCache returns an empty cache.
func NewCache() *Cache { return &Cache{} }

// Update brings the cache in line with lines. firstChanged is the first line
// index whose text may differ from the previous Update; lines above it are
// assumed unchanged and kept as is. The line count may differ from the
// previous call (inserted or deleted lines).
//
// From firstChanged down, each line keeps its cached entry (including any
// memoized spans) when a cached line with the same text and the same
// incoming state exists at the matching position (shifted by the change in
// line count). Otherwise its outgoing state is recomputed and its spans are
// invalidated. An ordinary edit therefore invalidates one line, while
// opening, closing or retagging a fence invalidates every line whose
// incoming state changed, at the cost of one cheap state step per line.
func (c *Cache) Update(lines []string, firstChanged int) {
	old := c.lines
	delta := len(lines) - len(old)
	keep := max(0, min(firstChanged, len(old), len(lines)))

	// Reusing an entry is always safe when text and incoming state match,
	// because both the outgoing state and the spans are pure functions of
	// the two. With an unchanged line count the slice is updated in place.
	next := old
	if delta != 0 {
		next = make([]entry, len(lines))
		copy(next, old[:keep])
	}
	for i := keep; i < len(lines); i++ {
		var in State
		if i > 0 {
			in = next[i-1].out
		}
		if j := i - delta; j >= keep && j < len(old) && old[j].in == in && old[j].text == lines[i] {
			next[i] = old[j]
			continue
		}
		if delta != 0 && i < len(old) && old[i].in == in && old[i].text == lines[i] {
			next[i] = old[i]
			continue
		}
		c.stateSteps++
		next[i] = entry{text: lines[i], in: in, out: nextState(lines[i], in)}
	}
	c.lines = next
}

// Spans returns the spans of line, or nil when out of range. Spans are
// computed on first access and memoized, so Spans mutates the cache and must
// be called from the same goroutine as Update. The returned slice is shared
// with the cache and must not be modified.
func (c *Cache) Spans(line int) []Span {
	if line < 0 || line >= len(c.lines) {
		return nil
	}
	e := &c.lines[line]
	if !e.computed {
		e.spans, _ = TokenizeLine(e.text, e.in)
		e.computed = true
		c.tokenizeCalls++
		if e.in.InFence && e.out.InFence {
			c.highlightCalls++
		}
	}
	return e.spans
}
