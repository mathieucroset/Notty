package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/vault"
)

// write creates the file rel under the vault with content.
func write(t *testing.T, v *vault.Vault, rel, content string) {
	t.Helper()
	abs := v.Abs(rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newVault(t *testing.T, files map[string]string) *vault.Vault {
	t.Helper()
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatalf("vault.Open: %v", err)
	}
	for rel, content := range files {
		write(t, v, rel, content)
	}
	return v
}

func build(t *testing.T, v *vault.Vault) *Index {
	t.Helper()
	ix, err := Build(v)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

func paths(notes []*Note) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Path)
	}
	return out
}

func TestBuild(t *testing.T) {
	files := map[string]string{}
	var want []string
	for i := range 50 {
		rel := fmt.Sprintf("n%02d.md", i)
		switch {
		case i%3 == 1:
			rel = fmt.Sprintf("Work/n%02d.md", i)
		case i%3 == 2:
			rel = fmt.Sprintf("Work/Client/Deep/n%02d.md", i)
		}
		files[rel] = fmt.Sprintf("# Note %d\n\nbody #tag%d\n- [ ] task %d\n", i, i%5, i)
		want = append(want, rel)
	}
	// Ignored entries.
	files[".trash/1/old.md"] = "# Trashed\n"
	files["attachments/a.md"] = "# Attachment\n"
	files[".notty/x.md"] = "# State\n"
	files[".git/y.md"] = "# Git\n"
	files["Work/.hidden.md"] = "# Hidden\n"
	files["Work/.dotdir/z.md"] = "# Dot dir\n"
	files["Work/image.png"] = "png"
	files["readme.txt"] = "text"
	files["UPPER.MD"] = "# Upper\n"
	want = append(want, "UPPER.MD")
	sort.Strings(want)

	v := newVault(t, files)
	ix := build(t, v)

	if got := paths(ix.Notes()); !reflect.DeepEqual(got, want) {
		t.Fatalf("Notes paths =\n%v\nwant\n%v", got, want)
	}
	if ix.Len() != 51 {
		t.Errorf("Len = %d, want 51", ix.Len())
	}
	n, ok := ix.Get("Work/Client/Deep/n02.md")
	if !ok {
		t.Fatal("Get deep note: missing")
	}
	if n.Title != "Note 2" {
		t.Errorf("Title = %q", n.Title)
	}
	if !reflect.DeepEqual(n.Tags, []string{"tag2"}) {
		t.Errorf("Tags = %v", n.Tags)
	}
	if len(n.Tasks) != 1 || n.Tasks[0].Text != "task 2" || n.Tasks[0].Line != 3 {
		t.Errorf("Tasks = %+v", n.Tasks)
	}
	if n.ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
	if n.Content != files["Work/Client/Deep/n02.md"] {
		t.Errorf("Content = %q", n.Content)
	}
	if len(ix.Problems()) != 0 {
		t.Errorf("Problems = %v", ix.Problems())
	}
}

func TestBuildTitleFallback(t *testing.T) {
	v := newVault(t, map[string]string{"Folder/No Heading.md": "just text\n"})
	ix := build(t, v)
	n, ok := ix.Get("Folder/No Heading.md")
	if !ok || n.Title != "No Heading" {
		t.Fatalf("Get = %+v, %v", n, ok)
	}
}

func TestBuildProblems(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits not enforced")
	}
	v := newVault(t, map[string]string{
		"ok.md":     "# OK\n",
		"locked.md": "# Locked\n",
		"bad.md":    "# Bad \xff\n",
	})
	if err := os.Chmod(v.Abs("locked.md"), 0); err != nil {
		t.Fatal(err)
	}
	ix := build(t, v)
	if got, want := paths(ix.Notes()), []string{"bad.md", "ok.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Notes = %v, want %v", got, want)
	}
	probs := ix.Problems()
	if len(probs) != 2 {
		t.Fatalf("Problems = %v, want 2", probs)
	}
	if !errors.Is(probs[0], ErrInvalidUTF8) {
		t.Errorf("Problems[0] = %v, want ErrInvalidUTF8", probs[0])
	}
	if !errors.Is(probs[1], os.ErrPermission) {
		t.Errorf("Problems[1] = %v, want permission error", probs[1])
	}

	// Fixing the file and updating clears its problem.
	if err := os.Chmod(v.Abs("locked.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ix.Update(v, "locked.md"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if probs := ix.Problems(); len(probs) != 1 {
		t.Errorf("Problems after fix = %v, want 1", probs)
	}
}

