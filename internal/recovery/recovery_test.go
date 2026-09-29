package recovery

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var at = time.Date(2026, 9, 29, 14, 3, 7, 0, time.Local)

func TestSnapshotNilSafe(t *testing.T) {
	var s *Snapshot
	s.Set("/v", "a.md", "x", true)
	s.RecordPanic("boom", nil)
	if _, _, _, dirty := s.Get(); dirty {
		t.Error("nil snapshot reports a dirty buffer")
	}
	if _, _, ok := s.Panic(); ok {
		t.Error("nil snapshot reports a panic")
	}
}

func TestSnapshotConcurrent(t *testing.T) {
	s := &Snapshot{}
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); s.Set("/v", "a.md", strings.Repeat("x", i), true) }()
		go func() { defer wg.Done(); _, _, _, _ = s.Get() }()
	}
	wg.Wait()
	root, rel, _, dirty := s.Get()
	if root != "/v" || rel != "a.md" || !dirty {
		t.Errorf("Get = %q %q %v", root, rel, dirty)
	}
}

func TestSnapshotKeepsTheFirstPanic(t *testing.T) {
	s := &Snapshot{}
	s.RecordPanic("first", []byte("stack 1"))
	s.RecordPanic("second", []byte("stack 2"))
	v, stack, ok := s.Panic()
	if !ok || v != "first" || string(stack) != "stack 1" {
		t.Errorf("Panic = %v %q %v, want the first one", v, stack, ok)
	}
}

func TestWriteNamesAndListsTheFile(t *testing.T) {
	root := t.TempDir()
	p, err := Write(root, "Work/Standup Notes.md", "# Standup\n\nunsaved\n", at)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, ".notty", "recovery", "standup-notes-20260929-140307.md")
	if p != want {
		t.Errorf("Write = %s, want %s", p, want)
	}
	files, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("List = %+v, want one file", files)
	}
	f := files[0]
	if f.Note != "Work/Standup Notes.md" || !f.Time.Equal(at) || f.Name != "standup-notes-20260929-140307.md" {
		t.Errorf("file = %+v", f)
	}
	content, err := Read(root, f)
	if err != nil {
		t.Fatal(err)
	}
	if content != "# Standup\n\nunsaved\n" {
		t.Errorf("Read = %q", content)
	}
	if err := Remove(root, f); err != nil {
		t.Fatal(err)
	}
	if files, _ := List(root); len(files) != 0 {
		t.Errorf("after Remove, List = %+v", files)
	}
}

func TestWriteAvoidsCollisions(t *testing.T) {
	root := t.TempDir()
	a, err := Write(root, "a.md", "one", at)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(root, "A.md", "two", at)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || filepath.Base(b) != "a-20260929-140307-2.md" {
		t.Errorf("second file = %s (first %s)", b, a)
	}
	files, _ := List(root)
	if len(files) != 2 {
		t.Fatalf("List = %+v", files)
	}
	if got, _ := Read(root, files[1]); got != "two" {
		t.Errorf("second file content = %q", got)
	}
}

func TestListOldestFirstAndSkipsJunk(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, "late.md", "l", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, "early.md", "e", at); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, Dir)
	for _, junk := range []string{"x.md.notty-tmp", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, junk), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	files, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Note != "early.md" || files[1].Note != "late.md" {
		t.Errorf("List = %+v, want early then late", files)
	}
}

func TestListWithoutDir(t *testing.T) {
	files, err := List(t.TempDir())
	if err != nil || len(files) != 0 {
		t.Errorf("List = %v, %v; want nothing", files, err)
	}
}

// A file dropped in the folder by hand has no header: it is still offered,
// named after the file, timed by its modification time.
func TestListForeignFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "My idea.md")
	if err := os.WriteFile(p, []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
	files, err := List(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("List = %v, %v", files, err)
	}
	if f := files[0]; f.Note != "My idea.md" || !f.Time.Equal(at) {
		t.Errorf("file = %+v", f)
	}
	if got, _ := Read(root, files[0]); got != "text" {
		t.Errorf("Read = %q", got)
	}
}

func TestSaveBufferWritesOnlyUnsavedText(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "same.md"), []byte("on disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		set     func(s *Snapshot)
		written bool
	}{
		{"clean", func(s *Snapshot) { s.Set(root, "a.md", "", false) }, false},
		{"no note", func(s *Snapshot) { s.Set(root, "", "x", true) }, false},
		{"no vault", func(s *Snapshot) { s.Set("", "a.md", "x", true) }, false},
		{"equal to disk", func(s *Snapshot) { s.Set(root, "same.md", "on disk", true) }, false},
		{"dirty", func(s *Snapshot) { s.Set(root, "same.md", "edited", true) }, true},
		{"dirty, gone from disk", func(s *Snapshot) { s.Set(root, "gone.md", "text", true) }, true},
	}
	for _, c := range cases {
		s := &Snapshot{}
		c.set(s)
		p, err := s.SaveBuffer(at)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if (p != "") != c.written {
			t.Errorf("%s: SaveBuffer = %q, written want %v", c.name, p, c.written)
		}
		if p != "" {
			_ = os.Remove(p)
		}
	}
}
