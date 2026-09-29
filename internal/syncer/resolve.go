package syncer

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/meta"
)

// statePath is the synced pins file.
const statePath = ".notty/state.json"

// autoResolve resolves the conflicts spec §7 handles without the resolver:
// .notty/state.json (a three-way merge of the pins) and the same note trashed
// on both sides (both trash items are kept, or only ours if their contents
// are identical). Everything else is left for the resolver.
func (s *Syncer) autoResolve() error {
	pcs, rest, err := s.repo.PathConflicts()
	if err != nil {
		return fmt.Errorf("syncer: auto-resolve: %w", err)
	}
	for _, pc := range pcs {
		if pc.Kind == gitsync.BothTrashed {
			if err := s.resolveBothTrashed(pc); err != nil {
				return err
			}
		}
	}
	for _, c := range rest {
		if c.Path == statePath && (c.Kind == gitsync.BothModified || c.Kind == gitsync.BothAdded) {
			if err := s.resolvePins(c); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolvePins merges the base, ours and theirs stages of state.json with
// meta.Merge3. A stage that does not parse leaves the file to the resolver.
func (s *Syncer) resolvePins(c gitsync.Conflict) error {
	var states [4]*meta.State
	for stage := 1; stage <= 3; stage++ {
		var data []byte
		if c.Blob[stage] != "" {
			b, err := s.repo.ShowBlob(c.Blob[stage])
			if err != nil {
				return fmt.Errorf("syncer: auto-resolve pins: %w", err)
			}
			data = b
		}
		st, err := meta.Parse(data)
		if err != nil {
			return nil
		}
		states[stage] = st
	}
	merged := &meta.State{Pins: meta.Merge3(states[1].Pins, states[2].Pins, states[3].Pins)}
	if err := s.writeFile(statePath, merged.Marshal()); err != nil {
		return err
	}
	if err := s.repo.Add(statePath); err != nil {
		return fmt.Errorf("syncer: auto-resolve pins: %w", err)
	}
	return nil
}

// resolveBothTrashed keeps both trash items of a note trashed on both sides,
// each with its own side's content, and removes the original path. When the
// contents are identical only ours is kept, and their now-empty trash item
// (its meta.json) is removed too.
func (s *Syncer) resolveBothTrashed(pc gitsync.PathConflict) error {
	ours, err := s.repo.ShowBlob(pc.OursBlob)
	if err != nil {
		return fmt.Errorf("syncer: auto-resolve trash: %w", err)
	}
	theirs, err := s.repo.ShowBlob(pc.TheirsBlob)
	if err != nil {
		return fmt.Errorf("syncer: auto-resolve trash: %w", err)
	}
	if err := s.writeFile(pc.Ours, ours); err != nil {
		return err
	}
	if err := s.repo.Add(pc.Ours); err != nil {
		return fmt.Errorf("syncer: auto-resolve trash: %w", err)
	}
	if bytes.Equal(ours, theirs) {
		if err := s.repo.Remove(pc.Theirs); err != nil {
			return fmt.Errorf("syncer: auto-resolve trash: %w", err)
		}
		if err := s.dropEmptyTrashItem(pc.Theirs); err != nil {
			return err
		}
	} else {
		if err := s.writeFile(pc.Theirs, theirs); err != nil {
			return err
		}
		if err := s.repo.Add(pc.Theirs); err != nil {
			return fmt.Errorf("syncer: auto-resolve trash: %w", err)
		}
	}
	if err := s.repo.Remove(pc.Original); err != nil {
		return fmt.Errorf("syncer: auto-resolve trash: %w", err)
	}
	return nil
}

// dropEmptyTrashItem removes the empty folders left above a removed trash
// file, and the whole .trash/<id>/ item if only its meta.json remains.
func (s *Syncer) dropEmptyTrashItem(removed string) error {
	rest, ok := cutTrashID(removed)
	if !ok {
		return nil
	}
	itemDir := trashDir + rest
	for d := path.Dir(removed); d != itemDir && d != "." && len(d) > len(itemDir); d = path.Dir(d) {
		if os.Remove(s.abs(d)) != nil {
			break // not empty
		}
	}
	entries, err := os.ReadDir(s.abs(itemDir))
	if err != nil || len(entries) != 1 || entries[0].Name() != "meta.json" {
		return nil
	}
	if err := s.repo.Remove(itemDir + "/meta.json"); err != nil {
		return fmt.Errorf("syncer: auto-resolve trash: %w", err)
	}
	_ = os.Remove(s.abs(itemDir))
	return nil
}

// cutTrashID returns the <id> of a path .trash/<id>/...
func cutTrashID(p string) (string, bool) {
	rest, ok := strings.CutPrefix(p, trashDir)
	if !ok {
		return "", false
	}
	id, _, ok := strings.Cut(rest, "/")
	return id, ok && id != ""
}

func (s *Syncer) abs(rel string) string { return filepath.Join(s.dir, filepath.FromSlash(rel)) }

// writeFile writes a resolved file into the working tree (the watcher is
// paused by the locked merge section).
func (s *Syncer) writeFile(rel string, data []byte) error {
	full := s.abs(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("syncer: write %s: %w", rel, err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return fmt.Errorf("syncer: write %s: %w", rel, err)
	}
	return nil
}