func TestBuildSkipsVanishedFiles(t *testing.T) {
	// A file listed by Tree but deleted before it is read is skipped
	// without a problem.
	v := newVault(t, map[string]string{"ok.md": "# OK\n"})
	ix := buildFrom(v, []string{"ok.md", "vanished.md"})
	if got, want := paths(ix.Notes()), []string{"ok.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Notes = %v, want %v", got, want)
	}
	if probs := ix.Problems(); len(probs) != 0 {
		t.Errorf("Problems = %v, want none", probs)
	}
}

func TestBuildUnreadableRoot(t *testing.T) {
	v := &vault.Vault{Root: filepath.Join(t.TempDir(), "missing")}
	if _, err := Build(v); err == nil {
		t.Fatal("Build on missing root: want error")
	}
}

func TestUpdate(t *testing.T) {
	tests := []struct {
		name      string
		rel       string
		change    func(t *testing.T, v *vault.Vault)
		wantPaths []string
		wantTitle string // title of rel after update, if present
	}{
		{
			name:      "edit",
			rel:       "a.md",
			change:    func(t *testing.T, v *vault.Vault) { write(t, v, "a.md", "# A2\n#new\n") },
			wantPaths: []string{"a.md", "dir/b.md"},
			wantTitle: "A2",
		},
		{
			name:      "new file",
			rel:       "dir/c.md",
			change:    func(t *testing.T, v *vault.Vault) { write(t, v, "dir/c.md", "# C\n") },
			wantPaths: []string{"a.md", "dir/b.md", "dir/c.md"},
			wantTitle: "C",
		},
		{
			name: "deleted",
			rel:  "dir/b.md",
			change: func(t *testing.T, v *vault.Vault) {
				if err := os.Remove(v.Abs("dir/b.md")); err != nil {
					t.Fatal(err)
				}
			},
			wantPaths: []string{"a.md"},
		},
		{
			name:      "non-note ignored",
			rel:       "dir/x.txt",
			change:    func(t *testing.T, v *vault.Vault) { write(t, v, "dir/x.txt", "x") },
			wantPaths: []string{"a.md", "dir/b.md"},
		},
		{
			name:      "trash ignored",
			rel:       ".trash/1/a.md",
			change:    func(t *testing.T, v *vault.Vault) { write(t, v, ".trash/1/a.md", "# A\n") },
			wantPaths: []string{"a.md", "dir/b.md"},
		},
		{
			name: "folder path keeps children",
			rel:  "dir",
			change: func(t *testing.T, v *vault.Vault) {
			},
			wantPaths: []string{"a.md", "dir/b.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVault(t, map[string]string{"a.md": "# A\n", "dir/b.md": "# B\n"})
			ix := build(t, v)
			tt.change(t, v)
			if err := ix.Update(v, tt.rel); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if got := paths(ix.Notes()); !reflect.DeepEqual(got, tt.wantPaths) {
				t.Errorf("paths = %v, want %v", got, tt.wantPaths)
			}
			if tt.wantTitle != "" {
				n, ok := ix.Get(tt.rel)
				if !ok || n.Title != tt.wantTitle {
					t.Errorf("Get(%q) = %+v, %v; want title %q", tt.rel, n, ok, tt.wantTitle)
				}
			}
		})
	}
}

func TestUpdateVanishedPathRemovesSubtree(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, v *vault.Vault)
		rel    string
		want   []string
	}{
		{
			name: "folder renamed on disk",
			change: func(t *testing.T, v *vault.Vault) {
				if err := os.Rename(v.Abs("Folder"), v.Abs("Moved")); err != nil {
					t.Fatal(err)
				}
			},
			rel:  "Folder",
			want: []string{"FolderNote.md", "top.md"},
		},
		{
			name: "folder deleted on disk",
			change: func(t *testing.T, v *vault.Vault) {
				if err := os.RemoveAll(v.Abs("Folder/Sub")); err != nil {
					t.Fatal(err)
				}
			},
			rel:  "Folder/Sub",
			want: []string{"Folder/a.md", "FolderNote.md", "top.md"},
		},
		{
			name:   "existing folder keeps children",
			change: func(t *testing.T, v *vault.Vault) {},
			rel:    "Folder",
			want:   []string{"Folder/Sub/b.md", "Folder/a.md", "FolderNote.md", "top.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVault(t, map[string]string{
				"top.md": "# T\n", "FolderNote.md": "# FN\n",
				"Folder/a.md": "# A\n", "Folder/Sub/b.md": "# B\n",
			})
			ix := build(t, v)
			tt.change(t, v)
			if err := ix.Update(v, tt.rel); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if got := paths(ix.Notes()); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("paths = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpdateSkipsStaleRead(t *testing.T) {
	tests := []struct {
		name      string
		fileAge   time.Duration // file mtime relative to the buffer update
		wantTitle string
	}{
		{"older file ignored", -time.Hour, "Buffer"},
		{"newer file wins", time.Hour, "Disk"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVault(t, map[string]string{"a.md": "# Disk\n"})
			ix := New()
			ix.UpdateContent("a.md", "# Buffer\n")
			n, _ := ix.Get("a.md")
			mt := n.ModTime.Add(tt.fileAge)
			if err := os.Chtimes(v.Abs("a.md"), mt, mt); err != nil {
				t.Fatal(err)
			}
			if err := ix.Update(v, "a.md"); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if n, _ := ix.Get("a.md"); n.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", n.Title, tt.wantTitle)
			}
		})
	}
}

