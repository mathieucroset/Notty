package syncer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/meta"
)

func writePins(t *testing.T, r *gitsync.Repo, pins ...string) {
	t.Helper()
	gittest.Write(t, r, ".notty/state.json", string((&meta.State{Pins: pins}).Marshal()))
}

func readPins(t *testing.T, r *gitsync.Repo) []string {
	t.Helper()
	st, err := meta.Parse([]byte(gittest.Read(t, r, ".notty/state.json")))
	if err != nil {
		t.Fatalf("parse state.json: %v", err)
	}
	return st.Pins
}

// trashNote moves rel into .trash/<id>/ with its meta.json, like vault.Trash.
func trashNote(t *testing.T, r *gitsync.Repo, rel, id, host string, isDir bool) {
	t.Helper()
	gittest.Move(t, r, rel, ".trash/"+id+"/"+filepath.Base(rel))
	m, err := json.Marshal(map[string]any{
		"original_path": rel, "deleted_at": "2026-09-29T10:15:00Z", "host": host, "is_dir": isDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	gittest.Write(t, r, ".trash/"+id+"/meta.json", string(m))
}

func exists(r *gitsync.Repo, rel string) bool {
	_, err := os.Stat(filepath.Join(r.Dir, filepath.FromSlash(rel)))
	return err == nil
}

func TestPinsConflictAutoMerges(t *testing.T) {
	tests := []struct {
		name       string
		base       []string // nil: state.json is created on both sides
		lap, desk  []string
		want       []string
		textConfl  bool
		wantStatus State
	}{
		{"both add", []string{"a.md"}, []string{"a.md", "b.md"}, []string{"a.md", "c.md"}, []string{"a.md", "b.md", "c.md"}, false, Synced},
		{"add and remove", []string{"a.md", "b.md"}, []string{"b.md", "a.md", "x.md"}, []string{"a.md", "y.md"}, []string{"a.md", "x.md", "y.md"}, false, Synced},
		{"created on both", nil, []string{"x.md"}, []string{"y.md"}, []string{"x.md", "y.md"}, false, Synced},
		{"with a text conflict", []string{"a.md"}, []string{"a.md", "b.md"}, []string{"a.md", "c.md"}, []string{"a.md", "b.md", "c.md"}, true, Conflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := gittest.New(t)
			shareBase(t, env, func(r *gitsync.Repo) {
				gittest.Write(t, r, "note.md", "line\n")
				if tt.base != nil {
					writePins(t, r, tt.base...)
				}
			})
			h := newHarness(t, env.Laptop)
			h.start()
			deskPush(t, env, func(r *gitsync.Repo) {
				writePins(t, r, tt.desk...)
				if tt.textConfl {
					gittest.Write(t, r, "note.md", "desktop\n")
				}
			})
			writePins(t, env.Laptop, tt.lap...)
			if tt.textConfl {
				gittest.Write(t, env.Laptop, "note.md", "laptop\n")
			}
			h.rec.reset()
			h.s.SyncNow()
			h.s.waitIdle()

			h.wantState(tt.wantStatus)
			if got := readPins(t, env.Laptop); !slices.Equal(got, tt.want) {
				t.Fatalf("pins = %v, want %v", got, tt.want)
			}
			if tt.textConfl {
				updates := h.rec.all()
				if got := updates[len(updates)-1].Conflicted; !slices.Equal(got, []string{"note.md"}) {
					t.Fatalf("Conflicted = %v, want only note.md (state.json auto-resolved)", got)
				}
				cs, _ := env.Laptop.ConflictedFiles()
				if len(cs) != 1 || cs[0].Path != "note.md" {
					t.Fatalf("unmerged = %+v, want only note.md", cs)
				}
				return
			}
			if env.Laptop.MergeInProgress() {
				t.Fatalf("merge still in progress")
			}
			if got := remoteSubject(t, env); got != "Merge · laptop" {
				t.Fatalf("remote subject = %q, want the merge commit", got)
			}
			for _, u := range h.rec.all() {
				if u.Status.State == Conflict {
					t.Fatalf("entered Conflict for an auto-resolvable pins conflict")
				}
			}
		})
	}
}

func TestBothTrashedAutoResolves(t *testing.T) {
	const lapID, deskID = "20260929101500-laptop-ab12", "20260929101600-desktop-cd34"
	base := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	tests := []struct {
		name      string
		deskEdit  string // desktop's content before trashing ("" = unchanged)
		keepTheir bool
	}{
		{"identical contents keep only ours", "", false},
		{"different contents keep both", base + "line 6 from desktop\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := gittest.New(t)
			shareBase(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "Standup.md", base) })
			h := newHarness(t, env.Laptop)
			h.start()
			deskPush(t, env, func(r *gitsync.Repo) {
				if tt.deskEdit != "" {
					gittest.Write(t, r, "Standup.md", tt.deskEdit)
				}
				trashNote(t, r, "Standup.md", deskID, "desktop", false)
			})
			trashNote(t, env.Laptop, "Standup.md", lapID, "laptop", false)
			h.rec.reset()
			h.s.SyncNow()
			h.s.waitIdle()

			h.wantState(Synced)
			if env.Laptop.MergeInProgress() {
				t.Fatalf("merge still in progress")
			}
			ours := ".trash/" + lapID + "/Standup.md"
			theirs := ".trash/" + deskID + "/Standup.md"
			if got := gittest.Read(t, env.Laptop, ours); got != base {
				t.Fatalf("our trash item = %q, want our content", got)
			}
			if exists(env.Laptop, "Standup.md") {
				t.Fatalf("original path still exists")
			}
			if tt.keepTheir {
				if got := gittest.Read(t, env.Laptop, theirs); got != tt.deskEdit {
					t.Fatalf("their trash item = %q, want their content", got)
				}
				if !exists(env.Laptop, ".trash/"+deskID+"/meta.json") {
					t.Fatalf("their meta.json was removed")
				}
			} else if exists(env.Laptop, ".trash/"+deskID) {
				t.Fatalf("their identical trash item was kept")
			}
			if st := gittest.Git(t, env.Laptop.Dir, "status", "--porcelain"); st != "" {
				t.Fatalf("working tree not clean after auto-resolution:\n%s", st)
			}
			if got := remoteSubject(t, env); got != "Merge · laptop" {
				t.Fatalf("remote subject = %q", got)
			}
			if ws := h.rec.trashWarnings(); len(ws) != 0 {
				t.Fatalf("trash warnings for a note trashed on both sides: %+v", ws)
			}
		})
	}
}
