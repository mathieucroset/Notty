package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// trashVault returns a vault whose trash host is fixed to "testhost".
func trashVault(t *testing.T) *Vault {
	t.Helper()
	v := openVault(t)
	v.host = "testhost"
	return v
}

func writeFile(t *testing.T, v *Vault, rel, content string) {
	t.Helper()
	abs := v.Abs(rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNewTrashID(t *testing.T) {
	now := time.Date(2026, 9, 29, 14, 3, 7, 0, time.UTC)
	tests := []struct {
		name, host, wantPrefix string
	}{
		{"plain host", "laptop", "20260929140307-laptop-"},
		{"dashes kept", "my-box-2", "20260929140307-my-box-2-"},
		{"invalid chars removed", "my_box.local é", "20260929140307-myboxlocal-"},
		{"empty host", "", "20260929140307-unknown-"},
		{"only invalid chars", "__..", "20260929140307-unknown-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := NewTrashID(now, tt.host)
			if !strings.HasPrefix(id, tt.wantPrefix) {
				t.Fatalf("NewTrashID = %q, want prefix %q", id, tt.wantPrefix)
			}
			if !regexp.MustCompile(`^[a-z0-9]{4}$`).MatchString(strings.TrimPrefix(id, tt.wantPrefix)) {
				t.Errorf("NewTrashID = %q: suffix is not 4 chars of [a-z0-9]", id)
			}
		})
	}
	t.Run("local time converted to UTC", func(t *testing.T) {
		local := now.In(time.FixedZone("X", 2*3600))
		if id := NewTrashID(local, "h"); !strings.HasPrefix(id, "20260929140307-h-") {
			t.Errorf("NewTrashID = %q, want UTC timestamp", id)
		}
	})
	t.Run("random suffix varies", func(t *testing.T) {
		seen := map[string]bool{}
		for i := 0; i < 50; i++ {
			seen[NewTrashID(now, "h")] = true
		}
		if len(seen) < 2 {
			t.Error("NewTrashID returned the same ID 50 times")
		}
	})
}

func TestTrash(t *testing.T) {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		files   []string // created before trashing
		rel     string
		isDir   bool
		content []string // paths expected under the trashed item's content path
	}{
		{"note at root", []string{"Note.md"}, "Note.md", false, []string{""}},
		{"nested note", []string{"Work/Standup.md", "Work/Other.md"}, "Work/Standup.md", false, []string{""}},
		{"folder", []string{"Work/A.md", "Work/Sub/B.md"}, "Work", true, []string{"A.md", "Sub/B.md"}},
		{"unclean path", []string{"Work/A.md"}, "/Work/../Work/A.md", false, []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := trashVault(t)
			mkfiles(t, v.Root, tt.files...)
			it, err := v.trashAt(tt.rel, now)
			if err != nil {
				t.Fatalf("Trash: %v", err)
			}
			orig := clean(tt.rel)
			if it.OriginalPath != orig || it.Name != path.Base(orig) || it.IsDir != tt.isDir ||
				it.Host != "testhost" || !it.DeletedAt.Equal(now) {
				t.Errorf("item = %+v", it)
			}
			if !strings.HasPrefix(it.ID, "20260929100000-testhost-") {
				t.Errorf("ID = %q", it.ID)
			}
			if exists(v, orig) {
				t.Errorf("original %q still exists", orig)
			}
			cp := v.TrashContentPath(it)
			if want := ".trash/" + it.ID + "/" + it.Name; cp != want {
				t.Errorf("TrashContentPath = %q, want %q", cp, want)
			}
			for _, c := range tt.content {
				if !exists(v, path.Join(cp, c)) {
					t.Errorf("trashed content %q missing", path.Join(cp, c))
				}
			}
			var meta map[string]any
			if err := json.Unmarshal([]byte(readFile(t, v, ".trash/"+it.ID+"/meta.json")), &meta); err != nil {
				t.Fatalf("meta.json: %v", err)
			}
			want := map[string]any{
				"original_path": orig,
				"deleted_at":    "2026-09-29T10:00:00Z",
				"host":          "testhost",
				"is_dir":        tt.isDir,
			}
			for k, w := range want {
				if meta[k] != w {
					t.Errorf("meta[%q] = %v, want %v", k, meta[k], w)
				}
			}
		})
	}
}

func TestTrashPublicUsesNow(t *testing.T) {
	v := trashVault(t)
	mkfiles(t, v.Root, "A.md")
	before := time.Now().Add(-time.Second)
	it, err := v.Trash("A.md")
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if it.DeletedAt.Before(before) || it.DeletedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("DeletedAt = %v, want about now", it.DeletedAt)
	}
}

