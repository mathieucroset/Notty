package syncer

import "fmt"

// State is the syncer's state (spec §7).
type State int

// Syncer states. Committing, Pulling, Merging and Pushing are shown as
// "syncing" in the status bar (spec §4.3).
const (
	LocalOnly State = iota // no remote, or sync.enabled = false: commits only
	Idle                   // not started yet
	Committing
	Pulling
	Merging
	Pushing
	Synced
	Offline  // the remote is unreachable; retrying with backoff
	Conflict // a merge is in progress with unresolved conflicts
	Error
)

func (s State) String() string {
	switch s {
	case LocalOnly:
		return "LocalOnly"
	case Idle:
		return "Idle"
	case Committing:
		return "Committing"
	case Pulling:
		return "Pulling"
	case Merging:
		return "Merging"
	case Pushing:
		return "Pushing"
	case Synced:
		return "Synced"
	case Offline:
		return "Offline"
	case Conflict:
		return "Conflict"
	case Error:
		return "Error"
	}
	return fmt.Sprintf("State(%d)", int(s))
}

// Status is the syncer's state as shown to the user.
type Status struct {
	State     State
	Pending   int    // Offline: local commits not pushed yet
	Conflicts int    // Conflict: number of conflicted files
	Err       error  // Error: the failure
	Detail    string // "auth" for an authentication failure
}

// TrashWarning reports a note that another computer moved to the trash
// although it was edited on this one (spec §7).
type TrashWarning struct {
	Path      string // the note's original path
	TrashPath string // where it is now: .trash/<id>/<name>
	Host      string // the host that trashed it
}

// Update is sent to the UI on every state change and after every merge.
//
// Updates are delivered asynchronously through Syncer.Updates, while Host
// calls are made synchronously from the syncer's worker. The UI must
// therefore not rely on their relative order: the Update carrying a merge's
// Reindex/Reload (or its Conflict and Conflicted) may arrive before or after
// the matching Host.UnlockMutations call.
type Update struct {
	Status Status
	// Reindex lists every path the last merge changed, including the old
	// path of renames (spec §7 "Re-index after merge").
	Reindex []string
	// Reload lists the reindexed paths that still exist; the UI decides for
	// each open note whether to reload it.
	Reload        []string
	TrashWarnings []TrashWarning
	// Conflicted is the full set of conflicted paths while in Conflict.
	Conflicted []string
}
