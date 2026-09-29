package mdstyle

import (
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// lexerCache memoizes lexer lookups by language name; lexers.Get falls back
// to a linear filename-pattern scan for unknown names, which is too slow to
// repeat for every code line on every keystroke.
var lexerCache sync.Map // string -> chroma.Lexer

func lexerFor(lang string) chroma.Lexer {
	key := strings.ToLower(lang)
	if l, ok := lexerCache.Load(key); ok {
		return l.(chroma.Lexer)
	}
	var l chroma.Lexer
	if key != "" {
		l = lexers.Get(key)
	}
	if l == nil {
		l = lexers.Fallback
	}
	l = chroma.Coalesce(l)
	lexerCache.Store(key, l)
	return l
}

// HighlightCode tokenizes one line of code with the chroma lexer for lang
// (plaintext when lang is empty or unknown). Every returned span has Kind
// CodeBlock, Lang set to lang, and Token set to the chroma token type; the
// spans cover the whole line. Lines are highlighted independently, so
// constructs spanning several lines (block comments, multi-line strings) are
// only approximated.
func HighlightCode(lang, line string) []Span {
	if line == "" {
		return nil
	}
	plain := []Span{{Start: 0, End: len(line), Kind: CodeBlock, Lang: lang, Token: chroma.Text}}
	full := line + "\n"
	it, err := lexerFor(lang).Tokenise(nil, full)
	if err != nil {
		return plain
	}
	var out []Span
	pos := 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		// Chroma may rewrite its input (e.g. invalid UTF-8 becomes U+FFFD);
		// if the tokens stop matching the line byte for byte, the offsets
		// would be wrong, so fall back to one plain span.
		if !strings.HasPrefix(full[pos:], tok.Value) {
			return plain
		}
		start := pos
		pos += len(tok.Value)
		end := min(pos, len(line))
		if end <= start {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Token == tok.Type && out[n-1].End == start {
			out[n-1].End = end
			continue
		}
		out = append(out, Span{Start: start, End: end, Kind: CodeBlock, Lang: lang, Token: tok.Type})
	}
	if pos < len(line) { // the tokens did not cover the whole line
		return plain
	}
	return out
}