func TestTrashDefaultHost(t *testing.T) {
	v := openVault(t)
	mkfiles(t, v.Root, "A.md")
	it, err := v.Trash("A.md")
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if it.Host != shortHostname() || it.Host == "" || strings.Contains(it.Host, ".") {
		t.Errorf("Host = %q, want short hostname %q", it.Host, shortHostname())
	}
}

func TestTrashErrors(t *testing.T) {
	tests := []struct {
		name    string
		rel     string
		wantErr error
	}{
		{"vault root", "", ErrInvalidPath},
		{"inside trash", ".trash/x", ErrInvalidPath},
		{"trash itself", ".trash", ErrInvalidPath},
		{"notty dir", ".notty/state.json", ErrInvalidPath},
		{"git dir", ".git", ErrInvalidPath},
		{"missing", "Nope.md", os.ErrNotExist},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := trashVault(t)
			mkfiles(t, v.Root, ".trash/x", ".notty/state.json", ".git/HEAD")
			if _, err := v.Trash(tt.rel); !errors.Is(err, tt.wantErr) {
				t.Errorf("Trash(%q) err = %v, want %v", tt.rel, err, tt.wantErr)
			}
			// A failed trash leaves no item behind.
			items, err := v.TrashItems()
			if err != nil || len(items) != 0 {
				t.Errorf("TrashItems = %v, %v; want none", items, err)
			}
		})
	}
}

func TestTrashItems(t *testing.T) {
	v := trashVault(t)
	mkfiles(t, v.Root, "Old.md", "Mid.md", "New.md", "Dir/X.md")
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, rel := range []string{"Old.md", "Dir", "Mid.md", "New.md"} {
		if _, err := v.trashAt(rel, base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatalf("Trash(%q): %v", rel, err)
		}
	}
	// Broken entries are skipped.
	mkfiles(t, v.Root,
		".trash/no-meta/Note.md",
		".trash/stray-file",
	)
	writeFile(t, v, ".trash/bad-json/meta.json", "{not json")
	writeFile(t, v, ".trash/bad-time/meta.json", `{"original_path":"A.md","deleted_at":"yesterday"}`)
	writeFile(t, v, ".trash/no-path/meta.json", `{"original_path":"","deleted_at":"2026-09-01T00:00:00Z"}`)
	writeFile(t, v, ".trash/escape/meta.json", `{"original_path":"../x.md","deleted_at":"2026-09-01T00:00:00Z"}`)
	writeFile(t, v, ".trash/reserved/meta.json", `{"original_path":".git/config","deleted_at":"2026-09-01T00:00:00Z"}`)
	for i, orig := range []string{"a:b.md", "Work/x?.md", "Work /a.md", "Note.md ", `a\b.md`} {
		meta, _ := json.Marshal(map[string]any{"original_path": orig, "deleted_at": "2026-09-01T00:00:00Z"})
		writeFile(t, v, ".trash/unsanitized"+string(rune('a'+i))+"/meta.json", string(meta))
	}
	valid := `{"original_path":"Valid.md","deleted_at":"2026-09-01T00:00:00Z","host":"h"}`
	writeFile(t, v, ".trash/big/meta.json", valid+strings.Repeat(" ", 64<<10))
	mkfiles(t, v.Root, ".trash/meta-dir/meta.json/")
	outside := filepath.Join(t.TempDir(), "meta.json")
	if err := os.WriteFile(outside, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	mkfiles(t, v.Root, ".trash/meta-link/")
	if err := os.Symlink(outside, v.Abs(".trash/meta-link/meta.json")); err != nil {
		t.Logf("symlinks unavailable: %v", err)
	}

	items, err := v.TrashItems()
	if err != nil {
		t.Fatalf("TrashItems: %v", err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.OriginalPath)
	}
	want := []string{"New.md", "Mid.md", "Dir", "Old.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("TrashItems order = %v, want %v", got, want)
	}
	if !items[2].IsDir || items[2].Name != "Dir" || items[2].Host != "testhost" {
		t.Errorf("folder item = %+v", items[2])
	}
}

func TestTrashItemsEmpty(t *testing.T) {
	v := trashVault(t)
	items, err := v.TrashItems()
	if err != nil || len(items) != 0 {
		t.Errorf("TrashItems on vault without .trash = %v, %v", items, err)
	}
}

func TestRestore(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		trash    string
		setup    func(t *testing.T, v *Vault) // runs after trashing
		want     string
		wantFile string // file under the restored path to check
	}{
		{
			name:  "original path",
			files: []string{"Work/Note.md"},
			trash: "Work/Note.md",
			want:  "Work/Note.md",
		},
		{
			name:  "collision",
			files: []string{"Work/Note.md"},
			trash: "Work/Note.md",
			setup: func(t *testing.T, v *Vault) { mkfiles(t, v.Root, "Work/Note.md") },
			want:  "Work/Note (restored).md",
		},
		{
			name:  "second collision",
			files: []string{"Note.md"},
			trash: "Note.md",
			setup: func(t *testing.T, v *Vault) { mkfiles(t, v.Root, "Note.md", "Note (restored).md") },
			want:  "Note (restored 2).md",
		},
		{
			name:  "third collision",
			files: []string{"Note.md"},
			trash: "Note.md",
			setup: func(t *testing.T, v *Vault) {
				mkfiles(t, v.Root, "Note.md", "Note (restored).md", "Note (restored 2).md")
			},
			want: "Note (restored 3).md",
		},
		{
			name:  "missing parents recreated",
			files: []string{"A/B/C/Note.md"},
			trash: "A/B/C/Note.md",
			setup: func(t *testing.T, v *Vault) {
				if err := os.RemoveAll(v.Abs("A")); err != nil {
					t.Fatal(err)
				}
			},
			want: "A/B/C/Note.md",
		},
		{
			name:     "folder",
			files:    []string{"Work/A.md", "Work/Sub/B.md"},
			trash:    "Work",
			want:     "Work",
			wantFile: "Sub/B.md",
		},
		{
			name:     "folder collision",
			files:    []string{"Work/A.md"},
			trash:    "Work",
			setup:    func(t *testing.T, v *Vault) { mkfiles(t, v.Root, "Work/") },
			want:     "Work (restored)",
			wantFile: "A.md",
		},
		{
			name:  "non-note file collision keeps extension",
			files: []string{"pic.png"},
			trash: "pic.png",
			setup: func(t *testing.T, v *Vault) { mkfiles(t, v.Root, "pic.png") },
			want:  "pic (restored).png",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := trashVault(t)
			mkfiles(t, v.Root, tt.files...)
			it, err := v.Trash(tt.trash)
			if err != nil {
				t.Fatalf("Trash: %v", err)
			}
			if tt.setup != nil {
				tt.setup(t, v)
			}
			got, err := v.Restore(it)
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if got != tt.want {
				t.Errorf("Restore = %q, want %q", got, tt.want)
			}
			if !exists(v, path.Join(got, tt.wantFile)) {
				t.Errorf("restored %q missing", path.Join(got, tt.wantFile))
			}
			if exists(v, ".trash/"+it.ID) {
				t.Errorf("trash dir %q not removed", it.ID)
			}
			if items, _ := v.TrashItems(); len(items) != 0 {
				t.Errorf("TrashItems after restore = %v", items)
			}
		})
	}
}

