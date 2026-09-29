package syncer

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/mathieucroset/notty/internal/gitsync"
)

const trashDir = ".trash/"

// maxNamedFiles is how many file names a commit message lists before it
// switches to a count (spec §7).
const maxNamedFiles = 3

// CommitMessage builds the subject of an automatic commit (spec §7):
// "Update a.md, b.md · host", or "Update 7 notes · host" for more than three
// files. When every change is of one kind the verb is "Create", "Delete",
// "Move" or "Restore" instead: moving a note into .trash/ is a delete, moving
// it out of .trash/ is a restore. The per-item .trash/<id>/meta.json files are
// not named, since they only accompany a trash or restore.
func CommitMessage(changes []gitsync.Change, host string) string {
	type item struct{ verb, name, key string }
	var items, metaOnly []item
	for _, c := range changes {
		it := item{verb: "Update", name: path.Base(c.Path), key: c.Path}
		switch c.Status {
		case 'A':
			it.verb = "Create"
		case 'D':
			it.verb = "Delete"
		case 'R':
			from, to := inTrash(c.OldPath), inTrash(c.Path)
			switch {
			case !from && to:
				it.verb, it.name = "Delete", path.Base(c.OldPath)
			case from && !to:
				it.verb = "Restore"
			default:
				it.verb = "Move"
			}
		}
		if isTrashMeta(c.Path) {
			metaOnly = append(metaOnly, item{verb: "Update", name: it.name, key: it.key})
			continue
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		items = metaOnly
	}
	slices.SortFunc(items, func(a, b item) int { return strings.Compare(a.key, b.key) })

	verb := "Update"
	for i, it := range items {
		if i == 0 {
			verb = it.verb
		} else if it.verb != verb {
			verb = "Update"
			break
		}
	}
	var what string
	switch {
	case len(items) == 0:
		return fmt.Sprintf("%s · %s", verb, host)
	case len(items) > maxNamedFiles:
		what = fmt.Sprintf("%d notes", len(items))
	default:
		names := make([]string, len(items))
		for i, it := range items {
			names[i] = it.name
		}
		what = strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s %s · %s", verb, what, host)
}

func inTrash(p string) bool { return strings.HasPrefix(p, trashDir) }

// isTrashMeta reports whether p is a trash item's .trash/<id>/meta.json.
func isTrashMeta(p string) bool {
	rest, ok := strings.CutPrefix(p, trashDir)
	if !ok {
		return false
	}
	id, file, ok := strings.Cut(rest, "/")
	return ok && id != "" && file == "meta.json"
}
