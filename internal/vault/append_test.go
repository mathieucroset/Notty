package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestValidateFolderPath(t *testing.T) {
	tests := []struct {
		name, in, want string
		wantErr        bool
	}{
		{"root", "", "", false},
		{"root slash", "/", "", false},
		{"plain", "Work", "Work", false},
		{"nested", "Work/Clients", "Work/Clients", false},
		{"surrounding slashes trimmed", "/Work/Clients/", "Work/Clients", false},
		{"surrounding spaces trimmed", "  Work ", "Work", false},
		{"unicode", "Café/☕", "Café/☕", false},
		{"inner dot kept", "v1.2", "v1.2", false},
		{"not reserved below the root", "Work/attachments", "Work/attachments", false},
		{"empty segment", "Work//Clients", "", true},
		{"dot segment", "Work/./Clients", "", true},
		{"dot dot segment", "Work/../Clients", "", true},
		{"only dot dot", "..", "", true},
		{"dot folder", ".hidden", "", true},
		{"nested dot folder", "Work/.hidden", "", true},
		{"trash", ".trash", "", true},
		{"git inside", ".git/objects", "", true},
		{"attachments", "attachments", "", true},
		{"attachments any case", "Attachments/x", "", true},
		{"forbidden character", "Wo:rk", "", true},
		{"segment with spaces around", "Work/ Clients", "", true},
		{"control character", "Wo\trk", "", true},
		{"drive letter", "C:/Work", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateFolderPath(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateFolderPath(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ValidateFolderPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A backslash separates folders on Windows only; elsewhere it is a
// forbidden name character.
func TestValidateFolderPathBackslash(t *testing.T) {
	got, err := ValidateFolderPath(`Work\Clients`)
	if runtime.GOOS == "windows" {
		if err != nil || got != "Work/Clients" {
			t.Errorf("got %q, %v; want Work/Clients", got, err)
		}
		return
	}
	if err == nil {
		t.Errorf("got %q, want an error", got)
	}
}

func TestRealFolderCase(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"root", "", ""},
		{"exact", "Work", "Work"},
		{"other case", "work", "Work"},
		{"nested", "WORK/clients", "Work/Clients"},
		{"missing tail keeps its case", "work/New/sub", "Work/New/sub"},
		{"missing", "Nope", "Nope"},
		{"a file is not a folder", "NOTES.MD/x", "NOTES.MD/x"},
		{"a symlink is not a folder", "EXT", "EXT"},
	}
	v := openVault(t)
	mkfiles(t, v.Root, "Work/Clients/", "notes.md")
	symlinks := os.Symlink(t.TempDir(), v.Abs("ext")) == nil
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.in == "EXT" && !symlinks {
				t.Skip("symlinks unsupported")
			}
			if got := v.RealFolderCase(tt.in); got != tt.want {
				t.Errorf("RealFolderCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// With folders differing only in case (case-sensitive filesystems), the
// exact spelling wins.
func TestRealFolderCasePrefersExact(t *testing.T) {
	v := openVault(t)
	mkfiles(t, v.Root, "Work/", "work/")
	if entries, _ := os.ReadDir(v.Root); len(entries) != 2 {
		t.Skip("case-insensitive filesystem")
	}
	for _, name := range []string{"Work", "work"} {
		if got := v.RealFolderCase(name); got != name {
			t.Errorf("RealFolderCase(%q) = %q", name, got)
		}
	}
}

func TestAppendToInbox(t *testing.T) {
	tests := []struct {
		name     string
		folder   string
		pre      *string // existing Inbox content; nil for none
		wantRel  string
		wantBody string
	}{
		{"new at root", "", nil, "Inbox.md", "# Inbox\n\n- hi\n"},
		{"new in a missing folder", "Work/Clients", nil, "Work/Clients/Inbox.md", "# Inbox\n\n- hi\n"},
		{"existing with newline", "", ptr("# Inbox\n\n- a\n"), "Inbox.md", "# Inbox\n\n- a\n- hi\n"},
		{"existing without newline", "", ptr("# Inbox\n\n- a"), "Inbox.md", "# Inbox\n\n- a\n- hi\n"},
		{"existing empty", "Work", ptr(""), "Work/Inbox.md", "- hi\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			if tt.pre != nil {
				if err := os.MkdirAll(filepath.Dir(v.Abs(tt.wantRel)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(v.Abs(tt.wantRel), []byte(*tt.pre), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			rel, err := v.AppendToInbox(tt.folder, "- hi")
			if err != nil {
				t.Fatalf("AppendToInbox: %v", err)
			}
			if rel != tt.wantRel {
				t.Errorf("rel = %q, want %q", rel, tt.wantRel)
			}
			if got := readFile(t, v, tt.wantRel); got != tt.wantBody {
				t.Errorf("content = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

func TestAppendToInboxRefuses(t *testing.T) {
	tests := []struct {
		name   string
		folder string
		setup  func(t *testing.T, v *Vault)
	}{
		{"invalid folder", "../out", nil},
		{"reserved folder", ".trash", nil},
		{"inbox is a folder", "", func(t *testing.T, v *Vault) { mkfiles(t, v.Root, "Inbox.md/") }},
		{"inbox is a symlink", "", func(t *testing.T, v *Vault) {
			mkfiles(t, v.Root, "target.md")
			if err := os.Symlink("target.md", v.Abs("Inbox.md")); err != nil {
				t.Skipf("symlinks unsupported: %v", err)
			}
		}},
		{"folder is a symlink", "Work", func(t *testing.T, v *Vault) {
			outside := t.TempDir()
			if err := os.Symlink(outside, v.Abs("Work")); err != nil {
				t.Skipf("symlinks unsupported: %v", err)
			}
		}},
		{"folder is a file", "Work/Sub", func(t *testing.T, v *Vault) { mkfiles(t, v.Root, "Work") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			if tt.setup != nil {
				tt.setup(t, v)
			}
			if _, err := v.AppendToInbox(tt.folder, "- hi"); err == nil {
				t.Fatal("AppendToInbox succeeded, want an error")
			}
			if exists(v, "target.md") {
				if got := readFile(t, v, "target.md"); got != "x" {
					t.Errorf("symlink target changed to %q", got)
				}
			}
		})
	}
}

func TestAppendToNote(t *testing.T) {
	tests := []struct {
		name, pre, text, want string
	}{
		{"with trailing newline", "# N\n\nbody\n", "moved\n", "# N\n\nbody\nmoved\n"},
		{"without trailing newline", "# N\n\nbody", "moved\n", "# N\n\nbody\nmoved\n"},
		{"empty note", "", "moved\n", "moved\n"},
		{"several lines", "a\n", "b\nc\n", "a\nb\nc\n"},
		{"text without final newline", "a\n", "b", "a\nb\n"},
		{"CRLF note", "a\r\n", "b\n", "a\r\nb\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			if err := os.MkdirAll(v.Abs("Work"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(v.Abs("Work/N.md"), []byte(tt.pre), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := v.AppendToNote("Work/N.md", tt.text); err != nil {
				t.Fatalf("AppendToNote: %v", err)
			}
			if got := readFile(t, v, "Work/N.md"); got != tt.want {
				t.Errorf("content = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppendToNoteRefuses(t *testing.T) {
	tests := []struct {
		name, rel string
		setup     func(t *testing.T, v *Vault)
		wantErr   error
	}{
		{"missing", "N.md", nil, os.ErrNotExist},
		{"vault root", "", nil, ErrInvalidPath},
		{"folder", "Work", func(t *testing.T, v *Vault) { mkfiles(t, v.Root, "Work/") }, ErrInvalidPath},
		{"reserved", ".trash/x.md", func(t *testing.T, v *Vault) { mkfiles(t, v.Root, ".trash/x.md") }, ErrInvalidPath},
		{"symlink", "link.md", func(t *testing.T, v *Vault) {
			mkfiles(t, v.Root, "target.md")
			if err := os.Symlink("target.md", v.Abs("link.md")); err != nil {
				t.Skipf("symlinks unsupported: %v", err)
			}
		}, ErrInvalidPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := openVault(t)
			if tt.setup != nil {
				tt.setup(t, v)
			}
			err := v.AppendToNote(tt.rel, "moved\n")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AppendToNote error = %v, want %v", err, tt.wantErr)
			}
			if exists(v, "target.md") {
				if got := readFile(t, v, "target.md"); got != "x" {
					t.Errorf("symlink target changed to %q", got)
				}
			}
			if exists(v, ".trash/x.md") {
				if got := readFile(t, v, ".trash/x.md"); got != "x" {
					t.Errorf("reserved file changed to %q", got)
				}
			}
		})
	}
}

// Two captures at the same moment must not drop each other's line, whether
// the Inbox exists already or is created by one of them.
func TestAppendToInboxConcurrent(t *testing.T) {
	const n = 20
	for _, existing := range []bool{true, false} {
		t.Run(fmt.Sprintf("existing=%v", existing), func(t *testing.T) {
			v := openVault(t)
			if existing {
				if err := os.WriteFile(v.Abs("Inbox.md"), []byte("# Inbox\n\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var wg sync.WaitGroup
			errs := make(chan error, n)
			for i := range n {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, err := v.AppendToInbox("", fmt.Sprintf("- line %d", i)); err != nil {
						errs <- err
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Errorf("AppendToInbox: %v", err)
			}
			got := readFile(t, v, "Inbox.md")
			if c := strings.Count(got, "# Inbox\n"); c != 1 {
				t.Errorf("%d headers in %q, want 1", c, got)
			}
			for i := range n {
				if !strings.Contains(got, fmt.Sprintf("- line %d\n", i)) {
					t.Errorf("line %d missing from %q", i, got)
				}
			}
		})
	}
}

func TestAppendToNoteConcurrent(t *testing.T) {
	const n = 20
	v := openVault(t)
	if err := os.WriteFile(v.Abs("N.md"), []byte("# N\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := v.AppendToNote("N.md", fmt.Sprintf("line %d\n", i)); err != nil {
				t.Errorf("AppendToNote: %v", err)
			}
		}()
	}
	wg.Wait()
	got := readFile(t, v, "N.md")
	if lines := strings.Count(got, "\n"); lines != n+1 {
		t.Errorf("%d lines in %q, want %d", lines, got, n+1)
	}
	for i := range n {
		if !strings.Contains(got, fmt.Sprintf("line %d\n", i)) {
			t.Errorf("line %d missing from %q", i, got)
		}
	}
}
