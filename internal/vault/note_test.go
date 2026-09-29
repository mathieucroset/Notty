package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFile(t *testing.T, v *Vault, rel string) string {
	t.Helper()
	s, err := v.Read(rel)
	if err != nil {
		t.Fatalf("Read(%q): %v", rel, err)
	}
	return s
}

func exists(v *Vault, rel string) bool {
	_, err := os.Lstat(v.Abs(rel))
	return err == nil
}

func assertNoTmp(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(p, ".notty-tmp") {
			t.Errorf("leftover temp file %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSaveAndRead(t *testing.T) {
	tests := []struct {
		name, rel, content string
		pre                string // existing content, "" for none
	}{
		{"new file at root", "a.md", "hello\n", ""},
		{"creates parents", "x/y/z/deep.md", "deep", ""},
		{"overwrites", "b.md", "new", "old content that is longer"},
		{"empty content", "e.md", "", ""},
		{"unicode", "u.md", "Café ☕\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			if tt.pre != "" {
				if err := v.Save(tt.rel, tt.pre); err != nil {
					t.Fatalf("pre Save: %v", err)
				}
			}
			if err := v.Save(tt.rel, tt.content); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if got := readFile(t, v, tt.rel); got != tt.content {
				t.Errorf("Read = %q, want %q", got, tt.content)
			}
			assertNoTmp(t, v.Root)
		})
	}
}

