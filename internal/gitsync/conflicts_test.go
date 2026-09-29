package gitsync_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
)

// diverge commits base on the laptop and syncs it to the desktop, then applies
// desk (committed and pushed) and lap (committed) and merges origin/main into
// the laptop, returning the merge error.
func diverge(t *testing.T, env *gittest.Env, base, lap, desk func(r *gitsync.Repo)) error {
	t.Helper()
	if base != nil {
		base(env.Laptop)
		gittest.CommitAll(t, env.Laptop, "base · laptop")
		gittest.Push(t, env.Laptop)
		gittest.Sync(t, env.Desktop)
	}
	desk(env.Desktop)
	gittest.CommitAll(t, env.Desktop, "desktop change · desktop")
	gittest.Push(t, env.Desktop)
	lap(env.Laptop)
	gittest.CommitAll(t, env.Laptop, "laptop change · laptop")
	if err := env.Laptop.Fetch(ctx); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	return env.Laptop.Merge("origin/main", false)
}

func mustConflict(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, gitsync.ErrConflict) {
		t.Fatalf("merge err = %v, want ErrConflict", err)
	}
}

func conflictMap(t *testing.T, r *gitsync.Repo) map[string]gitsync.Conflict {
	t.Helper()
	cs, err := r.ConflictedFiles()
	if err != nil {
		t.Fatalf("ConflictedFiles: %v", err)
	}
	m := map[string]gitsync.Conflict{}
	for _, c := range cs {
		m[c.Path] = c
	}
	return m
}

func TestTextConflictUU(t *testing.T) {
	env := gittest.New(t)
	err := diverge(t, env,
		func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "line\n") },
		func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "laptop\n") },
		func(r *gitsync.Repo) {
			gittest.Write(t, r, "note.md", "desktop\n")
			gittest.Write(t, r, "clean.md", "clean from desktop\n")
		})
	mustConflict(t, err)
	lap := env.Laptop

	cs, err := lap.ConflictedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Path != "note.md" || cs[0].Kind != gitsync.BothModified {
		t.Fatalf("ConflictedFiles = %+v, want one UU note.md", cs)
	}
	for stage, want := range map[int]string{1: "line\n", 2: "laptop\n", 3: "desktop\n"} {
		if cs[0].Blob[stage] == "" {
			t.Fatalf("Blob[%d] empty", stage)
		}
		got, err := lap.Show(stage, "note.md")
		if err != nil || string(got) != want {
			t.Errorf("Show(%d) = %q, %v; want %q", stage, got, err, want)
		}
		blob, err := lap.ShowBlob(cs[0].Blob[stage])
		if err != nil || string(blob) != want {
			t.Errorf("ShowBlob(Blob[%d]) = %q, %v; want %q", stage, blob, err, want)
		}
	}
	if lap.IsBinary("note.md") {
		t.Errorf("IsBinary(note.md) = true")
	}

	// Re-index source: the working tree vs ORIG_HEAD includes the file merged
	// cleanly as part of the conflicted merge.
	changes, err := lap.DiffNameStatus("ORIG_HEAD", "")
	if err != nil {
		t.Fatalf("DiffNameStatus: %v", err)
	}
	if !slices.Contains(changes, gitsync.Change{Status: 'A', Path: "clean.md"}) {
		t.Fatalf("DiffNameStatus(ORIG_HEAD, \"\") = %+v, want clean.md added", changes)
	}
	if !slices.ContainsFunc(changes, func(c gitsync.Change) bool { return c.Path == "note.md" }) {
		t.Fatalf("DiffNameStatus(ORIG_HEAD, \"\") = %+v, want note.md", changes)
	}

	pcs, rest, err := lap.PathConflicts()
	if err != nil || len(pcs) != 0 || len(rest) != 1 {
		t.Fatalf("PathConflicts = %+v, %+v, %v; want none and one ordinary", pcs, rest, err)
	}
}

