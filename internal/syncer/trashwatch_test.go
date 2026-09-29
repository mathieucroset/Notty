package syncer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
)

const deskTrashID = "20260929101600-desktop-cd34"

func TestTrashWarningLocalEdit(t *testing.T) {
	// The desktop created the note; the laptop edits it while the desktop
	// trashes it. Git follows the rename, so the edit lands in the trash.
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "Standup notes.md", "line 1\nline 2\nline 3\n") })
	h.s.SyncNow()
	h.s.waitIdle()

	deskPush(t, env, func(r *gitsync.Repo) { trashNote(t, r, "Standup notes.md", deskTrashID, "desktop", false) })
	gittest.Write(t, env.Laptop, "Standup notes.md", "line 1\nline 2\nline 3\nlaptop edit\n")
	h.rec.reset()
	h.s.SyncNow()
	h.s.waitIdle()

	h.wantState(Synced)
	trashed := ".trash/" + deskTrashID + "/Standup notes.md"
	if got := gittest.Read(t, env.Laptop, trashed); got != "line 1\nline 2\nline 3\nlaptop edit\n" {
		t.Fatalf("trashed copy = %q, want the laptop edit", got)
	}
	want := []TrashWarning{{Path: "Standup notes.md", TrashPath: trashed, Host: "desktop"}}
	if got := h.rec.trashWarnings(); !slices.Equal(got, want) {
		t.Fatalf("TrashWarnings = %+v, want %+v", got, want)
	}
}

func TestTrashWarningLastChangeFromThisHost(t *testing.T) {
	// The laptop wrote a note and a folder, the desktop synced and trashed
	// both; the laptop receives a fast-forward through the fetch timer.
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	gittest.Write(t, env.Laptop, "Idea.md", "idea\n")
	gittest.Write(t, env.Laptop, "Work/a.md", "a\n")
	gittest.Write(t, env.Laptop, "Work/b.md", "b\n")
	h.s.SyncNow()
	h.s.waitIdle()
	gittest.Sync(t, env.Desktop)

	deskPush(t, env, func(r *gitsync.Repo) {
		trashNote(t, r, "Idea.md", "20260929101600-desktop-aaaa", "desktop", false)
		trashNote(t, r, "Work", "20260929101700-desktop-bbbb", "desktop", true)
	})
	h.rec.reset()
	h.advance(5 * time.Minute)

	want := []TrashWarning{
		{Path: "Idea.md", TrashPath: ".trash/20260929101600-desktop-aaaa/Idea.md", Host: "desktop"},
		{Path: "Work", TrashPath: ".trash/20260929101700-desktop-bbbb/Work", Host: "desktop"},
	}
	if got := h.rec.trashWarnings(); !slices.Equal(got, want) {
		t.Fatalf("TrashWarnings = %+v, want one per trashed item %+v", got, want)
	}
}

// fakeTrashRepo models a merge commit HEAD with parents O (ours) and T
// (theirs) and merge base B, where T trashed note.md.
type fakeTrashRepo struct{ local []gitsync.Change }

func (f *fakeTrashRepo) MergeBase(a, b string) (string, error) {
	revs := map[string]string{"O": "O", "HEAD": "M", "HEAD^1": "O", "HEAD^2": "T"}
	if a == b {
		if id, ok := revs[a]; ok {
			return id, nil
		}
		return "", errors.New("unknown revision")
	}
	return "B", nil
}

func (f *fakeTrashRepo) DiffNameStatus(from, to string) ([]gitsync.Change, error) {
	switch {
	case from == "O" && to == "HEAD":
		return []gitsync.Change{
			{Status: 'R', OldPath: "note.md", Path: ".trash/" + deskTrashID + "/note.md"},
			{Status: 'A', Path: ".trash/" + deskTrashID + "/meta.json"},
		}, nil
	case from == "B" && to == "O":
		return f.local, nil
	}
	return nil, fmt.Errorf("unexpected diff %s..%s", from, to)
}

func (f *fakeTrashRepo) ShowAt(rev, path string) ([]byte, error) {
	if rev == "O" && path == "note.md" {
		return []byte("x"), nil
	}
	return nil, errors.New("missing")
}

func (f *fakeTrashRepo) LastCommitAdding(string) (string, error) { return "D", nil }

func (f *fakeTrashRepo) LastCommitHostFor(string, string) (string, error) { return "desktop", nil }