func TestUpdateReplacesNotMutates(t *testing.T) {
	v := newVault(t, map[string]string{"a.md": "# A\n"})
	ix := build(t, v)
	before, _ := ix.Get("a.md")
	write(t, v, "a.md", "# Changed\n")
	if err := ix.Update(v, "a.md"); err != nil {
		t.Fatal(err)
	}
	if before.Title != "A" || before.Content != "# A\n" {
		t.Errorf("old snapshot mutated: %+v", before)
	}
}

func TestUpdateContent(t *testing.T) {
	ix := New()
	start := time.Now()
	ix.UpdateContent("Work/x.md", "# From Buffer\n- [x] done #Work\n")
	n, ok := ix.Get("Work/x.md")
	if !ok {
		t.Fatal("missing after UpdateContent")
	}
	if n.Title != "From Buffer" || n.Path != "Work/x.md" {
		t.Errorf("note = %+v", n)
	}
	if !reflect.DeepEqual(n.Tags, []string{"Work"}) {
		t.Errorf("Tags = %v", n.Tags)
	}
	if len(n.Tasks) != 1 || !n.Tasks[0].Done {
		t.Errorf("Tasks = %+v", n.Tasks)
	}
	if n.ModTime.Before(start) {
		t.Errorf("ModTime %v before %v", n.ModTime, start)
	}
	// Paths are cleaned; non-note and hidden paths are ignored.
	ix.UpdateContent("/Work/./x.md", "# Again\n")
	ix.UpdateContent("notes.txt", "x")
	ix.UpdateContent(".trash/1/x.md", "x")
	if got, want := paths(ix.Notes()), []string{"Work/x.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("paths = %v, want %v", got, want)
	}
	if n, _ := ix.Get("Work/x.md"); n.Title != "Again" {
		t.Errorf("Title = %q", n.Title)
	}
}