func TestSavePreservesMode(t *testing.T) {
	v := openVault(t)
	mkfiles(t, v.Root, "m.md")
	if err := os.Chmod(v.Abs("m.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := v.Save("m.md", "x"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(v.Abs("m.md"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestSaveReplacesSymlink(t *testing.T) {
	v := openVault(t)
	if err := os.WriteFile(v.Abs("target.md"), []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.md", v.Abs("link.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := v.Save("link.md", "new"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Lstat(v.Abs("link.md"))
	if err != nil || !fi.Mode().IsRegular() {
		t.Errorf("link.md should now be a regular file: %v %v", fi, err)
	}
	if got := readFile(t, v, "target.md"); got != "orig" {
		t.Errorf("target changed to %q", got)
	}
	if got := readFile(t, v, "link.md"); got != "new" {
		t.Errorf("link.md = %q, want new", got)
	}
}

func TestSaveErrors(t *testing.T) {
	v := openVault(t)
	mkfiles(t, v.Root, "dir/")
	for _, rel := range []string{"", ".", "..", "dir"} {
		if err := v.Save(rel, "x"); err == nil {
			t.Errorf("Save(%q): want error", rel)
		}
	}
	assertNoTmp(t, v.Root)
}

func TestSaveCannotEscapeRoot(t *testing.T) {
	parent := t.TempDir()
	v, err := Open(filepath.Join(parent, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Save("../outside.md", "x"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "outside.md")); err == nil {
		t.Error("Save wrote outside the vault root")
	}
	if !exists(v, "outside.md") {
		t.Error("Save did not write the cleaned path inside the vault")
	}
}

func TestReadMissing(t *testing.T) {
	v := openVault(t)
	if _, err := v.Read("nope.md"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read missing: err = %v, want ErrNotExist", err)
	}
}

func TestCreateNote(t *testing.T) {
	v := openVault(t)
	tests := []struct {
		name, folder, title, wantRel, wantContent string
	}{
		{"root", "", "Standup notes", "Standup notes.md", "# Standup notes\n\n"},
		{"collision 2", "", "Standup notes", "Standup notes 2.md", "# Standup notes\n\n"},
		{"collision 3", "", "Standup notes", "Standup notes 3.md", "# Standup notes\n\n"},
		{"in folder", "Work", "Plan", "Work/Plan.md", "# Plan\n\n"},
		{"in folder collision", "Work", "Plan", "Work/Plan 2.md", "# Plan\n\n"},
		{"new nested folder", "A/B", "x", "A/B/x.md", "# x\n\n"},
		{"sanitized filename", "", " ..a/b:c ", "abc.md", "# ..a/b:c\n\n"},
		{"empty title", "", "", "Untitled.md", "# Untitled\n\n"},
		{"empty title collision", "", "  ", "Untitled 2.md", "# Untitled\n\n"},
		{"md title not doubled", "", "notes.md", "notes.md", "# notes.md\n\n"},
		{"md title collision", "", "notes.MD", "notes 2.md", "# notes.MD\n\n"},
		{"only md title", "", ".md", "Untitled 3.md", "# .md\n\n"},
		{"control chars in heading", "", "x\ny\tz\r", "xyz.md", "# x y z\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel, err := v.CreateNote(tt.folder, tt.title)
			if err != nil {
				t.Fatalf("CreateNote: %v", err)
			}
			if rel != tt.wantRel {
				t.Errorf("rel = %q, want %q", rel, tt.wantRel)
			}
			if got := readFile(t, v, rel); got != tt.wantContent {
				t.Errorf("content = %q, want %q", got, tt.wantContent)
			}
		})
	}
	assertNoTmp(t, v.Root)
}

func TestCreateNoteCollidesWithFolder(t *testing.T) {
	v := openVault(t)
	mkfiles(t, v.Root, "Idea.md/")
	rel, err := v.CreateNote("", "Idea")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "Idea 2.md" {
		t.Errorf("rel = %q, want %q", rel, "Idea 2.md")
	}
}

func TestCreateFolder(t *testing.T) {
	v := openVault(t)
	mkfiles(t, v.Root, "Taken")
	tests := []struct {
		name, parent, in, want string
	}{
		{"root", "", "Work", "Work"},
		{"collision 2", "", "Work", "Work 2"},
		{"collision 3", "", "Work", "Work 3"},
		{"nested", "Work", "Sub", "Work/Sub"},
		{"sanitized", "", " .a:b* ", "ab"},
		{"collides with file", "", "Taken", "Taken 2"},
		{"empty", "", "", "Untitled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := v.CreateFolder(tt.parent, tt.in)
			if err != nil {
				t.Fatalf("CreateFolder: %v", err)
			}
			if got != tt.want {
				t.Errorf("rel = %q, want %q", got, tt.want)
			}
			fi, err := os.Stat(v.Abs(got))
			if err != nil || !fi.IsDir() {
				t.Errorf("%q is not a directory: %v", got, err)
			}
		})
	}
}

func TestRename(t *testing.T) {
	tests := []struct {
		name, rel, newName, want string
	}{
		{"note keeps md", "Work/a.md", "b", "Work/b.md"},
		{"note with md", "Work/a.md", "b.md", "Work/b.md"},
		{"note with upper md", "Work/a.md", "b.MD", "Work/b.md"},
		{"upper md note normalized", "Work/U.MD", "v", "Work/v.md"},
		{"note sanitized", "Work/a.md", " .x/y ", "Work/xy.md"},
		{"note same name", "Work/a.md", "a", "Work/a.md"},
		{"case only", "Work/a.md", "A", "Work/A.md"},
		{"folder", "Work", "Job", "Job"},
		{"folder no md appended", "Work", "Job.md", "Job.md"},
		{"other file", "Work/pic.png", "photo.png", "Work/photo.png"},
		{"other file no md", "Work/pic.png", "photo", "Work/photo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			mkfiles(t, v.Root, "Work/a.md", "Work/U.MD", "Work/pic.png")
			before := readFile(t, v, tt.rel+suffixIfDir(v, tt.rel))
			got, err := v.Rename(tt.rel, tt.newName)
			if err != nil {
				t.Fatalf("Rename: %v", err)
			}
			if got != tt.want {
				t.Errorf("newRel = %q, want %q", got, tt.want)
			}
			if after := readFile(t, v, got+suffixIfDir(v, got)); after != before {
				t.Errorf("content changed: %q -> %q", before, after)
			}
			if got != tt.rel && !strings.EqualFold(got, tt.rel) && exists(v, tt.rel) {
				t.Errorf("old path %q still exists", tt.rel)
			}
		})
	}
}

// suffixIfDir returns "/a.md" when rel is a directory, so tests can read a
// file inside a renamed folder.
func suffixIfDir(v *Vault, rel string) string {
	if fi, err := os.Stat(v.Abs(rel)); err == nil && fi.IsDir() {
		return "/a.md"
	}
	return ""
}

func TestRenameErrors(t *testing.T) {
	tests := []struct {
		name, rel, newName string
		wantErr            error
	}{
		{"target note exists", "a.md", "b", ErrExists},
		{"target folder exists", "F", "G", ErrExists},
		{"missing source", "nope.md", "x", os.ErrNotExist},
		{"empty name", "a.md", "  ", ErrInvalidName},
		{"only md", "a.md", ".md", ErrInvalidName},
		{"only upper md", "a.md", " .MD ", ErrInvalidName},
		{"folder empty name", "F", "...", ErrInvalidName},
		{"root", "", "x", ErrInvalidPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			mkfiles(t, v.Root, "a.md", "b.md", "F/", "G/")
			_, err := v.Rename(tt.rel, tt.newName)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if errors.Is(tt.wantErr, ErrExists) && !exists(v, tt.rel) {
				t.Errorf("source %q vanished after failed rename", tt.rel)
			}
		})
	}
}

func TestMove(t *testing.T) {
	tests := []struct {
		name, rel, dest, want string
		check                 string // a path that must exist afterwards
	}{
		{"note to folder", "a.md", "Work", "Work/a.md", "Work/a.md"},
		{"note to root", "Work/w.md", "", "w.md", "w.md"},
		{"note to new folder", "a.md", "New/Sub", "New/Sub/a.md", "New/Sub/a.md"},
		{"folder into folder", "Work", "Archive", "Archive/Work", "Archive/Work/w.md"},
		{"nested folder to root", "Archive/Old", "", "Old", "Old/o.md"},
		{"same folder no-op", "Work/w.md", "Work", "Work/w.md", "Work/w.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			mkfiles(t, v.Root, "a.md", "Work/w.md", "Archive/Old/o.md")
			got, err := v.Move(tt.rel, tt.dest)
			if err != nil {
				t.Fatalf("Move: %v", err)
			}
			if got != tt.want {
				t.Errorf("newRel = %q, want %q", got, tt.want)
			}
			if !exists(v, tt.check) {
				t.Errorf("%q missing after move", tt.check)
			}
			if got != tt.rel && exists(v, tt.rel) {
				t.Errorf("old path %q still exists", tt.rel)
			}
		})
	}
}