func TestBinaryConflict(t *testing.T) {
	env := gittest.New(t)
	err := diverge(t, env,
		func(r *gitsync.Repo) { gittest.Write(t, r, "attachments/img.png", "\x89PNG\x00base") },
		func(r *gitsync.Repo) { gittest.Write(t, r, "attachments/img.png", "\x89PNG\x00laptop") },
		func(r *gitsync.Repo) { gittest.Write(t, r, "attachments/img.png", "\x89PNG\x00desktop") })
	mustConflict(t, err)
	if !env.Laptop.IsBinary("attachments/img.png") {
		t.Fatalf("IsBinary = false for a conflicted binary file")
	}
	if env.Laptop.IsBinary("README.md") {
		t.Fatalf("IsBinary(README.md) = true")
	}
	if env.Laptop.IsBinary("missing.md") {
		t.Fatalf("IsBinary(missing) = true")
	}
}

func TestModifyDeleteConflicts(t *testing.T) {
	t.Run("UD: we modified, they deleted", func(t *testing.T) {
		env := gittest.New(t)
		err := diverge(t, env,
			func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "v1\n") },
			func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "v2 laptop\n") },
			func(r *gitsync.Repo) {
				if err := r.Remove("note.md"); err != nil {
					t.Fatal(err)
				}
			})
		mustConflict(t, err)
		c := conflictMap(t, env.Laptop)["note.md"]
		if c.Kind != gitsync.DeletedByThem || c.Blob[1] == "" || c.Blob[2] == "" || c.Blob[3] != "" {
			t.Fatalf("conflict = %+v, want UD with stages 1,2", c)
		}
	})
	t.Run("DU: we deleted, they modified", func(t *testing.T) {
		env := gittest.New(t)
		err := diverge(t, env,
			func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "v1\n") },
			func(r *gitsync.Repo) {
				if err := r.Remove("note.md"); err != nil {
					t.Fatal(err)
				}
			},
			func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "v2 desktop\n") })
		mustConflict(t, err)
		c := conflictMap(t, env.Laptop)["note.md"]
		if c.Kind != gitsync.DeletedByUs || c.Blob[1] == "" || c.Blob[2] != "" || c.Blob[3] == "" {
			t.Fatalf("conflict = %+v, want DU with stages 1,3", c)
		}
		// Resolve as "Keep edited": take theirs and stage it.
		content, err := env.Laptop.Show(3, "note.md")
		if err != nil {
			t.Fatal(err)
		}
		gittest.Write(t, env.Laptop, "note.md", string(content))
		if err := env.Laptop.Add("note.md"); err != nil {
			t.Fatal(err)
		}
		if err := env.Laptop.CommitMerge("Merge · laptop"); err != nil {
			t.Fatalf("CommitMerge: %v", err)
		}
	})
}

func TestCreateCreateAA(t *testing.T) {
	env := gittest.New(t)
	err := diverge(t, env, nil,
		func(r *gitsync.Repo) { gittest.Write(t, r, "Ideas.md", "# Ideas\nlaptop\n") },
		func(r *gitsync.Repo) { gittest.Write(t, r, "Ideas.md", "# Ideas\ndesktop\n") })
	mustConflict(t, err)
	c := conflictMap(t, env.Laptop)["Ideas.md"]
	if c.Kind != gitsync.BothAdded || c.Blob[1] != "" || c.Blob[2] == "" || c.Blob[3] == "" {
		t.Fatalf("conflict = %+v, want AA with stages 2,3", c)
	}
}

const noteBody = "# Standup notes\n\nA note long enough that git rename detection is confident.\n- item one\n- item two\n"

