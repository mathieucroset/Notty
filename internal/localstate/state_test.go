package localstate

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
)

func TestPathFor(t *testing.T) {
	stateDir := t.TempDir()
	vaultA := t.TempDir()
	vaultB := t.TempDir()

	t.Run("stable for same vault", func(t *testing.T) {
		p1 := PathFor(stateDir, vaultA)
		p2 := PathFor(stateDir, vaultA)
		if p1 != p2 {
			t.Fatalf("PathFor not stable: %q != %q", p1, p2)
		}
	})

	t.Run("differs per vault", func(t *testing.T) {
		p1 := PathFor(stateDir, vaultA)
		p2 := PathFor(stateDir, vaultB)
		if p1 == p2 {
			t.Fatalf("PathFor should differ per vault, both = %q", p1)
		}
	})

	t.Run("format is stateDir/<12 hex chars>.json", func(t *testing.T) {
		p := PathFor(stateDir, vaultA)
		if filepath.Dir(p) != stateDir {
			t.Fatalf("expected dir %q, got %q", stateDir, filepath.Dir(p))
		}
		base := filepath.Base(p)
		if !regexp.MustCompile(`^[0-9a-f]{12}\.json$`).MatchString(base) {
			t.Fatalf("unexpected filename format: %q", base)
		}
	})

	t.Run("matches sha1 of abs clean vault root", func(t *testing.T) {
		abs, err := filepath.Abs(vaultA)
		if err != nil {
			t.Fatal(err)
		}
		abs = filepath.Clean(abs)
		sum := sha1.Sum([]byte(abs))
		want := filepath.Join(stateDir, hex.EncodeToString(sum[:])[:12]+".json")
		got := PathFor(stateDir, vaultA)
		if got != want {
			t.Fatalf("PathFor = %q, want %q", got, want)
		}
	})

	t.Run("normalizes trailing slash and non-clean paths", func(t *testing.T) {
		p1 := PathFor(stateDir, vaultA)
		p2 := PathFor(stateDir, vaultA+"/")
		p3 := PathFor(stateDir, vaultA+"/./")
		if p1 != p2 || p1 != p3 {
			t.Fatalf("PathFor should normalize path variants: %q, %q, %q", p1, p2, p3)
		}
	})
}

func TestLoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load missing file returned error: %v", err)
	}
	if s == nil {
		t.Fatal("Load missing file returned nil state")
	}
	if s.Recents == nil {
		t.Error("Recents should be non-nil for empty state")
	}
	if s.Cursor == nil {
		t.Error("Cursor should be non-nil for empty state")
	}
	if s.Expanded == nil {
		t.Error("Expanded should be non-nil for empty state")
	}
	if len(s.Recents) != 0 || len(s.Cursor) != 0 || len(s.Expanded) != 0 || s.LastNote != "" {
		t.Errorf("expected empty state, got %+v", s)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "state.json")

	s := &State{
		Recents:  []string{"Work/Standup notes.md", "Personal/ideas.md"},
		LastNote: "Work/Standup notes.md",
		Cursor: map[string][2]int{
			"Work/Standup notes.md": {4, 12},
		},
		Expanded: []string{"Work", "Personal"},
	}

	if err := s.Save(path); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !reflect.DeepEqual(s, loaded) {
		t.Fatalf("round trip mismatch:\nwant %+v\ngot  %+v", s, loaded)
	}

	// No leftover temp files in the directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("expected only state.json in dir, found %v", entries)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "state.json")

	s := newState()
	if err := s.Save(path); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
}

func TestTouch(t *testing.T) {
	t.Run("moves existing entry to front and dedupes", func(t *testing.T) {
		s := &State{Recents: []string{"a.md", "b.md", "c.md"}}
		s.Touch("b.md")
		want := []string{"b.md", "a.md", "c.md"}
		if !reflect.DeepEqual(s.Recents, want) {
			t.Fatalf("Recents = %v, want %v", s.Recents, want)
		}
		if s.LastNote != "b.md" {
			t.Fatalf("LastNote = %q, want %q", s.LastNote, "b.md")
		}
	})

	t.Run("adds new entry to front", func(t *testing.T) {
		s := &State{Recents: []string{"a.md", "b.md"}}
		s.Touch("new.md")
		want := []string{"new.md", "a.md", "b.md"}
		if !reflect.DeepEqual(s.Recents, want) {
			t.Fatalf("Recents = %v, want %v", s.Recents, want)
		}
	})

	t.Run("caps at 20 entries", func(t *testing.T) {
		s := newState()
		for i := 0; i < 25; i++ {
			s.Touch(string(rune('a' + i)))
		}
		if len(s.Recents) != 20 {
			t.Fatalf("len(Recents) = %d, want 20", len(s.Recents))
		}
		// Most recent (last touched) should be at front.
		if s.Recents[0] != string(rune('a'+24)) {
			t.Fatalf("Recents[0] = %q, want most recently touched entry", s.Recents[0])
		}
	})

	t.Run("re-touching same entry keeps it deduped at front", func(t *testing.T) {
		s := newState()
		s.Touch("a.md")
		s.Touch("b.md")
		s.Touch("a.md")
		want := []string{"a.md", "b.md"}
		if !reflect.DeepEqual(s.Recents, want) {
			t.Fatalf("Recents = %v, want %v", s.Recents, want)
		}
	})
}