func TestRemove(t *testing.T) {
	tests := []struct {
		name string
		rel  string
		want []string
	}{
		{"note", "Work/a.md", []string{"Work/Client/c.md", "Work/b.md", "WorkNotes.md", "top.md"}},
		{"folder", "Work", []string{"WorkNotes.md", "top.md"}},
		{"folder trailing slash", "Work/", []string{"WorkNotes.md", "top.md"}},
		{"nested folder", "Work/Client", []string{"Work/a.md", "Work/b.md", "WorkNotes.md", "top.md"}},
		{"missing", "nope.md", []string{"Work/Client/c.md", "Work/a.md", "Work/b.md", "WorkNotes.md", "top.md"}},
		{"root is no-op", "", []string{"Work/Client/c.md", "Work/a.md", "Work/b.md", "WorkNotes.md", "top.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix := New()
			for _, p := range []string{"top.md", "WorkNotes.md", "Work/a.md", "Work/b.md", "Work/Client/c.md"} {
				ix.UpdateContent(p, "x")
			}
			ix.Remove(tt.rel)
			if got := paths(ix.Notes()); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("paths = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRename(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		want     []string
		titles   map[string]string
	}{
		{
			name: "note", old: "Work/a.md", new: "Personal/renamed.md",
			want:   []string{"Personal/renamed.md", "Work/Client/c.md", "WorkNotes.md"},
			titles: map[string]string{"Personal/renamed.md": "renamed"}, // title falls back to new name
		},
		{
			name: "folder", old: "Work", new: "Archive/Old",
			want:   []string{"Archive/Old/Client/c.md", "Archive/Old/a.md", "WorkNotes.md"},
			titles: map[string]string{"Archive/Old/Client/c.md": "C"},
		},
		{
			name: "to non-note drops", old: "WorkNotes.md", new: "WorkNotes.txt",
			want: []string{"Work/Client/c.md", "Work/a.md"},
		},
		{
			name: "into trash drops", old: "Work", new: ".trash/123/Work",
			want: []string{"WorkNotes.md"},
		},
		{
			name: "same path", old: "Work/a.md", new: "Work/a.md",
			want: []string{"Work/Client/c.md", "Work/a.md", "WorkNotes.md"},
		},
		{
			name: "missing", old: "nope", new: "other",
			want: []string{"Work/Client/c.md", "Work/a.md", "WorkNotes.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix := New()
			ix.UpdateContent("Work/a.md", "no heading")
			ix.UpdateContent("Work/Client/c.md", "# C\n")
			ix.UpdateContent("WorkNotes.md", "# WN\n")
			ix.Rename(tt.old, tt.new)
			if got := paths(ix.Notes()); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("paths = %v, want %v", got, tt.want)
			}
			for p, title := range tt.titles {
				n, ok := ix.Get(p)
				if !ok || n.Title != title || n.Path != p {
					t.Errorf("Get(%q) = %+v, %v; want title %q", p, n, ok, title)
				}
			}
		})
	}
}

func TestTagCounts(t *testing.T) {
	ix := New()
	ix.UpdateContent("a.md", "#Work #idea")
	ix.UpdateContent("b.md", "#work #work/client")
	ix.UpdateContent("c.md", "#WORK #beta")
	ix.UpdateContent("d.md", "#alpha #beta")
	ix.UpdateContent("e.md", "no tags")
	want := []TagCount{
		{"Work", 3}, // grouped case-insensitively, spelling from first note by path
		{"beta", 2},
		{"alpha", 1},
		{"idea", 1},
		{"work/client", 1},
	}
	if got := ix.TagCounts(); !reflect.DeepEqual(got, want) {
		t.Errorf("TagCounts =\n%v\nwant\n%v", got, want)
	}
	if got := New().TagCounts(); len(got) != 0 {
		t.Errorf("empty TagCounts = %v", got)
	}
}

func TestNotesWithTag(t *testing.T) {
	ix := New()
	ix.UpdateContent("a.md", "#work")
	ix.UpdateContent("b.md", "#Work/Client")
	ix.UpdateContent("c.md", "#workshop")
	ix.UpdateContent("d.md", "#work/client/x")
	ix.UpdateContent("e.md", "nothing")
	tests := []struct {
		tag  string
		want []string
	}{
		{"work", []string{"a.md", "b.md", "d.md"}},
		{"#WORK", []string{"a.md", "b.md", "d.md"}},
		{"work/client", []string{"b.md", "d.md"}},
		{"workshop", []string{"c.md"}},
		{"work/", []string{"a.md", "b.md", "d.md"}},
		{"missing", nil},
		{"", nil},
		{"#", nil},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := ix.NotesWithTag(tt.tag); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NotesWithTag(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestAllTasks(t *testing.T) {
	ix := New()
	ix.UpdateContent("b.md", "# B\n- [ ] b1\ntext\n- [x] b2 @2026-10-01\n")
	ix.UpdateContent("a.md", "# A\n- [ ] a1\n")
	ix.UpdateContent("c.md", "# C\nno tasks\n")
	got := ix.AllTasks()
	type row struct {
		Path, Title, Text string
		Line              int
		Done              bool
	}
	var rows []row
	for _, r := range got {
		rows = append(rows, row{r.Path, r.Title, r.Task.Text, r.Task.Line, r.Task.Done})
	}
	want := []row{
		{"a.md", "A", "a1", 1, false},
		{"b.md", "B", "b1", 1, false},
		{"b.md", "B", "b2 @2026-10-01", 3, true},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("AllTasks =\n%+v\nwant\n%+v", rows, want)
	}
	if got[2].Task.Due == nil {
		t.Error("due date not parsed")
	}
}

func TestConcurrentReadsDuringUpdate(t *testing.T) {
	files := map[string]string{}
	for i := range 20 {
		files[fmt.Sprintf("d%d/n%d.md", i%4, i)] = fmt.Sprintf("# N%d\n#t%d\n- [ ] x\n", i, i%3)
	}
	v := newVault(t, files)
	ix := build(t, v)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, n := range ix.Notes() {
					_ = n.Title + n.Content
					_ = len(n.Tags) + len(n.Tasks)
				}
				_ = ix.TagCounts()
				_ = ix.NotesWithTag("t1")
				_ = ix.AllTasks()
				_, _ = ix.Get("d0/n0.md")
				_ = ix.Len()
				_ = ix.Problems()
			}
		})
	}
	for i := range 100 {
		rel := fmt.Sprintf("d%d/n%d.md", i%4, i%20)
		write(t, v, rel, fmt.Sprintf("# N%d v%d\n#t%d\n", i%20, i, i%5))
		if err := ix.Update(v, rel); err != nil {
			t.Errorf("Update: %v", err)
		}
		ix.UpdateContent("buf.md", fmt.Sprintf("# buf %d", i))
		ix.Rename("buf.md", "buf2.md")
		ix.Remove("buf2.md")
	}
	close(stop)
	wg.Wait()
	if ix.Len() != 20 {
		t.Errorf("Len = %d, want 20", ix.Len())
	}
}