func TestMoveErrors(t *testing.T) {
	tests := []struct {
		name, rel, dest string
		wantErr         error
	}{
		{"target exists", "a.md", "Work", ErrExists},
		{"folder into itself", "Work", "Work", ErrInvalidPath},
		{"folder into descendant", "Work", "Work/Sub", ErrInvalidPath},
		{"folder into itself other case", "Work", "WORK", ErrInvalidPath},
		{"folder into descendant other case", "Work", "work/Sub", ErrInvalidPath},
		{"missing source", "nope.md", "Work", os.ErrNotExist},
		{"dest is a file", "b.md", "a.md", ErrInvalidPath},
		{"root", "", "Work", ErrInvalidPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			mkfiles(t, v.Root, "a.md", "b.md", "Work/a.md", "Work/Sub/", "WorkX/")
			_, err := v.Move(tt.rel, tt.dest)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if !errors.Is(tt.wantErr, os.ErrNotExist) && tt.rel != "" && !exists(v, tt.rel) {
				t.Errorf("source %q vanished after failed move", tt.rel)
			}
		})
	}
}

func TestMoveSiblingPrefixAllowed(t *testing.T) {
	// "Work" -> "WorkX" must not be mistaken for moving into a descendant.
	v := openVault(t)
	mkfiles(t, v.Root, "Work/a.md", "WorkX/")
	got, err := v.Move("Work", "WorkX")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got != "WorkX/Work" {
		t.Errorf("newRel = %q, want WorkX/Work", got)
	}
}

func TestErrorPrefixOnce(t *testing.T) {
	v := openVault(t)
	mkfiles(t, v.Root, "a.md", "b.md", "file")
	tests := []struct {
		name string
		op   func() error
	}{
		{"read", func() error { _, err := v.Read("nope.md"); return err }},
		{"save root", func() error { return v.Save("", "x") }},
		{"create note in file", func() error { _, err := v.CreateNote("file", "x"); return err }},
		{"create folder in file", func() error { _, err := v.CreateFolder("file", "x"); return err }},
		{"rename exists", func() error { _, err := v.Rename("a.md", "b"); return err }},
		{"rename missing", func() error { _, err := v.Rename("nope.md", "x"); return err }},
		{"move missing", func() error { _, err := v.Move("nope.md", ""); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op()
			if err == nil {
				t.Fatal("want error")
			}
			if n := strings.Count(err.Error(), "vault:"); n != 1 {
				t.Errorf("%q has %d \"vault:\" prefixes, want 1", err, n)
			}
		})
	}
}