func TestRestoreUnsafeAncestor(t *testing.T) {
	tests := []struct {
		name  string
		orig  string
		setup func(t *testing.T, v *Vault, outside string) // runs after trashing
	}{
		{"parent is a symlink", "Work/Note.md", func(t *testing.T, v *Vault, outside string) {
			if err := os.Symlink(outside, v.Abs("Work")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}},
		{"grandparent is a symlink", "A/B/Note.md", func(t *testing.T, v *Vault, outside string) {
			if err := os.Symlink(outside, v.Abs("A")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}},
		{"parent is a file", "Work/Note.md", func(t *testing.T, v *Vault, _ string) {
			mkfiles(t, v.Root, "Work")
		}},
		{"grandparent is a file", "A/B/Note.md", func(t *testing.T, v *Vault, _ string) {
			mkfiles(t, v.Root, "A")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := trashVault(t)
			outside := t.TempDir()
			mkfiles(t, v.Root, tt.orig)
			it, err := v.Trash(tt.orig)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(v.Abs(strings.Split(tt.orig, "/")[0])); err != nil {
				t.Fatal(err)
			}
			tt.setup(t, v, outside)
			if _, err := v.Restore(it); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("Restore err = %v, want ErrInvalidPath", err)
			}
			if !exists(v, v.TrashContentPath(it)) {
				t.Error("trashed content lost")
			}
			if entries, _ := os.ReadDir(outside); len(entries) != 0 {
				t.Errorf("Restore wrote %d entries outside the vault", len(entries))
			}
		})
	}
}

func TestTrashRefusesUnsanitizedNames(t *testing.T) {
	for _, rel := range []string{"a:b.md", "Work/x?.md", "Bad|dir/n.md"} {
		t.Run(rel, func(t *testing.T) {
			v := trashVault(t)
			abs := v.Abs(rel)
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				t.Skipf("cannot create %q here: %v", rel, err)
			}
			if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
				t.Skipf("cannot create %q here: %v", rel, err)
			}
			// It would be trashed but never listed (invalid metadata).
			if _, err := v.Trash(rel); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("Trash(%q) err = %v, want ErrInvalidPath", rel, err)
			}
			if !exists(v, rel) {
				t.Error("file moved despite the error")
			}
		})
	}
}

