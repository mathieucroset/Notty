package meta

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMerge3(t *testing.T) {
	tests := []struct {
		name   string
		base   []string
		ours   []string
		theirs []string
		want   []string
	}{
		{
			name:   "both sides add different pins",
			base:   []string{"a.md"},
			ours:   []string{"a.md", "b.md"},
			theirs: []string{"a.md", "c.md"},
			want:   []string{"a.md", "b.md", "c.md"},
		},
		{
			name:   "same pin added both sides, no dup",
			base:   []string{},
			ours:   []string{"a.md"},
			theirs: []string{"a.md"},
			want:   []string{"a.md"},
		},
		{
			name:   "ours removes",
			base:   []string{"a.md", "b.md"},
			ours:   []string{"a.md"},
			theirs: []string{"a.md", "b.md"},
			want:   []string{"a.md"},
		},
		{
			name:   "theirs removes",
			base:   []string{"a.md", "b.md"},
			ours:   []string{"a.md", "b.md"},
			theirs: []string{"a.md"},
			want:   []string{"a.md"},
		},
		{
			name:   "one side removes while the other reorders",
			base:   []string{"a.md", "b.md", "c.md"},
			ours:   []string{"a.md", "c.md"},
			theirs: []string{"c.md", "a.md", "b.md"},
			want:   []string{"a.md", "c.md"},
		},
		{
			name:   "empty base",
			base:   []string{},
			ours:   []string{"a.md", "b.md"},
			theirs: []string{"b.md", "c.md"},
			want:   []string{"a.md", "b.md", "c.md"},
		},
		{
			name:   "all empty",
			base:   []string{},
			ours:   []string{},
			theirs: []string{},
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Merge3(tt.base, tt.ours, tt.theirs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Merge3(%v, %v, %v) = %v, want %v", tt.base, tt.ours, tt.theirs, got, tt.want)
			}
		})
	}
}

func TestToggleAndIsPinned(t *testing.T) {
	s := &State{}

	if s.IsPinned("a.md") {
		t.Fatalf("a.md should not be pinned initially")
	}

	if pinned := s.Toggle("a.md"); !pinned {
		t.Fatalf("Toggle(a.md) = false, want true (newly pinned)")
	}
	if !s.IsPinned("a.md") {
		t.Fatalf("a.md should be pinned after Toggle")
	}

	if pinned := s.Toggle("b.md"); !pinned {
		t.Fatalf("Toggle(b.md) = false, want true (newly pinned)")
	}
	want := []string{"a.md", "b.md"}
	if !reflect.DeepEqual(s.Pins, want) {
		t.Fatalf("Pins = %v, want %v (added to end)", s.Pins, want)
	}

	if pinned := s.Toggle("a.md"); pinned {
		t.Fatalf("Toggle(a.md) = true, want false (unpinned)")
	}
	if s.IsPinned("a.md") {
		t.Fatalf("a.md should not be pinned after second Toggle")
	}
	want = []string{"b.md"}
	if !reflect.DeepEqual(s.Pins, want) {
		t.Fatalf("Pins = %v, want %v (a.md removed)", s.Pins, want)
	}
}

func TestRenameNote(t *testing.T) {
	s := &State{Pins: []string{"Standup notes.md", "Work/a.md"}}
	s.Rename("Standup notes.md", "Daily notes.md")

	want := []string{"Daily notes.md", "Work/a.md"}
	if !reflect.DeepEqual(s.Pins, want) {
		t.Fatalf("Pins = %v, want %v", s.Pins, want)
	}
}

func TestRenameFolderPrefix(t *testing.T) {
	s := &State{Pins: []string{"Work/a.md", "Work2/b.md", "Other.md"}}
	s.Rename("Work", "Job")

	want := []string{"Job/a.md", "Work2/b.md", "Other.md"}
	if !reflect.DeepEqual(s.Pins, want) {
		t.Fatalf("Pins = %v, want %v", s.Pins, want)
	}
}

func TestRemoveFolder(t *testing.T) {
	s := &State{Pins: []string{"Work/a.md", "Work/b.md", "Work2/c.md", "Other.md"}}
	s.Remove("Work")

	want := []string{"Work2/c.md", "Other.md"}
	if !reflect.DeepEqual(s.Pins, want) {
		t.Fatalf("Pins = %v, want %v", s.Pins, want)
	}
}

func TestRemoveNote(t *testing.T) {
	s := &State{Pins: []string{"a.md", "b.md"}}
	s.Remove("a.md")

	want := []string{"b.md"}
	if !reflect.DeepEqual(s.Pins, want) {
		t.Fatalf("Pins = %v, want %v", s.Pins, want)
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()

	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if len(s.Pins) != 0 {
		t.Fatalf("Pins = %v, want empty", s.Pins)
	}
}

func TestParseEmptyData(t *testing.T) {
	s, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil) error = %v, want nil", err)
	}
	if len(s.Pins) != 0 {
		t.Fatalf("Pins = %v, want empty", s.Pins)
	}

	s, err = Parse([]byte{})
	if err != nil {
		t.Fatalf("Parse([]byte{}) error = %v, want nil", err)
	}
	if len(s.Pins) != 0 {
		t.Fatalf("Pins = %v, want empty", s.Pins)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	s := &State{Pins: []string{"a.md", "Work/b.md"}}
	if err := s.Save(dir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".notty", "state.json")); err != nil {
		t.Fatalf("state.json not created: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got.Pins, s.Pins) {
		t.Fatalf("Load() = %v, want %v", got.Pins, s.Pins)
	}
}

func TestSaveCreatesNottyDir(t *testing.T) {
	dir := t.TempDir()

	s := &State{Pins: []string{"a.md"}}
	if err := s.Save(dir); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, ".notty"))
	if err != nil {
		t.Fatalf(".notty dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf(".notty is not a directory")
	}
}

func TestMarshalStable(t *testing.T) {
	s := &State{Pins: []string{"a.md", "Work/b.md"}}
	got := string(s.Marshal())

	want := "{\n  \"pins\": [\n    \"a.md\",\n    \"Work/b.md\"\n  ]\n}\n"
	if got != want {
		t.Fatalf("Marshal() =\n%q\nwant\n%q", got, want)
	}
}

func TestMarshalEmptyPins(t *testing.T) {
	s := &State{}
	got := string(s.Marshal())

	want := "{\n  \"pins\": []\n}\n"
	if got != want {
		t.Fatalf("Marshal() =\n%q\nwant\n%q", got, want)
	}
}