func TestMoveOntoHardLinkRefused(t *testing.T) {
	// A hard link is the same file but a different name: renaming onto it
	// must fail rather than silently "succeed" as a no-op rename.
	tests := []struct {
		name string
		op   func(v *Vault) (string, error)
	}{
		{"rename", func(v *Vault) (string, error) { return v.Rename("a.md", "b") }},
		{"move", func(v *Vault) (string, error) { return v.Move("a.md", "Work") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			mkfiles(t, v.Root, "a.md", "Work/")
			for _, l := range []string{"b.md", "Work/a.md"} {
				if err := os.Link(v.Abs("a.md"), v.Abs(l)); err != nil {
					t.Skipf("hard links unsupported: %v", err)
				}
			}
			if _, err := tt.op(v); !errors.Is(err, ErrExists) {
				t.Errorf("err = %v, want ErrExists", err)
			}
			if !exists(v, "a.md") {
				t.Error("a.md vanished")
			}
		})
	}
}

func TestCreateFolderSkipsReservedNames(t *testing.T) {
	tests := []struct {
		name, parent, in, want string
	}{
		{"attachments at root", "", "attachments", "attachments 2"},
		{"attachments any case", "", "Attachments", "Attachments 2"},
		{"attachments nested allowed", "Work", "attachments", "Work/attachments"},
		{"trash name without dot allowed", "", "trash", "trash"},
		{"temp suffix", "", "x.notty-tmp", "x.notty-tmp 2"},
		{"temp suffix nested", "Work", "y.notty-tmp", "Work/y.notty-tmp 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			got, err := v.CreateFolder(tt.parent, tt.in)
			if err != nil {
				t.Fatalf("CreateFolder: %v", err)
			}
			if got != tt.want {
				t.Errorf("rel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCreateInHiddenLocations(t *testing.T) {
	dirs := []string{".git", ".trash/123", ".notty", "attachments", "Attachments/sub", "Work/.hidden", "Work/x.notty-tmp"}
	for _, dir := range dirs {
		t.Run(dir, func(t *testing.T) {
			v := openVault(t)
			if _, err := v.CreateNote(dir, "n"); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("CreateNote(%q): err = %v, want ErrInvalidPath", dir, err)
			}
			if _, err := v.CreateFolder(dir, "f"); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("CreateFolder(%q): err = %v, want ErrInvalidPath", dir, err)
			}
			if exists(v, dir) {
				t.Errorf("%q was created", dir)
			}
		})
	}
}

func TestRenameMoveToReservedNames(t *testing.T) {
	rename := func(rel, name string) func(v *Vault) (string, error) {
		return func(v *Vault) (string, error) { return v.Rename(rel, name) }
	}
	move := func(rel, dest string) func(v *Vault) (string, error) {
		return func(v *Vault) (string, error) { return v.Move(rel, dest) }
	}
	tests := []struct {
		name    string
		op      func(v *Vault) (string, error)
		wantErr error
		want    string
	}{
		{"rename folder to attachments", rename("F", "attachments"), ErrInvalidName, ""},
		{"rename folder to Attachments", rename("F", "Attachments"), ErrInvalidName, ""},
		{"rename folder to temp name", rename("F", "x.notty-tmp"), ErrInvalidName, ""},
		{"rename file to temp name", rename("p.png", "p.notty-tmp"), ErrInvalidName, ""},
		{"rename nested folder to attachments ok", rename("Work/F", "attachments"), nil, "Work/attachments"},
		{"move into .trash", move("a.md", ".trash"), ErrInvalidPath, ""},
		{"move into .git", move("a.md", ".git/objects"), ErrInvalidPath, ""},
		{"move into .notty", move("a.md", ".notty"), ErrInvalidPath, ""},
		{"move into attachments", move("a.md", "attachments"), ErrInvalidPath, ""},
		{"move into Attachments", move("a.md", "Attachments"), ErrInvalidPath, ""},
		{"move nested attachments to root", move("Other/attachments", ""), ErrInvalidPath, ""},
		{"move nested attachments elsewhere ok", move("Other/attachments", "F"), nil, "F/attachments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			mkfiles(t, v.Root, "a.md", "p.png", "F/", "Work/F/", "Other/attachments/",
				".git/objects/", ".trash/", ".notty/", "attachments/")
			got, err := tt.op(v)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("newRel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSaveUnrestricted(t *testing.T) {
	v := openVault(t)
	for _, rel := range []string{".notty/state.json", ".trash/1/meta.json", "attachments/a.txt"} {
		if err := v.Save(rel, "x"); err != nil {
			t.Errorf("Save(%q): %v", rel, err)
		}
	}
}
