// Package vault manages notes and folders stored as plain files inside a
// vault directory: listing, naming, creating, renaming, moving and atomically
// saving them.
package vault

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Sentinel errors, matchable with errors.Is.
var (
	// ErrExists means the destination of a create, rename or move is taken.
	ErrExists = errors.New("target already exists")
	// ErrInvalidPath means a path is unusable for the operation (for
	// example the vault root itself, or moving a folder into itself).
	ErrInvalidPath = errors.New("invalid path")
	// ErrInvalidName means a name is empty after sanitization.
	ErrInvalidName = errors.New("invalid name")
)

// tmpSuffix is appended to a file's name while it is being saved atomically.
const tmpSuffix = ".notty-tmp"

// hiddenTopLevel lists vault-root entries that never appear in the tree.
var hiddenTopLevel = map[string]bool{
	".git":        true,
	".notty":      true,
	".trash":      true,
	"attachments": true,
}

// Vault is a directory of notes. Root is absolute and clean.
type Vault struct{ Root string }

// Node is an entry in the vault tree. Path is vault-relative with "/"
// separators; the root node has Path "".
type Node struct {
	Name, Path    string
	IsDir, IsNote bool
	Children      []*Node
}

// Open returns the vault rooted at root, creating the directory if missing.
func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("vault: resolve root %q: %w", root, err)
	}
	abs = filepath.Clean(abs)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("vault: create root: %w", err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("vault: stat root: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("vault: root %q is not a directory: %w", abs, ErrInvalidPath)
	}
	return &Vault{Root: abs}, nil
}

// clean normalizes a vault-relative path to a "/"-separated path with no
// leading slash and no ".." escaping the root. The root itself is "".
func clean(rel string) string {
	p := path.Clean("/" + filepath.ToSlash(rel))
	return strings.TrimPrefix(p, "/")
}

// Abs returns the absolute filesystem path of a vault-relative path. Paths
// that would escape the root are cleaned so the result always lies within it.
func (v *Vault) Abs(rel string) string {
	c := clean(rel)
	if c == "" {
		return v.Root
	}
	return filepath.Join(v.Root, filepath.FromSlash(c))
}

// Tree returns the vault's folders and files. Hidden entries (.git, .notty,
// .trash and attachments at the top level, dotfiles anywhere, and in-flight
// save temp files) are omitted. Directories come first, then files, each
// sorted alphabetically ignoring case.
func (v *Vault) Tree() (*Node, error) {
	root := &Node{Name: filepath.Base(v.Root), Path: "", IsDir: true}
	if err := v.fill(root); err != nil {
		return nil, err
	}
	return root, nil
}

func (v *Vault) fill(dir *Node) error {
	entries, err := os.ReadDir(v.Abs(dir.Path))
	if err != nil {
		return fmt.Errorf("vault: read dir %q: %w", dir.Path, err)
	}
	for _, e := range entries {
		name := e.Name()
		if hidden(dir.Path, name) {
			continue
		}
		n := &Node{Name: name, Path: path.Join(dir.Path, name)}
		switch {
		case e.IsDir():
			n.IsDir = true
			if err := v.fill(n); err != nil {
				return err
			}
		case e.Type().IsRegular() && isNoteName(name):
			n.IsNote = true
		}
		dir.Children = append(dir.Children, n)
	}
	sort.Slice(dir.Children, func(i, j int) bool {
		a, b := dir.Children[i], dir.Children[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if la != lb {
			return la < lb
		}
		return a.Name < b.Name
	})
	return nil
}

// hidden reports whether Tree omits the entry name inside folder parent.
func hidden(parent, name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, tmpSuffix) {
		return true
	}
	return parent == "" && hiddenTopLevel[name]
}

// reserved reports whether name inside folder parent must not be created by
// user actions: anything Tree hides, with the top-level names matched
// case-insensitively so "Attachments" cannot alias "attachments" on
// case-insensitive filesystems.
func reserved(parent, name string) bool {
	return hidden(parent, name) || (parent == "" && hiddenTopLevel[strings.ToLower(name)])
}

// inReserved reports whether the clean path rel is, or lies inside, a
// reserved entry (for example ".trash/x" or "attachments").
func inReserved(rel string) bool {
	if rel == "" {
		return false
	}
	parent := ""
	for _, seg := range strings.Split(rel, "/") {
		if reserved(parent, seg) {
			return true
		}
		parent = path.Join(parent, seg)
	}
	return false
}

// parentOf returns the clean parent folder of a clean path ("" for the root).
func parentOf(rel string) string {
	if d := path.Dir(rel); d != "." {
		return d
	}
	return ""
}

// isNoteName reports whether a file name has the note extension (".md",
// ignoring case).
func isNoteName(name string) bool { return hasNoteExt(name) }