func TestRestoreKeepsContent(t *testing.T) {
	v := trashVault(t)
	writeFile(t, v, "Note.md", "# Note\n\nbody\n")
	it, err := v.Trash("Note.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Restore(it); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, v, "Note.md"); got != "# Note\n\nbody\n" {
		t.Errorf("content = %q", got)
	}
}

func TestRestoreErrors(t *testing.T) {
	tests := []struct {
		name    string
		item    func(real TrashItem) TrashItem
		wantErr error
	}{
		{"empty id", func(it TrashItem) TrashItem { it.ID = ""; return it }, ErrInvalidPath},
		{"id with slash", func(it TrashItem) TrashItem { it.ID = "a/b"; return it }, ErrInvalidPath},
		{"dotdot id", func(it TrashItem) TrashItem { it.ID = ".."; return it }, ErrInvalidPath},
		{"reserved original", func(it TrashItem) TrashItem { it.OriginalPath = ".git/x.md"; return it }, ErrInvalidPath},
		{"unsanitized original", func(it TrashItem) TrashItem { it.OriginalPath = "a:b.md"; return it }, ErrInvalidPath},
		{"dotdot name", func(it TrashItem) TrashItem { it.Name = ".."; return it }, ErrInvalidPath},
		{"name with slash", func(it TrashItem) TrashItem { it.Name = "a/b"; return it }, ErrInvalidPath},
		{"empty name", func(it TrashItem) TrashItem { it.Name = ""; return it }, ErrInvalidPath},
		{"empty original", func(it TrashItem) TrashItem { it.OriginalPath = ""; return it }, ErrInvalidPath},
		{"unknown id", func(it TrashItem) TrashItem { it.ID = "nope"; return it }, os.ErrNotExist},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := trashVault(t)
			mkfiles(t, v.Root, "Note.md")
			real, err := v.Trash("Note.md")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := v.Restore(tt.item(real)); !errors.Is(err, tt.wantErr) {
				t.Errorf("Restore err = %v, want %v", err, tt.wantErr)
			}
			if !exists(v, v.TrashContentPath(real)) {
				t.Error("trashed content lost after failed restore")
			}
		})
	}
}

func TestDeleteForever(t *testing.T) {
	v := trashVault(t)
	mkfiles(t, v.Root, "A.md", "B/C.md")
	a, err := v.Trash("A.md")
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.Trash("B")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteForever(b); err != nil {
		t.Fatalf("DeleteForever: %v", err)
	}
	if exists(v, ".trash/"+b.ID) {
		t.Error("deleted item still on disk")
	}
	items, err := v.TrashItems()
	if err != nil || len(items) != 1 || items[0].ID != a.ID {
		t.Errorf("TrashItems = %v, %v; want only %q", items, err, a.ID)
	}
	for _, id := range []string{"", ".", "..", "a/b", `a\b`} {
		if err := v.DeleteForever(TrashItem{ID: id}); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("DeleteForever(ID %q) err = %v, want ErrInvalidPath", id, err)
		}
	}
	if !exists(v, ".trash/"+a.ID) {
		t.Error("invalid DeleteForever removed another item")
	}
}

