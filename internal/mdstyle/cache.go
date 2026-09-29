package mdstyle

// entry is the cached tokenization of one line.
type entry struct {
	text    string
	in, out State
	spans   []Span
}

// Cache holds per-line spans for a document and re-tokenizes incrementally.
type Cache struct {
	lines         []entry
	tokenizeCalls int // number of TokenizeLine calls, for tests
}

// NewCache returns an empty cache.
func NewCache() *Cache { return &Cache{} }

// Update brings the cache in line with lines. firstChanged is the first line
// index whose text may differ from the previous Update; lines above it are
// assumed unchanged and kept as is. The line count may differ from the
// previous call (inserted or deleted lines).
//
// From firstChanged down, each line is re-tokenized unless a cached line
// with the same text and the same incoming state is available at the
// matching position (shifted by the change in line count), in which case its
// spans are reused. An ordinary edit therefore re-tokenizes one line, while
// opening or closing a fence re-tokenizes every line whose incoming state
// changed.
func (c *Cache) Update(lines []string, firstChanged int) {
	old := c.lines
	delta := len(lines) - len(old)
	keep := max(0, min(firstChanged, len(old), len(lines)))

	// Reusing an entry is always safe when text and incoming state match,
	// because tokenization is a pure function of the two. With an unchanged
	// line count the slice is updated in place.
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
		spans, out := TokenizeLine(lines[i], in)
		c.tokenizeCalls++
		next[i] = entry{text: lines[i], in: in, out: out, spans: spans}
	}
	c.lines = next
}

// Spans returns the cached spans of line, or nil when out of range. The
// returned slice is shared with the cache and must not be modified.
func (c *Cache) Spans(line int) []Span {
	if line < 0 || line >= len(c.lines) {
		return nil
	}
	return c.lines[line].spans
}
