// Package buffer is the text model under the editor: a slice of lines with
// grapheme-aware positions, three primitive edit operations (Insert, Delete,
// Replace) and grouped undo/redo.
package buffer

import "github.com/rivo/uniseg"

// Graphemes splits s into extended grapheme clusters. It returns nil for "".
func Graphemes(s string) []string {
	var out []string
	state := -1
	for s != "" {
		var cluster string
		cluster, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		out = append(out, cluster)
	}
	return out
}

// graphemeCount returns the number of grapheme clusters in s.
func graphemeCount(s string) int {
	return uniseg.GraphemeClusterCount(s)
}

// DisplayWidth returns the number of terminal cells s occupies
// (CJK and emoji are 2 cells wide). Tabs and other control characters count
// as width 0: DisplayWidth knows nothing about tab stops, so the renderer
// must expand tabs itself before (or while) measuring.
func DisplayWidth(s string) int {
	return uniseg.StringWidth(s)
}

// ColToByte returns the byte offset in s where grapheme cluster col starts.
// col <= 0 yields 0; col >= the cluster count yields len(s).
func ColToByte(s string, col int) int {
	if col <= 0 {
		return 0
	}
	off := 0
	rest := s
	state := -1
	for i := 0; i < col && rest != ""; i++ {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		off += len(cluster)
	}
	return off
}

// ByteToCol returns the index of the grapheme cluster containing byte offset
// byteOff in s (an offset inside a cluster floors to that cluster). Offsets
// <= 0 yield 0; offsets >= len(s) yield the cluster count.
func ByteToCol(s string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	if byteOff >= len(s) {
		return graphemeCount(s)
	}
	col, off := 0, 0
	rest := s
	state := -1
	for rest != "" {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		off += len(cluster)
		if off > byteOff {
			return col
		}
		col++
	}
	return col
}

// byteToColCeil is ByteToCol rounding up: an offset inside a cluster maps to
// the column just after that cluster.
func byteToColCeil(s string, byteOff int) int {
	if byteOff <= 0 {
		return 0
	}
	if byteOff >= len(s) {
		return graphemeCount(s)
	}
	col, off := 0, 0
	rest := s
	state := -1
	for off < byteOff {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		off += len(cluster)
		col++
	}
	return col
}
