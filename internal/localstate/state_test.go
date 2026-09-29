package localstate

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
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

func TestLoadCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Load(path)
	if err == nil {
		t.Fatal("Load with corrupt JSON returned nil error")
	}
	if s != nil {
		t.Fatalf("Load with corrupt JSON returned non-nil state: %+v", s)
	}
	if errors.Unwrap(err) == nil {
		t.Errorf("expected error to wrap the underlying JSON error, got %v", err)
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

func TestSaveParentPathIsRegularFile(t *testing.T) {
	dir := t.TempDir()
	// blocker exists as a regular file, so it can never be created as a
	// directory: MkdirAll(blocker, ...) must fail.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "state.json")

	err := newState().Save(path)
	if err == nil {
		t.Fatal("Save returned nil error when parent path is a regular file")
	}
	if errors.Unwrap(err) == nil {
		t.Errorf("expected error to wrap the underlying MkdirAll error, got %v", err)
	}
}

func TestTouch(t *testing.T) {
	// many simulates touching more than maxRecents distinct notes, in order.
	many := make([]string, maxRecents+5)
	for i := range many {
		many[i] = fmt.Sprintf("note-%02d.md", i)
	}
	// The cap keeps the last maxRecents touches, most recently touched
	// first (i.e. the tail of many, reversed).
	wantCapped := make([]string, 0, maxRecents)
	for i := len(many) - 1; i >= len(many)-maxRecents; i-- {
		wantCapped = append(wantCapped, many[i])
	}

	cases := []struct {
		name         string
		initial      []string
		touches      []string
		wantRecents  []string
		wantLastNote string
	}{
		{
			name:         "moves existing entry to front and dedupes",
			initial:      []string{"a.md", "b.md", "c.md"},
			touches:      []string{"b.md"},
			wantRecents:  []string{"b.md", "a.md", "c.md"},
			wantLastNote: "b.md",
		},
		{
			name:         "adds new entry to front",
			initial:      []string{"a.md", "b.md"},
			touches:      []string{"new.md"},
			wantRecents:  []string{"new.md", "a.md", "b.md"},
			wantLastNote: "new.md",
		},
		{
			name:         "re-touching same entry keeps it deduped at front",
			touches:      []string{"a.md", "b.md", "a.md"},
			wantRecents:  []string{"a.md", "b.md"},
			wantLastNote: "a.md",
		},
		{
			name:         "caps at maxRecents entries, most recent first",
			touches:      many,
			wantRecents:  wantCapped,
			wantLastNote: many[len(many)-1],
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &State{Recents: append([]string(nil), tc.initial...)}
			for _, p := range tc.touches {
				s.Touch(p)
			}
			if !reflect.DeepEqual(s.Recents, tc.wantRecents) {
				t.Errorf("Recents = %v, want %v", s.Recents, tc.wantRecents)
			}
			if s.LastNote != tc.wantLastNote {
				t.Errorf("LastNote = %q, want %q", s.LastNote, tc.wantLastNote)
			}
		})
	}
}