func TestRenameNote(t *testing.T) {
	s := &State{
		Recents:  []string{"Work/Standup notes.md", "Personal/ideas.md"},
		LastNote: "Work/Standup notes.md",
		Cursor: map[string][2]int{
			"Work/Standup notes.md": {4, 12},
			"Personal/ideas.md":     {0, 0},
		},
		Expanded: []string{"Work", "Personal"},
	}

	s.Rename("Work/Standup notes.md", "Work/Daily notes.md")

	wantRecents := []string{"Work/Daily notes.md", "Personal/ideas.md"}
	if !reflect.DeepEqual(s.Recents, wantRecents) {
		t.Errorf("Recents = %v, want %v", s.Recents, wantRecents)
	}
	if s.LastNote != "Work/Daily notes.md" {
		t.Errorf("LastNote = %q, want %q", s.LastNote, "Work/Daily notes.md")
	}
	if _, ok := s.Cursor["Work/Standup notes.md"]; ok {
		t.Error("old cursor key should be removed")
	}
	if got, ok := s.Cursor["Work/Daily notes.md"]; !ok || got != [2]int{4, 12} {
		t.Errorf("Cursor[new] = %v, ok=%v, want {4,12}, true", got, ok)
	}
	if got, ok := s.Cursor["Personal/ideas.md"]; !ok || got != [2]int{0, 0} {
		t.Errorf("unrelated cursor entry should be untouched: %v, %v", got, ok)
	}
	// Expanded is a folder list; renaming a note path should not affect it.
	wantExpanded := []string{"Work", "Personal"}
	if !reflect.DeepEqual(s.Expanded, wantExpanded) {
		t.Errorf("Expanded = %v, want %v", s.Expanded, wantExpanded)
	}
}

func TestRenameFolder(t *testing.T) {
	s := &State{
		Recents: []string{
			"Work/Standup notes.md",
			"Work/Sub/nested.md",
			"Work2/other.md",
			"Personal/ideas.md",
		},
		LastNote: "Work/Standup notes.md",
		Cursor: map[string][2]int{
			"Work/Standup notes.md": {1, 1},
			"Work/Sub/nested.md":    {2, 2},
			"Work2/other.md":        {3, 3},
		},
		Expanded: []string{"Work", "Work/Sub", "Work2", "Personal"},
	}

	s.Rename("Work", "Projects")

	wantRecents := []string{
		"Projects/Standup notes.md",
		"Projects/Sub/nested.md",
		"Work2/other.md", // must NOT match "Work" prefix
		"Personal/ideas.md",
	}
	if !reflect.DeepEqual(s.Recents, wantRecents) {
		t.Errorf("Recents = %v, want %v", s.Recents, wantRecents)
	}
	if s.LastNote != "Projects/Standup notes.md" {
		t.Errorf("LastNote = %q, want %q", s.LastNote, "Projects/Standup notes.md")
	}

	wantCursor := map[string][2]int{
		"Projects/Standup notes.md": {1, 1},
		"Projects/Sub/nested.md":    {2, 2},
		"Work2/other.md":            {3, 3},
	}
	if !reflect.DeepEqual(s.Cursor, wantCursor) {
		t.Errorf("Cursor = %v, want %v", s.Cursor, wantCursor)
	}

	wantExpanded := []string{"Projects", "Projects/Sub", "Work2", "Personal"}
	if !reflect.DeepEqual(s.Expanded, wantExpanded) {
		t.Errorf("Expanded = %v, want %v", s.Expanded, wantExpanded)
	}
}

func TestRemove(t *testing.T) {
	t.Run("removes a note everywhere", func(t *testing.T) {
		s := &State{
			Recents:  []string{"Work/a.md", "Work/b.md"},
			LastNote: "Work/a.md",
			Cursor: map[string][2]int{
				"Work/a.md": {1, 1},
				"Work/b.md": {2, 2},
			},
			Expanded: []string{"Work"},
		}
		s.Remove("Work/a.md")

		if reflect.DeepEqual(s.Recents, []string{"Work/a.md", "Work/b.md"}) {
			t.Fatal("Recents unchanged")
		}
		wantRecents := []string{"Work/b.md"}
		if !reflect.DeepEqual(s.Recents, wantRecents) {
			t.Errorf("Recents = %v, want %v", s.Recents, wantRecents)
		}
		if s.LastNote != "" {
			t.Errorf("LastNote = %q, want empty", s.LastNote)
		}
		if _, ok := s.Cursor["Work/a.md"]; ok {
			t.Error("cursor entry for removed note should be gone")
		}
		if _, ok := s.Cursor["Work/b.md"]; !ok {
			t.Error("unrelated cursor entry should remain")
		}
	})

	t.Run("removes a folder and its children, not siblings with prefix name", func(t *testing.T) {
		s := &State{
			Recents: []string{
				"Work/a.md",
				"Work/Sub/b.md",
				"Work2/c.md",
			},
			LastNote: "Work/Sub/b.md",
			Cursor: map[string][2]int{
				"Work/a.md":     {1, 1},
				"Work/Sub/b.md": {2, 2},
				"Work2/c.md":    {3, 3},
			},
			Expanded: []string{"Work", "Work/Sub", "Work2"},
		}
		s.Remove("Work")

		wantRecents := []string{"Work2/c.md"}
		if !reflect.DeepEqual(s.Recents, wantRecents) {
			t.Errorf("Recents = %v, want %v", s.Recents, wantRecents)
		}
		if s.LastNote != "" {
			t.Errorf("LastNote = %q, want empty", s.LastNote)
		}
		wantCursor := map[string][2]int{"Work2/c.md": {3, 3}}
		if !reflect.DeepEqual(s.Cursor, wantCursor) {
			t.Errorf("Cursor = %v, want %v", s.Cursor, wantCursor)
		}
		wantExpanded := []string{"Work2"}
		if !reflect.DeepEqual(s.Expanded, wantExpanded) {
			t.Errorf("Expanded = %v, want %v", s.Expanded, wantExpanded)
		}
	})
}