func TestPathConflicts(t *testing.T) {
	tests := []struct {
		name    string
		lapTo   string
		deskTo  string
		lapEdit bool
		want    gitsync.PathConflictKind
	}{
		{"both trashed", ".trash/20260929101500-laptop-ab12/Standup notes.md", ".trash/20260929101600-desktop-cd34/Standup notes.md", true, gitsync.BothTrashed},
		{"trash vs move", ".trash/20260929101500-laptop-ab12/Standup notes.md", "Work/Standup notes.md", false, gitsync.TrashVsMove},
		{"move vs trash", "Archive/Standup notes.md", ".trash/20260929101600-desktop-cd34/Standup notes.md", false, gitsync.TrashVsMove},
		{"rename rename", "Work/Standup in Work.md", "Meetings/Standup notes.md", true, gitsync.RenameRename},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := gittest.New(t)
			const orig = "Standup notes.md"
			err := diverge(t, env,
				func(r *gitsync.Repo) {
					gittest.Write(t, r, orig, noteBody)
					gittest.Write(t, r, "other.md", "other\n")
				},
				func(r *gitsync.Repo) {
					gittest.Move(t, r, orig, tt.lapTo)
					if tt.lapEdit {
						gittest.Write(t, r, tt.lapTo, noteBody+"- laptop edit\n")
					}
					gittest.Write(t, r, "other.md", "other laptop\n")
				},
				func(r *gitsync.Repo) {
					gittest.Move(t, r, orig, tt.deskTo)
					gittest.Write(t, r, "other.md", "other desktop\n")
				})
			mustConflict(t, err)

			check := func(t *testing.T, r *gitsync.Repo) {
				t.Helper()
				pcs, rest, err := r.PathConflicts()
				if err != nil {
					t.Fatalf("PathConflicts: %v", err)
				}
				if len(pcs) != 1 {
					t.Fatalf("PathConflicts = %+v (rest %+v), want exactly one", pcs, rest)
				}
				pc := pcs[0]
				if pc.Kind != tt.want || pc.Original != orig || pc.Ours != tt.lapTo || pc.Theirs != tt.deskTo {
					t.Fatalf("PathConflict = %+v, want %v %q -> ours %q, theirs %q", pc, tt.want, orig, tt.lapTo, tt.deskTo)
				}
				ours, err := r.ShowBlob(pc.OursBlob)
				if err != nil {
					t.Fatal(err)
				}
				theirs, err := r.ShowBlob(pc.TheirsBlob)
				if err != nil {
					t.Fatal(err)
				}
				wantOurs := noteBody
				if tt.lapEdit {
					wantOurs += "- laptop edit\n"
				}
				if string(ours) != wantOurs || string(theirs) != noteBody {
					t.Fatalf("side blobs = %q / %q, want %q / %q", ours, theirs, wantOurs, noteBody)
				}
				if len(rest) != 1 || rest[0].Path != "other.md" || rest[0].Kind != gitsync.BothModified {
					t.Fatalf("rest = %+v, want only other.md UU", rest)
				}
			}
			t.Run("from merge output", func(t *testing.T) {
				if out := gitsync.RecordedMergeOutput(env.Laptop); !strings.Contains(out, "CONFLICT (rename/rename): "+orig) {
					t.Fatalf("recorded merge output = %q", out)
				}
				check(t, env.Laptop)
			})
			// A fresh Repo (as after an app restart) has no recorded output and
			// falls back to rename detection.
			t.Run("after restart", func(t *testing.T) {
				fresh := gitsync.Open(env.Laptop.Dir)
				if out := gitsync.RecordedMergeOutput(fresh); out != "" {
					t.Fatalf("fresh Repo has recorded output %q", out)
				}
				check(t, fresh)
			})

			// Resolve "keep both": the original goes away, each path keeps its
			// side's content.
			pcs, _, _ := env.Laptop.PathConflicts()
			pc := pcs[0]
			for path, blob := range map[string]string{pc.Ours: pc.OursBlob, pc.Theirs: pc.TheirsBlob} {
				b, err := env.Laptop.ShowBlob(blob)
				if err != nil {
					t.Fatal(err)
				}
				gittest.Write(t, env.Laptop, path, string(b))
			}
			gittest.Write(t, env.Laptop, "other.md", "other merged\n")
			if err := env.Laptop.Remove(pc.Original); err != nil {
				t.Fatalf("Remove: %v", err)
			}
			if err := env.Laptop.Add(pc.Ours, pc.Theirs, "other.md"); err != nil {
				t.Fatalf("Add: %v", err)
			}
			if err := env.Laptop.CommitMerge("Merge · laptop"); err != nil {
				t.Fatalf("CommitMerge: %v", err)
			}
			if out := gitsync.RecordedMergeOutput(env.Laptop); out != "" {
				t.Errorf("merge output still recorded after CommitMerge: %q", out)
			}
			for _, p := range []string{pc.Ours, pc.Theirs} {
				if _, err := os.Stat(filepath.Join(env.Laptop.Dir, filepath.FromSlash(p))); err != nil {
					t.Errorf("%s missing after keep-both: %v", p, err)
				}
			}
			if _, err := os.Stat(filepath.Join(env.Laptop.Dir, orig)); !os.IsNotExist(err) {
				t.Errorf("original path still present: %v", err)
			}
		})
	}
}

