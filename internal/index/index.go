// Package index keeps an in-memory index of every note in a vault (spec §8):
// path, title, content, modification time, tags and tasks. It is built at
// startup by reading all notes in parallel, updated one note at a time on
// saves, watcher events and merges, and never written to disk. The fuzzy
// finder, full-text search, tag sidebar and Tasks view all query it.
//
// Notes stored in the index are immutable: every change replaces the *Note,
// so snapshots returned by Notes, Get and friends are safe to read
// concurrently with updates. Callers must not modify them.
package index

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"github.com/mathieucroset/notty/internal/tags"
	"github.com/mathieucroset/notty/internal/tasks"
	"github.com/mathieucroset/notty/internal/vault"
)

// ErrInvalidUTF8 is recorded as a problem for notes whose content is not
// valid UTF-8. Such notes are still indexed.
var ErrInvalidUTF8 = errors.New("invalid UTF-8")

// Note is one indexed note. Path is vault-relative with "/" separators.
// Build notes with NewNote so the folded content used by ContainsFold is
// precomputed.
type Note struct {
	Path, Title, Content string
	ModTime              time.Time
	Tags                 []string
	Tasks                []tasks.Task

	folded string // Fold(Content)
}

// TagCount is a tag with the number of notes carrying it.
type TagCount struct {
	Tag   string
	Count int
}

// TaskRef is a task together with the note it belongs to.
type TaskRef struct {
	Path, Title string
	Task        tasks.Task
}

// Index is a concurrency-safe in-memory index of notes keyed by path.
type Index struct {
	mu       sync.RWMutex
	notes    map[string]*Note
	problems map[string]error // per-file problems, keyed by path
}

// New returns an empty index.
func New() *Index {
	return &Index{notes: map[string]*Note{}, problems: map[string]error{}}
}

// Build indexes every note in v, reading files in parallel. Hidden
// locations (.git, .notty, .trash, attachments, dotfiles) and non-note files
// are skipped. Only an unreadable vault root is an error; per-file problems
// (unreadable files, invalid UTF-8) are available from Problems, and
// unreadable files are left out of the index.
func Build(v *vault.Vault) (*Index, error) {
	root, err := v.Tree()
	if err != nil {
		return nil, fmt.Errorf("index: build: %w", err)
	}
	var rels []string
	collect(root, &rels)
	return buildFrom(v, rels), nil
}

// buildFrom indexes the notes at rels in parallel. Files that vanished
// since they were listed are skipped silently.
func buildFrom(v *vault.Vault, rels []string) *Index {
	type result struct {
		note *Note
		err  error
	}
	results := make([]result, len(rels))
	var g errgroup.Group
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, rel := range rels {
		g.Go(func() error {
			n, err := load(v, rel)
			if errors.Is(err, fs.ErrNotExist) {
				err = nil // deleted between listing and reading
			}
			results[i] = result{n, err}
			return nil // per-file problems are not fatal
		})
	}
	_ = g.Wait() // workers never return errors

	ix := New()
	for i, r := range results {
		if r.note != nil {
			ix.notes[rels[i]] = r.note
		}
		if r.err != nil {
			ix.problems[rels[i]] = r.err
		}
	}
	return ix
}

// collect appends the paths of all notes below n.
func collect(n *vault.Node, out *[]string) {
	if n.IsNote {
		*out = append(*out, n.Path)
	}
	for _, c := range n.Children {
		collect(c, out)
	}
}

// load reads the note at rel. A nil note means the file could not be read
// (err says why) or is not a regular file (err is nil). A non-nil note may
// come with a problem such as ErrInvalidUTF8.
func load(v *vault.Vault, rel string) (*Note, error) {
	fi, err := os.Lstat(v.Abs(rel))
	if err != nil {
		return nil, fmt.Errorf("index: stat %q: %w", rel, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, nil
	}
	content, err := v.Read(rel)
	if err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	n := NewNote(rel, content, fi.ModTime())
	if !utf8.ValidString(content) {
		return n, fmt.Errorf("index: %q: %w", rel, ErrInvalidUTF8)
	}
	return n, nil
}

// NewNote builds a Note for content at the vault-relative path rel,
// deriving its title, tags, tasks and case-folded content.
func NewNote(rel, content string, mod time.Time) *Note {
	return &Note{
		Path:    rel,
		Title:   vault.Title(content, rel),
		Content: content,
		ModTime: mod,
		Tags:    tags.Parse(content),
		Tasks:   tasks.Parse(content),
		folded:  Fold(content),
	}
}

// ContainsFold reports whether needle occurs in the note's content under
// Unicode simple case folding (as strings.EqualFold compares runes). It
// uses the folded copy precomputed by NewNote; a Note built as a literal
// is folded on every call.
func (n *Note) ContainsFold(needle string) bool {
	f := n.folded
	if f == "" && n.Content != "" {
		f = Fold(n.Content)
	}
	return strings.Contains(f, Fold(needle))
}

// Fold maps every rune of s to the smallest rune of its unicode.SimpleFold
// orbit, so two strings are equal under simple case folding exactly when
// their folds are equal, and substring tests on folded strings are
// case-insensitive. ASCII letters fold to upper case. Invalid UTF-8 bytes
// become U+FFFD. A string that is already folded is returned unchanged
// without allocating.
func Fold(s string) string {
	return strings.Map(foldRune, s)
}

func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if 'a' <= r && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}
	lo := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		lo = min(lo, f)
	}
	return lo
}

