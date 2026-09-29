// Package localstate manages the per-machine UI state that lives outside the
// vault (recently opened notes, the last open note and cursor position, and
// which folders are expanded). It is never synced, so it must tolerate a
// missing file and never fail the caller when there is simply nothing saved
// yet.
package localstate

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxRecents is the cap on the number of recently opened notes retained.
const maxRecents = 20

// State is the per-machine UI state for a single vault.
type State struct {
	// Recents holds vault-relative note paths, most recently opened first,
	// capped at maxRecents entries.
	Recents []string `json:"recents"`
	// LastNote is the vault-relative path of the note that was open last.
	LastNote string `json:"last_note"`
	// Cursor maps a vault-relative note path to its [line, col] cursor
	// position.
	Cursor map[string][2]int `json:"cursor"`
	// Expanded holds the vault-relative paths of folders currently expanded
	// in the sidebar.
	Expanded []string `json:"expanded"`
}

// newState returns an empty State with all fields initialized to non-nil,
// empty values.
func newState() *State {
	return &State{
		Recents:  []string{},
		LastNote: "",
		Cursor:   map[string][2]int{},
		Expanded: []string{},
	}
}

// PathFor returns the path of the state file for the vault rooted at
// vaultRoot, inside stateDir: stateDir/<hex sha1(abs clean vaultRoot)[:12]>.json.
func PathFor(stateDir, vaultRoot string) string {
	abs, err := filepath.Abs(vaultRoot)
	if err != nil {
		abs = vaultRoot
	}
	abs = filepath.Clean(abs)

	sum := sha1.Sum([]byte(abs)) //nolint:gosec // used as a stable identifier, not for security
	name := hex.EncodeToString(sum[:])[:12] + ".json"
	return filepath.Join(stateDir, name)
}

// Load reads the state file at path. A missing file is not an error: it
// returns an empty state with non-nil maps and slices.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newState(), nil
		}
		return nil, fmt.Errorf("localstate: read %s: %w", path, err)
	}

	s := newState()
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("localstate: parse %s: %w", path, err)
	}

	// Guard against a saved file that had null for one of these fields.
	if s.Recents == nil {
		s.Recents = []string{}
	}
	if s.Cursor == nil {
		s.Cursor = map[string][2]int{}
	}
	if s.Expanded == nil {
		s.Expanded = []string{}
	}
	return s, nil
}

// Save writes the state to path atomically: it writes to a temp file in the
// same directory and renames it into place, creating parent directories as
// needed.
func (s *State) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("localstate: create dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("localstate: marshal state: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("localstate: create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup; harmless if the rename below already moved it.
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("localstate: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("localstate: close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("localstate: rename temp file to %s: %w", path, err)
	}
	return nil
}

// Touch records notePath as the most recently opened note: it moves the
// entry to the front of Recents (deduping any existing occurrence), caps
// Recents at maxRecents, and sets LastNote.
func (s *State) Touch(notePath string) {
	filtered := make([]string, 0, len(s.Recents)+1)
	filtered = append(filtered, notePath)
	for _, r := range s.Recents {
		if r == notePath {
			continue
		}
		filtered = append(filtered, r)
	}
	if len(filtered) > maxRecents {
		filtered = filtered[:maxRecents]
	}
	s.Recents = filtered
	s.LastNote = notePath
}

// renamePath rewrites p if it equals oldPath (an exact note match) or if it
// is inside the folder oldPath (i.e. starts with "oldPath/"). Any other path,
// including one that merely shares oldPath as a string prefix without the
// separator (e.g. "Work2/x" when renaming "Work"), is left untouched.
func renamePath(p, oldPath, newPath string) string {
	if p == oldPath {
		return newPath
	}
	if strings.HasPrefix(p, oldPath+"/") {
		return newPath + p[len(oldPath):]
	}
	return p
}

// Rename updates every reference to oldPath (or, for a folder, everything
// under it) to newPath across Recents, LastNote, Cursor, and Expanded.
func (s *State) Rename(oldPath, newPath string) {
	for i, r := range s.Recents {
		s.Recents[i] = renamePath(r, oldPath, newPath)
	}

	if s.LastNote != "" {
		s.LastNote = renamePath(s.LastNote, oldPath, newPath)
	}

	if len(s.Cursor) > 0 {
		renamed := make(map[string][2]int, len(s.Cursor))
		for k, v := range s.Cursor {
			renamed[renamePath(k, oldPath, newPath)] = v
		}
		s.Cursor = renamed
	}

	for i, e := range s.Expanded {
		s.Expanded[i] = renamePath(e, oldPath, newPath)
	}
}

// matchesPath reports whether p is target itself or is inside the folder
// target (i.e. starts with "target/").
func matchesPath(p, target string) bool {
	return p == target || strings.HasPrefix(p, target+"/")
}

// Remove deletes every reference to path (and, for a folder, everything
// under it) from Recents, LastNote, Cursor, and Expanded.
func (s *State) Remove(path string) {
	recents := make([]string, 0, len(s.Recents))
	for _, r := range s.Recents {
		if !matchesPath(r, path) {
			recents = append(recents, r)
		}
	}
	s.Recents = recents

	if matchesPath(s.LastNote, path) {
		s.LastNote = ""
	}

	for k := range s.Cursor {
		if matchesPath(k, path) {
			delete(s.Cursor, k)
		}
	}

	expanded := make([]string, 0, len(s.Expanded))
	for _, e := range s.Expanded {
		if !matchesPath(e, path) {
			expanded = append(expanded, e)
		}
	}
	s.Expanded = expanded
}
