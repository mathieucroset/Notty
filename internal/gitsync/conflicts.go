package gitsync

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ConflictKind is the two-letter porcelain code of an unmerged path.
type ConflictKind string

// Conflict kinds, named after `git status` codes (spec §7).
const (
	BothModified  ConflictKind = "UU" // stages 1, 2, 3
	DeletedByThem ConflictKind = "UD" // stages 1, 2: we modified, they deleted
	DeletedByUs   ConflictKind = "DU" // stages 1, 3: we deleted, they modified
	BothAdded     ConflictKind = "AA" // stages 2, 3: created on both sides
	AddedByUs     ConflictKind = "AU" // stage 2 only
	AddedByThem   ConflictKind = "UA" // stage 3 only
	BothDeleted   ConflictKind = "DD" // stage 1 only
)

// Conflict is one unmerged path from `git ls-files -u`.
type Conflict struct {
	Path string
	Kind ConflictKind
	// Blob[1], Blob[2] and Blob[3] are the blob IDs of the base, ours and
	// theirs stages; "" when that stage is absent. Blob[0] is unused.
	Blob [4]string
}

// ConflictedFiles lists the unmerged paths of the merge in progress, grouped
// by path in git's order. The kind is derived from which stages exist and
// cross-checked with the "u" entries of `git status --porcelain=v2`.
func (r *Repo) ConflictedFiles() ([]Conflict, error) {
	res, err := r.git("ls-files", "-u", "-z")
	if err != nil {
		return nil, fmt.Errorf("gitsync: list conflicts: %w", err)
	}
	conflicts, err := parseLsFilesUnmerged(string(res.Stdout))
	if err != nil {
		return nil, fmt.Errorf("gitsync: list conflicts: %w", err)
	}
	if len(conflicts) == 0 {
		return nil, nil
	}
	status, err := r.Status()
	if err != nil {
		return nil, fmt.Errorf("gitsync: list conflicts: %w", err)
	}
	xy := map[string]string{}
	for _, e := range status {
		if e.Kind == 'u' {
			xy[e.Path] = e.XY
		}
	}
	for i := range conflicts {
		if k, ok := xy[conflicts[i].Path]; ok && k != string(conflicts[i].Kind) && isConflictKind(k) {
			conflicts[i].Kind = ConflictKind(k)
		}
	}
	return conflicts, nil
}

func isConflictKind(s string) bool {
	switch ConflictKind(s) {
	case BothModified, DeletedByThem, DeletedByUs, BothAdded, AddedByUs, AddedByThem, BothDeleted:
		return true
	}
	return false
}

// parseLsFilesUnmerged parses `git ls-files -u -z` ("<mode> <blob> <stage>\t<path>").
func parseLsFilesUnmerged(out string) ([]Conflict, error) {
	var conflicts []Conflict
	index := map[string]int{}
	for rec := range strings.SplitSeq(out, "\x00") {
		if rec == "" {
			continue
		}
		meta, path, ok := strings.Cut(rec, "\t")
		f := strings.Fields(meta)
		if !ok || len(f) != 3 || len(f[2]) != 1 || f[2][0] < '1' || f[2][0] > '3' {
			return nil, fmt.Errorf("malformed ls-files record %q", rec)
		}
		i, seen := index[path]
		if !seen {
			i = len(conflicts)
			index[path] = i
			conflicts = append(conflicts, Conflict{Path: path})
		}
		conflicts[i].Blob[f[2][0]-'0'] = f[1]
	}
	for i := range conflicts {
		conflicts[i].Kind = kindFromStages(conflicts[i].Blob)
	}
	return conflicts, nil
}

func kindFromStages(b [4]string) ConflictKind {
	base, ours, theirs := b[1] != "", b[2] != "", b[3] != ""
	switch {
	case base && ours && theirs:
		return BothModified
	case base && ours:
		return DeletedByThem
	case base && theirs:
		return DeletedByUs
	case ours && theirs:
		return BothAdded
	case ours:
		return AddedByUs
	case theirs:
		return AddedByThem
	default:
		return BothDeleted
	}
}

// Show returns the content of path at index stage 1 (base), 2 (ours) or 3
// (theirs) during a merge (`git show :N:path`).
func (r *Repo) Show(stage int, path string) ([]byte, error) {
	if stage < 1 || stage > 3 {
		return nil, fmt.Errorf("gitsync: show: invalid stage %d", stage)
	}
	return r.catBlob(fmt.Sprintf(":%d:%s", stage, path))
}

// ShowBlob returns the content of a blob by ID.
func (r *Repo) ShowBlob(id string) ([]byte, error) {
	if err := checkArg("blob", id); err != nil {
		return nil, err
	}
	return r.catBlob(id)
}

func (r *Repo) catBlob(obj string) ([]byte, error) {
	res, err := r.git("cat-file", "blob", obj)
	if err != nil {
		return nil, fmt.Errorf("gitsync: show %s: %w", obj, err)
	}
	return res.Stdout, nil
}