// Update re-reads the file at rel. When nothing exists at rel any more, the
// entry and every note under rel are removed, since the watcher reports a
// folder rename or deletion under the folder path only. When rel exists but
// is not a regular .md file (for example a folder), or lies in a hidden
// location, only an entry with that exact path is dropped, never children.
// Other read errors are returned (and recorded in Problems), leaving any
// existing entry untouched. Updating the vault root is a no-op.
//
// A read whose file ModTime is older than the indexed entry's (for example
// an UpdateContent from the editor buffer that happened after the file was
// written) does not overwrite the entry. This guards against stale reads
// that overlap, but filesystems with coarse timestamps can defeat it, so
// callers should still serialize Update/UpdateContent calls per path.
func (ix *Index) Update(v *vault.Vault, rel string) error {
	rel = clean(rel)
	if rel == "" {
		return nil
	}
	if _, err := os.Lstat(v.Abs(rel)); errors.Is(err, fs.ErrNotExist) {
		ix.Remove(rel)
		return nil
	}
	if !isNotePath(rel) {
		ix.dropExact(rel)
		return nil
	}
	n, err := load(v, rel)
	if errors.Is(err, fs.ErrNotExist) { // vanished since the Lstat above
		ix.Remove(rel)
		return nil
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	switch {
	case n == nil && err == nil: // not a regular file
		delete(ix.notes, rel)
		delete(ix.problems, rel)
		return nil
	case n == nil:
		ix.problems[rel] = err
		return err
	}
	if old, ok := ix.notes[rel]; ok && old.ModTime.After(n.ModTime) {
		return nil // stale read: the indexed version is newer
	}
	ix.notes[rel] = n
	if err != nil {
		ix.problems[rel] = err
	} else {
		delete(ix.problems, rel)
	}
	return nil
}

// dropExact removes the entry at exactly rel, if any.
func (ix *Index) dropExact(rel string) {
	ix.mu.Lock()
	delete(ix.notes, rel)
	delete(ix.problems, rel)
	ix.mu.Unlock()
}

// UpdateContent indexes content (typically the editor buffer) as the note
// at rel, with ModTime set to now. Paths that are not notes, or that lie in
// hidden locations, are ignored.
func (ix *Index) UpdateContent(rel, content string) {
	rel = clean(rel)
	if !isNotePath(rel) {
		return
	}
	n := NewNote(rel, content, time.Now())
	ix.mu.Lock()
	ix.notes[rel] = n
	delete(ix.problems, rel)
	ix.mu.Unlock()
}

// Remove drops the note at rel, or every note inside rel when it is a
// folder. Removing the vault root ("") is a no-op.
func (ix *Index) Remove(rel string) {
	rel = clean(rel)
	if rel == "" {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for p := range ix.notes {
		if within(p, rel) {
			delete(ix.notes, p)
		}
	}
	for p := range ix.problems {
		if within(p, rel) {
			delete(ix.problems, p)
		}
	}
}

// Rename moves the note at oldRel, or every note inside the folder oldRel,
// to newRel. Titles are recomputed, since they may fall back to the file
// name. Notes whose new path is not a note or lies in a hidden location
// (such as .trash) are dropped. Renaming the vault root is a no-op.
func (ix *Index) Rename(oldRel, newRel string) {
	oldRel, newRel = clean(oldRel), clean(newRel)
	if oldRel == "" || newRel == "" || oldRel == newRel {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	moved := map[string]*Note{}
	for p, n := range ix.notes {
		if !within(p, oldRel) {
			continue
		}
		delete(ix.notes, p)
		delete(ix.problems, p)
		np := newRel + p[len(oldRel):]
		if !isNotePath(np) {
			continue
		}
		cp := *n
		cp.Path = np
		cp.Title = vault.Title(n.Content, np)
		moved[np] = &cp
	}
	for p, n := range moved {
		ix.notes[p] = n
	}
}

// Notes returns a snapshot of all notes sorted by path.
func (ix *Index) Notes() []*Note {
	ix.mu.RLock()
	out := make([]*Note, 0, len(ix.notes))
	for _, n := range ix.notes {
		out = append(out, n)
	}
	ix.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Get returns the note at rel.
func (ix *Index) Get(rel string) (*Note, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	n, ok := ix.notes[clean(rel)]
	return n, ok
}

// Len returns the number of indexed notes.
func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.notes)
}

// Problems returns the current per-file problems (from Build and later
// updates), sorted by path.
func (ix *Index) Problems() []error {
	ix.mu.RLock()
	ps := make([]string, 0, len(ix.problems))
	for p := range ix.problems {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	out := make([]error, len(ps))
	for i, p := range ps {
		out[i] = ix.problems[p]
	}
	ix.mu.RUnlock()
	return out
}

// TagCounts lists every tag used in some note with the number of distinct
// notes that carry it or a tag nested under it, so each count equals
// len(NotesWithTag(tag)). Tags are grouped case-insensitively and shown
// with the spelling of the first note (by path) that uses them. A parent
// that is only used through nested tags (#a/b without any #a) is not
// listed. The result is sorted by count descending, then by name ignoring
// case.
func (ix *Index) TagCounts() []TagCount {
	spelling := map[string]string{} // lowercased tag -> display spelling
	counts := map[string]int{}      // lowercased tag or ancestor -> notes
	for _, n := range ix.Notes() {
		keys := map[string]bool{} // this note's tags and their ancestors
		for _, t := range n.Tags {
			key := strings.ToLower(t)
			if _, ok := spelling[key]; !ok {
				spelling[key] = t
			}
			for k := key; ; {
				keys[k] = true
				i := strings.LastIndexByte(k, '/')
				if i < 0 {
					break
				}
				k = k[:i]
			}
		}
		for k := range keys {
			counts[k]++
		}
	}
	out := make([]TagCount, 0, len(spelling))
	for key, t := range spelling {
		out = append(out, TagCount{Tag: t, Count: counts[key]})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		la, lb := strings.ToLower(a.Tag), strings.ToLower(b.Tag)
		if la != lb {
			return la < lb
		}
		return a.Tag < b.Tag
	})
	return out
}

// NotesWithTag returns the paths, sorted, of notes tagged tag (a leading
// '#' is optional). Matching ignores case and includes nested tags: "work"
// matches notes tagged work or work/anything.
func (ix *Index) NotesWithTag(tag string) []string {
	var out []string
	for _, n := range ix.Notes() {
		if HasTag(n, tag) {
			out = append(out, n.Path)
		}
	}
	return out
}

// HasTag reports whether n carries tag with NotesWithTag semantics. An
// empty tag matches nothing.
func HasTag(n *Note, tag string) bool {
	want := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(tag, "#"), "/"))
	if want == "" {
		return false
	}
	for _, t := range n.Tags {
		lt := strings.ToLower(t)
		if lt == want || strings.HasPrefix(lt, want+"/") {
			return true
		}
	}
	return false
}