func TestEmptyTrash(t *testing.T) {
	v := trashVault(t)
	if err := v.EmptyTrash(); err != nil {
		t.Errorf("EmptyTrash without .trash: %v", err)
	}
	mkfiles(t, v.Root, "A.md", "B/C.md", "Keep.md")
	for _, rel := range []string{"A.md", "B"} {
		if _, err := v.Trash(rel); err != nil {
			t.Fatal(err)
		}
	}
	mkfiles(t, v.Root, ".trash/junk/file")
	if err := v.EmptyTrash(); err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	entries, err := os.ReadDir(v.Abs(".trash"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf(".trash still has %d entries", len(entries))
	}
	if !exists(v, "Keep.md") {
		t.Error("EmptyTrash touched a note outside the trash")
	}
}

func TestPurgeOlderThan(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	v := trashVault(t)
	ages := map[string]time.Duration{
		"Fresh.md":   0,
		"Week.md":    7 * day,
		"Exactly.md": 30 * day,
		"Old.md":     31 * day,
		"Ancient":    400 * day,
	}
	for rel, age := range ages {
		if strings.HasSuffix(rel, ".md") {
			mkfiles(t, v.Root, rel)
		} else {
			mkfiles(t, v.Root, rel+"/x.md")
		}
		if _, err := v.trashAt(rel, now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	n, err := v.PurgeOlderThan(30*day, now)
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if n != 2 {
		t.Errorf("purged %d, want 2", n)
	}
	items, err := v.TrashItems()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.OriginalPath)
	}
	if want := "Fresh.md,Week.md,Exactly.md"; strings.Join(got, ",") != want {
		t.Errorf("remaining = %v, want %s", got, want)
	}
}

func TestTrashFolderMustBeRealDirectory(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, v *Vault) (target string)
	}{
		{"symlink to directory", func(t *testing.T, v *Vault) string {
			target := t.TempDir()
			mkfiles(t, target, "keep.txt", "item/Note.md")
			writeFile(t, &Vault{Root: target}, "item/meta.json",
				`{"original_path":"Note.md","deleted_at":"2000-01-01T00:00:00Z","host":"h","is_dir":false}`)
			if err := os.Symlink(target, v.Abs(".trash")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return target
		}},
		{"regular file", func(t *testing.T, v *Vault) string {
			mkfiles(t, v.Root, ".trash")
			return ""
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := trashVault(t)
			mkfiles(t, v.Root, "A.md")
			target := tt.setup(t, v)

			if _, err := v.Trash("A.md"); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("Trash err = %v, want ErrInvalidPath", err)
			}
			if !exists(v, "A.md") {
				t.Error("Trash moved the note despite the error")
			}
			if _, err := v.TrashItems(); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("TrashItems err = %v, want ErrInvalidPath", err)
			}
			if err := v.EmptyTrash(); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("EmptyTrash err = %v, want ErrInvalidPath", err)
			}
			if err := v.DeleteForever(TrashItem{ID: "item"}); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("DeleteForever err = %v, want ErrInvalidPath", err)
			}
			if _, err := v.PurgeOlderThan(time.Hour, time.Now()); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("PurgeOlderThan err = %v, want ErrInvalidPath", err)
			}
			it := TrashItem{ID: "item", Name: "Note.md", OriginalPath: "Note.md"}
			if _, err := v.Restore(it); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("Restore err = %v, want ErrInvalidPath", err)
			}
			if target != "" {
				for _, p := range []string{"keep.txt", "item/meta.json", "item/Note.md"} {
					if _, err := os.Lstat(filepath.Join(target, p)); err != nil {
						t.Errorf("symlink target lost %s: %v", p, err)
					}
				}
				if entries, _ := os.ReadDir(target); len(entries) != 2 {
					t.Errorf("symlink target has %d entries, want 2 (nothing trashed into it)", len(entries))
				}
			}
		})
	}
}

func TestSymlinkedTrashItem(t *testing.T) {
	v := trashVault(t)
	target := t.TempDir()
	mkfiles(t, target, "Note.md")
	writeFile(t, &Vault{Root: target}, "meta.json",
		`{"original_path":"Note.md","deleted_at":"2000-01-01T00:00:00Z","host":"h","is_dir":false}`)
	mkfiles(t, v.Root, ".trash/")
	if err := os.Symlink(target, v.Abs(".trash/evil")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if items, err := v.TrashItems(); err != nil || len(items) != 0 {
		t.Errorf("TrashItems = %v, %v; want symlinked item skipped", items, err)
	}
	it := TrashItem{ID: "evil", Name: "Note.md", OriginalPath: "Note.md"}
	if _, err := v.Restore(it); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("Restore err = %v, want ErrInvalidPath", err)
	}
	if exists(v, "Note.md") {
		t.Error("Restore pulled a file in from outside the vault")
	}
	if err := v.DeleteForever(it); err != nil {
		t.Fatalf("DeleteForever: %v", err)
	}
	if exists(v, ".trash/evil") {
		t.Error("symlink not removed")
	}
	for _, p := range []string{"Note.md", "meta.json"} {
		if _, err := os.Stat(filepath.Join(target, p)); err != nil {
			t.Errorf("DeleteForever followed the symlink: %s: %v", p, err)
		}
	}
}