// IsBinary reports whether path looks binary: a NUL byte in the first 8000
// bytes of its stage-2 content, else its stage-3 content, else the working
// file. It returns false when none of them can be read.
func (r *Repo) IsBinary(path string) bool {
	for _, stage := range []int{2, 3} {
		if b, err := r.Show(stage, path); err == nil {
			return looksBinary(b)
		}
	}
	f, err := os.Open(filepath.Join(r.Dir, filepath.FromSlash(path)))
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8000)
	n, _ := f.Read(buf)
	return looksBinary(buf[:n])
}

func looksBinary(b []byte) bool {
	if len(b) > 8000 {
		b = b[:8000]
	}
	return bytes.IndexByte(b, 0) >= 0
}

// PathConflictKind classifies a rename/rename conflict (spec §7).
type PathConflictKind int

const (
	// BothTrashed: both sides moved the note into (different) .trash/ items.
	BothTrashed PathConflictKind = iota
	// TrashVsMove: one side trashed the note, the other moved or renamed it.
	TrashVsMove
	// RenameRename: both sides moved or renamed it to different places.
	RenameRename
)

func (k PathConflictKind) String() string {
	switch k {
	case BothTrashed:
		return "BothTrashed"
	case TrashVsMove:
		return "TrashVsMove"
	case RenameRename:
		return "RenameRename"
	}
	return fmt.Sprintf("PathConflictKind(%d)", int(k))
}

// PathConflict is a rename/rename conflict: git reports a DD entry at
// Original, an AU entry at Ours and a UA entry at Theirs.
//
// OursBlob and TheirsBlob are the blobs committed on each side (HEAD:Ours and
// MERGE_HEAD:Theirs), so each side's own edits are preserved. They are not
// the index stage blobs: for rename/rename, git writes the content merge of
// both sides (possibly with conflict markers) to both new paths, in the index
// and in the working tree. A resolver that keeps a path should write that
// side's blob to it before staging it.
type PathConflict struct {
	Kind       PathConflictKind
	Original   string
	Ours       string
	Theirs     string
	OursBlob   string
	TheirsBlob string
}

const trashPrefix = ".trash/"

func pathConflictKind(ours, theirs string) PathConflictKind {
	ot, tt := strings.HasPrefix(ours, trashPrefix), strings.HasPrefix(theirs, trashPrefix)
	switch {
	case ot && tt:
		return BothTrashed
	case ot || tt:
		return TrashVsMove
	}
	return RenameRename
}

// PathConflicts returns the rename/rename path conflicts of the merge in
// progress, plus the remaining ordinary conflicts.
//
// Pairs are found, in order, from: the "CONFLICT (rename/rename)" lines of the
// last Merge run through this Repo (when it is still the merge in progress);
// rename detection between the merge base and each side (which also works
// after an app restart); side blobs equal to the base blob; and finally, when
// exactly one DD and one AU/UA remain, those.
//
// This replaces the plan's GroupPathConflicts(conflicts): pairing needs the
// merge output and the committed side blobs, so it must be a Repo method.
func (r *Repo) PathConflicts() ([]PathConflict, []Conflict, error) {
	conflicts, err := r.ConflictedFiles()
	if err != nil {
		return nil, nil, err
	}
	pcs, rest := groupPathConflicts(conflicts, r.recordedMergeOutput(), &repoSides{r: r})
	return pcs, rest, nil
}

// sideInfo answers questions about the two sides of the merge in progress.
type sideInfo interface {
	oursRenames() map[string]string   // original path -> our new path
	theirsRenames() map[string]string // original path -> their new path
	oursBlob(path string) string      // blob of HEAD:path, "" if none
	theirsBlob(path string) string    // blob of MERGE_HEAD:path, "" if none
}

type repoSides struct {
	r          *Repo
	loaded     bool
	ours, thrs map[string]string
}

func (s *repoSides) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.ours, s.thrs = map[string]string{}, map[string]string{}
	base, err := s.r.MergeBase("HEAD", "MERGE_HEAD")
	if err != nil {
		return
	}
	for rev, m := range map[string]map[string]string{"HEAD": s.ours, "MERGE_HEAD": s.thrs} {
		changes, err := s.r.DiffNameStatus(base, rev)
		if err != nil {
			continue
		}
		for _, c := range changes {
			if c.Status == 'R' {
				m[c.OldPath] = c.Path
			}
		}
	}
}

func (s *repoSides) oursRenames() map[string]string   { s.load(); return s.ours }
func (s *repoSides) theirsRenames() map[string]string { s.load(); return s.thrs }
func (s *repoSides) oursBlob(p string) string         { return s.r.revBlob("HEAD", p) }
func (s *repoSides) theirsBlob(p string) string       { return s.r.revBlob("MERGE_HEAD", p) }

func (r *Repo) revBlob(rev, path string) string { return r.revParse(rev + ":" + path) }

