package syncer

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mathieucroset/notty/internal/gitsync"
)

// trashRepo is what trash detection needs from the repository.
type trashRepo interface {
	MergeBase(a, b string) (string, error)
	DiffNameStatus(from, to string) ([]gitsync.Change, error)
	ShowAt(rev, path string) ([]byte, error)
	LastCommitAdding(path string) (string, error)
	LastCommitHostFor(path, before string) (string, error)
}

// trashMeta is the part of .trash/<id>/meta.json the syncer reads (written by
// vault.Trash).
type trashMeta struct {
	OriginalPath string `json:"original_path"`
	Host         string `json:"host"`
}

// DetectTrashWarnings runs after a merge or fast-forward from origHead (the
// commit before it) to HEAD (spec §7). For each trash item that arrived under
// .trash/<id>/, it warns if the original path was modified locally since the
// merge base, or if its last change before the deletion was committed from
// host. It returns one warning per trash item (a trashed folder gives one
// warning), sorted by trash ID. Items whose original path was already gone
// locally (trashed on both sides) are skipped.
func DetectTrashWarnings(repo *gitsync.Repo, origHead, host string) ([]TrashWarning, error) {
	return detectTrashWarnings(repo, repo.Dir, origHead, host)
}

func detectTrashWarnings(r trashRepo, dir, origHead, host string) ([]TrashWarning, error) {
	orig := revID(r, origHead)
	if orig == "" {
		return nil, fmt.Errorf("syncer: trash check: unknown revision %q", origHead)
	}
	// The other side: HEAD itself after a fast-forward, HEAD^2 when HEAD is
	// the merge commit on top of origHead.
	theirs := "HEAD"
	if revID(r, "HEAD^1") == orig && revID(r, "HEAD^2") != "" {
		theirs = "HEAD^2"
	}
	base, err := r.MergeBase(orig, theirs)
	if err != nil {
		return nil, fmt.Errorf("syncer: trash check: %w", err)
	}
	arrived, err := r.DiffNameStatus(orig, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("syncer: trash check: %w", err)
	}
	local, err := r.DiffNameStatus(base, orig)
	if err != nil {
		return nil, fmt.Errorf("syncer: trash check: %w", err)
	}
	editedHere := map[string]bool{}
	for _, c := range local {
		// A rename to the path counts too: git reports a rename with edits
		// as R, and the content at that path changed here.
		if c.Status == 'M' || c.Status == 'A' || c.Status == 'R' {
			editedHere[c.Path] = true
		}
	}

	// Files that arrived in the trash, grouped by trash item.
	items := map[string][]string{} // id -> paths inside .trash/<id>/
	for _, c := range arrived {
		if c.Status != 'A' && c.Status != 'R' {
			continue
		}
		rest, ok := strings.CutPrefix(c.Path, trashDir)
		if !ok || isTrashMeta(c.Path) {
			continue
		}
		id, inner, ok := strings.Cut(rest, "/")
		if !ok || id == "" || inner == "" {
			continue
		}
		items[id] = append(items[id], inner)
	}
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	var warnings []TrashWarning
	for _, id := range ids {
		m, ok := readTrashMeta(dir, id)
		if !ok {
			continue
		}
		name := path.Base(m.OriginalPath)
		for _, inner := range items[id] {
			// .trash/<id>/<name>[/sub/path] was <dir of original>/<name>[/sub/path].
			if inner != name && !strings.HasPrefix(inner, name+"/") {
				continue
			}
			was := path.Join(path.Dir(m.OriginalPath), inner)
			if _, err := r.ShowAt(orig, was); err != nil {
				continue // already gone here: trashed on both sides
			}
			if editedHere[was] || (host != "" && lastChangeFrom(r, trashDir+id+"/"+inner, was) == host) {
				warnings = append(warnings, TrashWarning{
					Path:      m.OriginalPath,
					TrashPath: trashDir + id + "/" + name,
					Host:      m.Host,
				})
				break
			}
		}
	}
	return warnings, nil
}

// lastChangeFrom returns the host of the last commit that changed was before
// the commit that moved it to trashed, or "" if that commit is not found.
// The trashing commit is the one that added trashed (trash IDs are unique).
func lastChangeFrom(r trashRepo, trashed, was string) string {
	trashCommit, err := r.LastCommitAdding(trashed)
	if err != nil || trashCommit == "" {
		return ""
	}
	h, err := r.LastCommitHostFor(was, trashCommit+"^")
	if err != nil {
		return ""
	}
	return h
}

// revID resolves a commit-ish to its ID, or "" if it does not exist. The
// merge base of a commit with itself is that commit.
func revID(r interface {
	MergeBase(a, b string) (string, error)
}, rev string) string {
	id, err := r.MergeBase(rev, rev)
	if err != nil {
		return ""
	}
	return id
}

func readTrashMeta(dir, id string) (trashMeta, bool) {
	data, err := os.ReadFile(filepath.Join(dir, ".trash", id, "meta.json"))
	if err != nil {
		return trashMeta{}, false
	}
	var m trashMeta
	if err := json.Unmarshal(data, &m); err != nil || m.OriginalPath == "" {
		return trashMeta{}, false
	}
	m.OriginalPath = filepath.ToSlash(m.OriginalPath)
	return m, true
}