func TestLogFollowAndHistory(t *testing.T) {
	env := gittest.New(t)
	lap, desk := env.Laptop, env.Desktop
	gittest.Write(t, lap, "Ideas.md", "one\ntwo\nthree\nfour\nfive\n")
	gittest.CommitAll(t, lap, "Create Ideas.md · laptop")
	first := gittest.Git(t, lap.Dir, "rev-parse", "HEAD")
	gittest.Push(t, lap)

	gittest.Sync(t, desk)
	gittest.Write(t, desk, "Ideas.md", "one\ntwo\nthree\nfour\nfive\nsix\n")
	gittest.CommitAll(t, desk, "Update Ideas.md · desktop")
	gittest.Push(t, desk)

	gittest.Sync(t, lap)
	gittest.Move(t, lap, "Ideas.md", "Projects/Ideas.md")
	gittest.CommitAll(t, lap, "Move Ideas.md · laptop")
	gittest.Write(t, lap, "Projects/Ideas.md", "one\ntwo\nTHREE\nfour\nfive\nsix\n")
	gittest.CommitAll(t, lap, "Manual edit without host")

	entries, err := lap.Log("Projects/Ideas.md")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	type row struct {
		subject, host  string
		added, deleted int
	}
	var got []row
	for _, e := range entries {
		got = append(got, row{e.Subject, e.Host, e.Added, e.Deleted})
		if e.Rev == "" || e.Date.IsZero() {
			t.Errorf("entry missing rev/date: %+v", e)
		}
	}
	want := []row{
		{"Manual edit without host", "", 1, 1},
		{"Move Ideas.md · laptop", "laptop", 0, 0},
		{"Update Ideas.md · desktop", "desktop", 1, 0},
		{"Create Ideas.md · laptop", "laptop", 5, 0},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Log = %+v\nwant %+v", got, want)
	}
	if entries[3].Rev != first {
		t.Errorf("oldest rev = %s, want %s", entries[3].Rev, first)
	}

	b, err := lap.ShowAt(first, "Ideas.md")
	if err != nil || string(b) != "one\ntwo\nthree\nfour\nfive\n" {
		t.Fatalf("ShowAt(first, Ideas.md) = %q, %v", b, err)
	}
	if _, err := lap.ShowAt("HEAD", "Ideas.md"); err == nil {
		t.Fatalf("ShowAt of a path absent at HEAD succeeded")
	}

	host, err := lap.LastCommitHostFor("Ideas.md", "HEAD")
	if err != nil || host != "laptop" {
		t.Fatalf("LastCommitHostFor(Ideas.md, HEAD) = %q, %v; want laptop (the move)", host, err)
	}
	host, err = lap.LastCommitHostFor("Ideas.md", "HEAD~2")
	if err != nil || host != "desktop" {
		t.Fatalf("LastCommitHostFor(Ideas.md, HEAD~2) = %q, %v; want desktop", host, err)
	}
	host, err = lap.LastCommitHostFor("never.md", "HEAD")
	if err != nil || host != "" {
		t.Fatalf("LastCommitHostFor(never.md) = %q, %v; want \"\"", host, err)
	}
}

func TestHostFromSubject(t *testing.T) {
	tests := map[string]string{
		"Update a.md · laptop":                         "laptop",
		"Update Standup notes.md, ideas.md · desk-top": "desk-top",
		"Merge · laptop":                               "laptop",
		"Manual commit":                                "",
		"odd · two words":                              "",
		"Update a · b.md · laptop":                     "laptop",
	}
	for subject, want := range tests {
		if got := gitsync.HostFromSubject(subject); got != want {
			t.Errorf("HostFromSubject(%q) = %q, want %q", subject, got, want)
		}
	}
}