// AllTasks returns every task across all notes, open and done (the
// Tasks view filters them), sorted by path then line.
func (ix *Index) AllTasks() []TaskRef {
	var out []TaskRef
	for _, n := range ix.Notes() { // already sorted by path
		for _, t := range n.Tasks { // already in line order
			out = append(out, TaskRef{Path: n.Path, Title: n.Title, Task: t})
		}
	}
	return out
}

// clean normalizes a vault-relative path to a "/"-separated path with no
// leading or trailing slash; the vault root is "".
func clean(rel string) string {
	return strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(rel)), "/")
}

// within reports whether the clean path p is base or lies inside it.
func within(p, base string) bool {
	return p == base || strings.HasPrefix(p, base+"/")
}

// hiddenTopLevel mirrors the vault-root entries vault.Tree hides.
var hiddenTopLevel = map[string]bool{
	".git":        true,
	".notty":      true,
	".trash":      true,
	"attachments": true,
}

// isNotePath reports whether the clean path rel names a note Build would
// index: a ".md" file (any case) outside hidden locations, matching the
// rules of vault.Tree.
func isNotePath(rel string) bool {
	if rel == "" || !strings.EqualFold(path.Ext(rel), ".md") {
		return false
	}
	for i, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".notty-tmp") {
			return false
		}
		if i == 0 && hiddenTopLevel[seg] {
			return false
		}
	}
	return true
}