// groupPathConflicts pairs DD entries with AU/UA entries (see PathConflicts).
func groupPathConflicts(conflicts []Conflict, mergeOutput string, sides sideInfo) ([]PathConflict, []Conflict) {
	var dds []Conflict
	aus, uas := map[string]Conflict{}, map[string]Conflict{}
	for _, c := range conflicts {
		switch c.Kind {
		case BothDeleted:
			dds = append(dds, c)
		case AddedByUs:
			aus[c.Path] = c
		case AddedByThem:
			uas[c.Path] = c
		}
	}
	if len(dds) == 0 || len(aus) == 0 || len(uas) == 0 {
		return nil, conflicts
	}

	type pair struct{ dd, ours, theirs string }
	pairs := make([]pair, len(dds))
	usedAU, usedUA := map[string]bool{}, map[string]bool{}
	claim := func(i int, ours, theirs string) {
		if ours != "" && pairs[i].ours == "" && !usedAU[ours] {
			pairs[i].ours, usedAU[ours] = ours, true
		}
		if theirs != "" && pairs[i].theirs == "" && !usedUA[theirs] {
			pairs[i].theirs, usedUA[theirs] = theirs, true
		}
	}
	for i, dd := range dds {
		pairs[i].dd = dd.Path
	}
	// 1. git's own merge output.
	for i, dd := range dds {
		if a, b, ok := parseRenameRename(mergeOutput, dd.Path, aus, uas); ok {
			claim(i, a, b)
		}
	}
	// 2. rename detection against the merge base.
	for i, dd := range dds {
		if p := sides.oursRenames()[dd.Path]; aus[p].Path != "" {
			claim(i, p, "")
		}
		if p := sides.theirsRenames()[dd.Path]; uas[p].Path != "" {
			claim(i, "", p)
		}
	}
	// 3. a side's committed blob (or stage blob) equals the base blob.
	for i, dd := range dds {
		base := dd.Blob[1]
		if pairs[i].ours == "" {
			for _, p := range sortedKeys(aus) {
				if !usedAU[p] && (sides.oursBlob(p) == base || aus[p].Blob[2] == base) {
					claim(i, p, "")
					break
				}
			}
		}
		if pairs[i].theirs == "" {
			for _, p := range sortedKeys(uas) {
				if !usedUA[p] && (sides.theirsBlob(p) == base || uas[p].Blob[3] == base) {
					claim(i, "", p)
					break
				}
			}
		}
	}
	// 4. exactly one incomplete DD and one unused AU/UA for its missing sides.
	var incomplete []int
	for i := range pairs {
		if pairs[i].ours == "" || pairs[i].theirs == "" {
			incomplete = append(incomplete, i)
		}
	}
	if len(incomplete) == 1 {
		i := incomplete[0]
		if left := unused(aus, usedAU); pairs[i].ours == "" && len(left) == 1 {
			claim(i, left[0], "")
		}
		if left := unused(uas, usedUA); pairs[i].theirs == "" && len(left) == 1 {
			claim(i, "", left[0])
		}
	}

	var pcs []PathConflict
	grouped := map[string]bool{}
	for _, p := range pairs {
		if p.ours == "" || p.theirs == "" {
			continue
		}
		ob := sides.oursBlob(p.ours)
		if ob == "" {
			ob = aus[p.ours].Blob[2]
		}
		tb := sides.theirsBlob(p.theirs)
		if tb == "" {
			tb = uas[p.theirs].Blob[3]
		}
		pcs = append(pcs, PathConflict{
			Kind:       pathConflictKind(p.ours, p.theirs),
			Original:   p.dd,
			Ours:       p.ours,
			Theirs:     p.theirs,
			OursBlob:   ob,
			TheirsBlob: tb,
		})
		grouped[p.dd], grouped[p.ours], grouped[p.theirs] = true, true, true
	}
	var rest []Conflict
	for _, c := range conflicts {
		if !grouped[c.Path] {
			rest = append(rest, c)
		}
	}
	return pcs, rest
}

// parseRenameRename finds git's line
//
//	CONFLICT (rename/rename): <orig> renamed to <ours> in <label> and to <theirs> in <label>.
//
// for orig, matching the new paths against the known AU/UA paths so that
// paths containing spaces or " in " are handled.
func parseRenameRename(out, orig string, aus, uas map[string]Conflict) (ours, theirs string, ok bool) {
	prefix := "CONFLICT (rename/rename): " + orig + " renamed to "
	for line := range strings.SplitSeq(out, "\n") {
		rest, found := strings.CutPrefix(line, prefix)
		if !found {
			continue
		}
		for a := range aus {
			afterA, found := strings.CutPrefix(rest, a+" in ")
			if !found {
				continue
			}
			// Labels are ref names ("HEAD", "origin/main"): no spaces.
			label, afterLabel, found := strings.Cut(afterA, " and to ")
			if !found || strings.Contains(label, " ") {
				continue
			}
			for b := range uas {
				if strings.HasPrefix(afterLabel, b+" in ") {
					return a, b, true
				}
			}
		}
	}
	return "", "", false
}

func sortedKeys(m map[string]Conflict) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func unused(m map[string]Conflict, used map[string]bool) []string {
	var left []string
	for _, k := range sortedKeys(m) {
		if !used[k] {
			left = append(left, k)
		}
	}
	return left
}
