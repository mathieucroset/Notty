// Package meta manages Notty's synced vault metadata: pinned notes stored in
// .notty/state.json. The file is small enough to merge with a three-way set
// merge when it conflicts during git sync (see docs/superpowers/specs, §7).
package meta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// State is the contents of .notty/state.json: the vault's pinned notes, in
// pin order (most recently pinned last).
type State struct {
	Pins []string `json:"pins"`
}

// stateRelPath is the state file's location relative to the vault root.
const stateRelPath = ".notty/state.json"

// Load reads .notty/state.json from vaultRoot. A missing file is not an
// error: it yields an empty State.
func Load(vaultRoot string) (*State, error) {
	data, err := os.ReadFile(filepath.Join(vaultRoot, filepath.FromSlash(stateRelPath)))
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Pins: []string{}}, nil
		}
		return nil, fmt.Errorf("meta: read state file: %w", err)
	}
	return Parse(data)
}

// Parse decodes a state.json document. It is also used to decode the base,
// ours, and theirs git stage blobs during conflict auto-resolution. Empty
// data (including a zero-length blob) yields an empty State rather than an
// error.
func Parse(data []byte) (*State, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return &State{Pins: []string{}}, nil
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("meta: parse state: %w", err)
	}
	if s.Pins == nil {
		s.Pins = []string{}
	}
	return &s, nil
}

// Marshal renders the state as stable JSON: 2-space indent, a trailing
// newline, and pins in their existing order (never sorted, never null).
func (s *State) Marshal() []byte {
	pins := s.Pins
	if pins == nil {
		pins = []string{}
	}
	out := struct {
		Pins []string `json:"pins"`
	}{Pins: pins}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		// State only ever holds a string slice: encoding cannot fail.
		panic(fmt.Sprintf("meta: marshal state: %v", err))
	}
	return append(data, '\n')
}

// Save atomically writes the state to .notty/state.json under vaultRoot,
// creating .notty/ if needed. It writes a temp file and renames it into
// place so readers never see a partial write.
func (s *State) Save(vaultRoot string) error {
	dir := filepath.Join(vaultRoot, ".notty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("meta: create .notty dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("meta: create temp state file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(s.Marshal()); err != nil {
		tmp.Close()
		return fmt.Errorf("meta: write temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("meta: close temp state file: %w", err)
	}

	target := filepath.Join(dir, "state.json")
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("meta: rename state file: %w", err)
	}
	return nil
}

// Toggle pins rel if it is not already pinned, or unpins it if it is.
// A newly pinned note is appended to the end. It reports whether rel is
// pinned after the call.
func (s *State) Toggle(rel string) bool {
	for i, p := range s.Pins {
		if p == rel {
			s.Pins = append(s.Pins[:i], s.Pins[i+1:]...)
			return false
		}
	}
	s.Pins = append(s.Pins, rel)
	return true
}

// IsPinned reports whether rel is pinned.
func (s *State) IsPinned(rel string) bool {
	for _, p := range s.Pins {
		if p == rel {
			return true
		}
	}
	return false
}

// Rename updates any pin at oldRel, or under it as a folder, to live under
// newRel instead. It is prefix-aware: renaming "Work" to "Job" updates
// "Work/a.md" but leaves "Work2/b.md" untouched.
func (s *State) Rename(oldRel, newRel string) {
	prefix := oldRel + "/"
	for i, p := range s.Pins {
		if p == oldRel {
			s.Pins[i] = newRel
		} else if strings.HasPrefix(p, prefix) {
			s.Pins[i] = newRel + p[len(oldRel):]
		}
	}
}

// Remove unpins rel. When rel names a folder, its children's pins are
// removed too.
func (s *State) Remove(rel string) {
	prefix := rel + "/"
	kept := make([]string, 0, len(s.Pins))
	for _, p := range s.Pins {
		if p == rel || strings.HasPrefix(p, prefix) {
			continue
		}
		kept = append(kept, p)
	}
	s.Pins = kept
}

// Merge3 three-way merges a pin list. Pins added on either side (relative to
// base) are kept; pins removed on either side are dropped, even if the other
// side left them untouched. The result orders ours first, in its existing
// order, followed by pins that theirs added and ours doesn't already have,
// in theirs' order. No duplicates.
func Merge3(base, ours, theirs []string) []string {
	baseSet := toSet(base)
	oursSet := toSet(ours)
	theirsSet := toSet(theirs)

	removed := make(map[string]bool, len(base))
	for _, b := range base {
		if !oursSet[b] || !theirsSet[b] {
			removed[b] = true
		}
	}

	result := make([]string, 0, len(ours)+len(theirs))
	inResult := make(map[string]bool, len(ours)+len(theirs))

	for _, o := range ours {
		if removed[o] || inResult[o] {
			continue
		}
		result = append(result, o)
		inResult[o] = true
	}
	for _, t := range theirs {
		if baseSet[t] || removed[t] || inResult[t] {
			continue
		}
		result = append(result, t)
		inResult[t] = true
	}
	return result
}

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, it := range items {
		set[it] = true
	}
	return set
}