func TestTrashWarningLocalChangeKinds(t *testing.T) {
	dir := t.TempDir()
	metaDir := filepath.Join(dir, ".trash", deskTrashID)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte(`{"original_path":"note.md","host":"desktop"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		local []gitsync.Change
		warn  bool
	}{
		{"edited", []gitsync.Change{{Status: 'M', Path: "note.md"}}, true},
		{"renamed here with edits", []gitsync.Change{{Status: 'R', OldPath: "draft.md", Path: "note.md"}}, true},
		{"other note edited", []gitsync.Change{{Status: 'M', Path: "other.md"}}, false},
		{"untouched", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, err := detectTrashWarnings(&fakeTrashRepo{local: tt.local}, dir, "O", "laptop")
			if err != nil {
				t.Fatal(err)
			}
			if got := len(ws) == 1; got != tt.warn {
				t.Fatalf("warnings = %+v, want warning: %v", ws, tt.warn)
			}
		})
	}
}

func TestTrashCheckRunsOutsideLockedSection(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	gittest.Write(t, env.Laptop, "mine.md", "mine\n")
	h.s.SyncNow()
	h.s.waitIdle()
	gittest.Sync(t, env.Desktop)
	deskPush(t, env, func(r *gitsync.Repo) { trashNote(t, r, "mine.md", deskTrashID, "desktop", false) })
	h.host.reset()
	h.rec.reset()
	h.s.SyncNow()
	h.s.waitIdle()

	calls := h.host.calls()
	unlock, check := slices.Index(calls, "Unlock"), slices.Index(calls, "LastCommitAdding")
	if unlock < 0 || check < 0 || check < unlock {
		t.Fatalf("calls = %v, want the trash check after Unlock", calls)
	}
	if got := h.rec.trashWarnings(); len(got) != 1 || got[0].Path != "mine.md" {
		t.Fatalf("TrashWarnings = %+v, want one for mine.md", got)
	}
}

func TestNoTrashWarningForRemoteNote(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "desk.md", "desktop only\n") })
	h.s.SyncNow()
	h.s.waitIdle()
	// The desktop edits it again, then trashes it.
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "desk.md", "desktop only\nmore\n") })
	deskPush(t, env, func(r *gitsync.Repo) { trashNote(t, r, "desk.md", deskTrashID, "desktop", false) })
	// Meanwhile the laptop edits another note (a real merge, not a
	// fast-forward).
	gittest.Write(t, env.Laptop, "other.md", "laptop\n")
	h.rec.reset()
	h.s.SyncNow()
	h.s.waitIdle()
	h.wantState(Synced)
	if ws := h.rec.trashWarnings(); len(ws) != 0 {
		t.Fatalf("TrashWarnings = %+v, want none", ws)
	}
}

func TestTrashWarningLastChangeBeforeDeletionIsDesktop(t *testing.T) {
	// The laptop created the note, but the desktop edited it last before
	// trashing it: no warning.
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	gittest.Write(t, env.Laptop, "n.md", "one\ntwo\nthree\n")
	h.s.SyncNow()
	h.s.waitIdle()
	gittest.Sync(t, env.Desktop)
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "n.md", "one\ntwo\nthree\nfour\n") })
	deskPush(t, env, func(r *gitsync.Repo) { trashNote(t, r, "n.md", deskTrashID, "desktop", false) })
	h.rec.reset()
	h.s.SyncNow()
	h.s.waitIdle()
	if ws := h.rec.trashWarnings(); len(ws) != 0 {
		t.Fatalf("TrashWarnings = %+v, want none", ws)
	}
}

func TestDetectTrashWarningsExported(t *testing.T) {
	env := gittest.New(t)
	gittest.Write(t, env.Laptop, "mine.md", "mine\n")
	gittest.CommitAll(t, env.Laptop, "Create mine.md · laptop")
	gittest.Push(t, env.Laptop)
	gittest.Sync(t, env.Desktop)
	deskPush(t, env, func(r *gitsync.Repo) { trashNote(t, r, "mine.md", deskTrashID, "desktop", false) })
	origHead := head(t, env.Laptop)
	gittest.Sync(t, env.Laptop)

	ws, err := DetectTrashWarnings(env.Laptop, origHead, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	want := []TrashWarning{{Path: "mine.md", TrashPath: ".trash/" + deskTrashID + "/mine.md", Host: "desktop"}}
	if !slices.Equal(ws, want) {
		t.Fatalf("DetectTrashWarnings = %+v, want %+v", ws, want)
	}
	ws, err = DetectTrashWarnings(env.Laptop, origHead, "somewhere-else")
	if err != nil || len(ws) != 0 {
		t.Fatalf("DetectTrashWarnings(other host) = %+v, %v; want none", ws, err)
	}
}

func TestTrashWarningAfterConflictResolved(t *testing.T) {
	env := gittest.New(t)
	shareBase(t, env, func(r *gitsync.Repo) {
		gittest.Write(t, r, "note.md", "line\n")
		gittest.Write(t, r, "gone.md", "a\nb\nc\n")
	})
	h := newHarness(t, env.Laptop)
	h.start()
	deskPush(t, env, func(r *gitsync.Repo) {
		gittest.Write(t, r, "note.md", "desktop\n")
		trashNote(t, r, "gone.md", deskTrashID, "desktop", false)
	})
	gittest.Write(t, env.Laptop, "note.md", "laptop\n")
	gittest.Write(t, env.Laptop, "gone.md", "a\nb\nc\nlaptop edit\n")
	h.rec.reset()
	h.s.SyncNow()
	h.s.waitIdle()
	h.wantState(Conflict)
	if ws := h.rec.trashWarnings(); len(ws) != 0 {
		t.Fatalf("warnings before the merge is concluded: %+v", ws)
	}

	gittest.Write(t, env.Laptop, "note.md", "resolved\n")
	if err := env.Laptop.Add("note.md"); err != nil {
		t.Fatal(err)
	}
	if err := env.Laptop.CommitMerge("Merge · laptop"); err != nil {
		t.Fatal(err)
	}
	h.s.ConflictResolved()
	h.s.waitIdle()
	h.wantState(Synced)
	want := []TrashWarning{{Path: "gone.md", TrashPath: ".trash/" + deskTrashID + "/gone.md", Host: "desktop"}}
	if got := h.rec.trashWarnings(); !slices.Equal(got, want) {
		t.Fatalf("TrashWarnings = %+v, want %+v", got, want)
	}
}
