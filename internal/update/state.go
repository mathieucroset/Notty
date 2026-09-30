package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// checkInterval is the least time between two network checks.
const checkInterval = 24 * time.Hour

// State is what the update check remembers between runs, per machine.
type State struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`   // newest release seen
	Notified  string    `json:"notified"` // version the toast was shown for
}

// LoadState reads the state file at path. A missing or corrupt file gives
// the zero State: the check then simply runs again.
func LoadState(path string) State {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}
	}
	return s
}

// Temp files Save writes: tmpPrefix + random + tmpSuffix. One older than
// staleTemp was left behind by a crash and is removed.
const (
	tmpPrefix = ".update-check-"
	tmpSuffix = ".tmp"
	staleTemp = time.Minute
)

// Save writes s to path atomically (a temp file synced, then renamed into
// place), with mode 0o600, creating the directory (0o700) if needed. It
// also removes temp files a crashed save left behind.
//
// Two instances starting at once may each run the check and show the toast
// once, and the last write wins; that is accepted rather than locking.
func (s State) Save(path string) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("update: encoding state: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("update: creating state directory %s: %w", dir, err)
	}
	removeStaleTemps(dir, time.Now())
	tmp, err := os.CreateTemp(dir, tmpPrefix+"*"+tmpSuffix)
	if err != nil {
		return fmt.Errorf("update: creating temp state file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("update: writing state %s: %w", path, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("update: setting state file mode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("update: syncing temp state file %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("update: closing temp state file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("update: renaming temp state file to %s: %w", path, err)
	}
	return nil
}

// removeStaleTemps removes Save's temp files in dir older than staleTemp,
// best effort: a newer one may belong to another instance saving now.
func removeStaleTemps(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(name, tmpPrefix) || !strings.HasSuffix(name, tmpSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) <= staleTemp {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// Due reports whether a network check is needed: never checked, or
// now - CheckedAt >= 24h (a CheckedAt in the future also counts as due).
func Due(s State, now time.Time) bool {
	if s.CheckedAt.IsZero() || s.CheckedAt.After(now) {
		return true
	}
	return now.Sub(s.CheckedAt) >= checkInterval
}

// Failed records a failed fetch: CheckedAt = now, so an offline machine
// retries at most daily, and the cached release is kept.
func Failed(s State, now time.Time) State {
	s.CheckedAt = now
	return s
}

// Outcome of applying a fetched or cached release to the state.
type Outcome struct {
	Available Release // zero when the running version is current
	Toast     bool    // show the one-time toast
	State     State   // updated state to save
}

// Apply records a successful fetch (fetched != nil, sets CheckedAt = now)
// or uses the cache (fetched == nil) and decides what to show for current:
// Available when the newest release seen is newer than current, and Toast
// the first time that version is available.
func Apply(s State, fetched *Release, current string, now time.Time) Outcome {
	if fetched != nil {
		s.Latest = fetched.Version
		s.CheckedAt = now
	}
	var out Outcome
	if Newer(s.Latest, current) {
		out.Available = Release{Version: s.Latest}
		if s.Latest != s.Notified {
			out.Toast = true
			s.Notified = s.Latest
		}
	}
	out.State = s
	return out
}
