package gitsync

import (
	"slices"
	"testing"
)

type fakeSides struct {
	ours, theirs         map[string]string
	oursBlobs, thrsBlobs map[string]string
}

func (f fakeSides) oursRenames() map[string]string   { return f.ours }
func (f fakeSides) theirsRenames() map[string]string { return f.theirs }
func (f fakeSides) oursBlob(p string) string         { return f.oursBlobs[p] }
func (f fakeSides) theirsBlob(p string) string       { return f.thrsBlobs[p] }

func dd(path, base string) Conflict {
	return Conflict{Path: path, Kind: BothDeleted, Blob: [4]string{1: base}}
}
func au(path, blob string) Conflict {
	return Conflict{Path: path, Kind: AddedByUs, Blob: [4]string{2: blob}}
}
func ua(path, blob string) Conflict {
	return Conflict{Path: path, Kind: AddedByThem, Blob: [4]string{3: blob}}
}

func TestGroupPathConflicts(t *testing.T) {
	uu := Conflict{Path: "x.md", Kind: BothModified, Blob: [4]string{1: "b1", 2: "b2", 3: "b3"}}
	two := []Conflict{
		dd("a.md", "A0"), dd("b.md", "B0"),
		au(".trash/1/a.md", "Am"), ua(".trash/2/a.md", "Am"),
		au("Work/b.md", "Bm"), ua("Old/b.md", "Bm"),
		uu,
	}
	type want struct{ orig, ours, theirs string }
	tests := []struct {
		name      string
		conflicts []Conflict
		output    string
		sides     fakeSides
		want      []want
		wantRest  []string
	}{
		{
			name:      "merge output",
			conflicts: two,
			output: "Auto-merging x.md\n" +
				"CONFLICT (rename/rename): a.md renamed to .trash/1/a.md in HEAD and to .trash/2/a.md in origin/main.\n" +
				"CONFLICT (rename/rename): b.md renamed to Work/b.md in HEAD and to Old/b.md in origin/main.\n",
			want:     []want{{"a.md", ".trash/1/a.md", ".trash/2/a.md"}, {"b.md", "Work/b.md", "Old/b.md"}},
			wantRest: []string{"x.md"},
		},
		{
			name:      "rename detection",
			conflicts: two,
			sides: fakeSides{
				ours:   map[string]string{"a.md": ".trash/1/a.md", "b.md": "Work/b.md"},
				theirs: map[string]string{"a.md": ".trash/2/a.md", "b.md": "Old/b.md"},
			},
			want:     []want{{"a.md", ".trash/1/a.md", ".trash/2/a.md"}, {"b.md", "Work/b.md", "Old/b.md"}},
			wantRest: []string{"x.md"},
		},
		{
			name:      "side blobs equal base",
			conflicts: two,
			sides: fakeSides{
				oursBlobs: map[string]string{".trash/1/a.md": "A0", "Work/b.md": "B0"},
				thrsBlobs: map[string]string{".trash/2/a.md": "A0", "Old/b.md": "B0"},
			},
			want:     []want{{"a.md", ".trash/1/a.md", ".trash/2/a.md"}, {"b.md", "Work/b.md", "Old/b.md"}},
			wantRest: []string{"x.md"},
		},
		{
			name:      "single remaining pair",
			conflicts: []Conflict{dd("a.md", "A0"), au("n1.md", "M"), ua("n2.md", "M"), uu},
			want:      []want{{"a.md", "n1.md", "n2.md"}},
			wantRest:  []string{"x.md"},
		},
		{
			name:      "ambiguous without hints stays ordinary",
			conflicts: two,
			want:      nil,
			wantRest:  []string{"a.md", "b.md", ".trash/1/a.md", ".trash/2/a.md", "Work/b.md", "Old/b.md", "x.md"},
		},
		{
			name:      "one side hinted, other side single remaining",
			conflicts: []Conflict{dd("a.md", "A0"), au("n1.md", "M"), ua("n2.md", "M")},
			sides:     fakeSides{ours: map[string]string{"a.md": "n1.md"}},
			want:      []want{{"a.md", "n1.md", "n2.md"}},
		},
		{
			name:      "no DD",
			conflicts: []Conflict{au("n1.md", "M"), uu},
			wantRest:  []string{"n1.md", "x.md"},
		},
		{
			name:      "paths with spaces and ' in '",
			conflicts: []Conflict{dd("My note.md", "A0"), au("Work/Plans in 2026.md", "M"), ua(".trash/9/My note.md", "M"), au("decoy.md", "Z"), dd("zz.md", "Q")},
			output:    "CONFLICT (rename/rename): My note.md renamed to Work/Plans in 2026.md in HEAD and to .trash/9/My note.md in origin/main.\n",
			want:      []want{{"My note.md", "Work/Plans in 2026.md", ".trash/9/My note.md"}},
			wantRest:  []string{"decoy.md", "zz.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pcs, rest := groupPathConflicts(tt.conflicts, tt.output, tt.sides)
			var got []want
			for _, pc := range pcs {
				got = append(got, want{pc.Original, pc.Ours, pc.Theirs})
				if pc.OursBlob == "" || pc.TheirsBlob == "" {
					t.Errorf("%s: missing blobs: %+v", pc.Original, pc)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("pairs = %+v, want %+v", got, tt.want)
			}
			var restPaths []string
			for _, c := range rest {
				restPaths = append(restPaths, c.Path)
			}
			if !slices.Equal(restPaths, tt.wantRest) {
				t.Errorf("rest = %v, want %v", restPaths, tt.wantRest)
			}
		})
	}
}

func TestPathConflictKind(t *testing.T) {
	tests := []struct {
		ours, theirs string
		want         PathConflictKind
	}{
		{".trash/1/a.md", ".trash/2/a.md", BothTrashed},
		{".trash/1/a.md", "Work/a.md", TrashVsMove},
		{"Work/a.md", ".trash/2/a.md", TrashVsMove},
		{"Work/a.md", "Home/a.md", RenameRename},
		{"x.trash/a.md", "Home/a.md", RenameRename},
	}
	for _, tt := range tests {
		if got := pathConflictKind(tt.ours, tt.theirs); got != tt.want {
			t.Errorf("pathConflictKind(%q, %q) = %v, want %v", tt.ours, tt.theirs, got, tt.want)
		}
	}
}

func TestParseLsFilesUnmerged(t *testing.T) {
	out := "100644 aaa 1\tboth.md\x00100644 bbb 2\tboth.md\x00100644 ccc 3\tboth.md\x00" +
		"100644 ddd 1\tud.md\x00100644 eee 2\tud.md\x00" +
		"100644 fff 1\tdu.md\x00100644 ggg 3\tdu.md\x00" +
		"100644 hhh 2\taa.md\x00100644 iii 3\taa.md\x00" +
		"100644 jjj 2\tau.md\x00100644 kkk 3\tua.md\x00100644 lll 1\tdd dir/x.md\x00"
	got, err := parseLsFilesUnmerged(out)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ConflictKind{"both.md": BothModified, "ud.md": DeletedByThem, "du.md": DeletedByUs, "aa.md": BothAdded, "au.md": AddedByUs, "ua.md": AddedByThem, "dd dir/x.md": BothDeleted}
	if len(got) != len(want) {
		t.Fatalf("got %d conflicts, want %d: %+v", len(got), len(want), got)
	}
	for _, c := range got {
		if want[c.Path] != c.Kind {
			t.Errorf("%s: kind %s, want %s", c.Path, c.Kind, want[c.Path])
		}
	}
	if got[0].Blob != [4]string{"", "aaa", "bbb", "ccc"} {
		t.Errorf("both.md blobs = %v", got[0].Blob)
	}
	if _, err := parseLsFilesUnmerged("garbage\x00"); err == nil {
		t.Errorf("malformed input accepted")
	}
}