func TestRename(t *testing.T) {
	cases := []struct {
		name         string
		initial      *State
		oldPath      string
		newPath      string
		wantRecents  []string
		wantLastNote string
		wantCursor   map[string][2]int
		wantExpanded []string
	}{
		{
			name: "renames a single note",
			initial: &State{
				Recents:  []string{"Work/Standup notes.md", "Personal/ideas.md"},
				LastNote: "Work/Standup notes.md",
				Cursor: map[string][2]int{
					"Work/Standup notes.md": {4, 12},
					"Personal/ideas.md":     {0, 0},
				},
				// Expanded is a folder list; renaming a note should not
				// affect it.
				Expanded: []string{"Work", "Personal"},
			},
			oldPath:      "Work/Standup notes.md",
			newPath:      "Work/Daily notes.md",
			wantRecents:  []string{"Work/Daily notes.md", "Personal/ideas.md"},
			wantLastNote: "Work/Daily notes.md",
			wantCursor: map[string][2]int{
				"Work/Daily notes.md": {4, 12},
				"Personal/ideas.md":   {0, 0},
			},
			wantExpanded: []string{"Work", "Personal"},
		},
		{
			name: "renames a folder and its descendants, not a same-prefix sibling",
			initial: &State{
				Recents: []string{
					"Work/Standup notes.md",
					"Work/Sub/nested.md",
					"Work2/other.md", // must NOT match "Work" prefix
					"Personal/ideas.md",
				},
				LastNote: "Work/Standup notes.md",
				Cursor: map[string][2]int{
					"Work/Standup notes.md": {1, 1},
					"Work/Sub/nested.md":    {2, 2},
					"Work2/other.md":        {3, 3},
				},
				Expanded: []string{"Work", "Work/Sub", "Work2", "Personal"},
			},
			oldPath: "Work",
			newPath: "Projects",
			wantRecents: []string{
				"Projects/Standup notes.md",
				"Projects/Sub/nested.md",
				"Work2/other.md",
				"Personal/ideas.md",
			},
			wantLastNote: "Projects/Standup notes.md",
			wantCursor: map[string][2]int{
				"Projects/Standup notes.md": {1, 1},
				"Projects/Sub/nested.md":    {2, 2},
				"Work2/other.md":            {3, 3},
			},
			wantExpanded: []string{"Projects", "Projects/Sub", "Work2", "Personal"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.initial.Rename(tc.oldPath, tc.newPath)

			if !reflect.DeepEqual(tc.initial.Recents, tc.wantRecents) {
				t.Errorf("Recents = %v, want %v", tc.initial.Recents, tc.wantRecents)
			}
			if tc.initial.LastNote != tc.wantLastNote {
				t.Errorf("LastNote = %q, want %q", tc.initial.LastNote, tc.wantLastNote)
			}
			if !reflect.DeepEqual(tc.initial.Cursor, tc.wantCursor) {
				t.Errorf("Cursor = %v, want %v", tc.initial.Cursor, tc.wantCursor)
			}
			if !reflect.DeepEqual(tc.initial.Expanded, tc.wantExpanded) {
				t.Errorf("Expanded = %v, want %v", tc.initial.Expanded, tc.wantExpanded)
			}
		})
	}
}

func TestRemove(t *testing.T) {
	cases := []struct {
		name         string
		initial      *State
		remove       string
		wantRecents  []string
		wantLastNote string
		wantCursor   map[string][2]int
		wantExpanded []string
	}{
		{
			name: "removes a note everywhere",
			initial: &State{
				Recents:  []string{"Work/a.md", "Work/b.md"},
				LastNote: "Work/a.md",
				Cursor: map[string][2]int{
					"Work/a.md": {1, 1},
					"Work/b.md": {2, 2},
				},
				Expanded: []string{"Work"},
			},
			remove:       "Work/a.md",
			wantRecents:  []string{"Work/b.md"},
			wantLastNote: "",
			wantCursor:   map[string][2]int{"Work/b.md": {2, 2}},
			wantExpanded: []string{"Work"},
		},
		{
			name: "removes a folder and its children, not a same-prefix sibling",
			initial: &State{
				Recents: []string{
					"Work/a.md",
					"Work/Sub/b.md",
					"Work2/c.md", // must NOT match "Work" prefix
				},
				LastNote: "Work/Sub/b.md",
				Cursor: map[string][2]int{
					"Work/a.md":     {1, 1},
					"Work/Sub/b.md": {2, 2},
					"Work2/c.md":    {3, 3},
				},
				Expanded: []string{"Work", "Work/Sub", "Work2"},
			},
			remove:       "Work",
			wantRecents:  []string{"Work2/c.md"},
			wantLastNote: "",
			wantCursor:   map[string][2]int{"Work2/c.md": {3, 3}},
			wantExpanded: []string{"Work2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.initial.Remove(tc.remove)

			if !reflect.DeepEqual(tc.initial.Recents, tc.wantRecents) {
				t.Errorf("Recents = %v, want %v", tc.initial.Recents, tc.wantRecents)
			}
			if tc.initial.LastNote != tc.wantLastNote {
				t.Errorf("LastNote = %q, want %q", tc.initial.LastNote, tc.wantLastNote)
			}
			if !reflect.DeepEqual(tc.initial.Cursor, tc.wantCursor) {
				t.Errorf("Cursor = %v, want %v", tc.initial.Cursor, tc.wantCursor)
			}
			if !reflect.DeepEqual(tc.initial.Expanded, tc.wantExpanded) {
				t.Errorf("Expanded = %v, want %v", tc.initial.Expanded, tc.wantExpanded)
			}
		})
	}
}
