package attach

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/vault"
)

func openVault(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatalf("vault.Open: %v", err)
	}
	return v
}

func TestSlug(t *testing.T) {
	tests := []struct {
		name    string
		noteRel string
		want    string
	}{
		{"simple", "Standup notes.md", "standup-notes"},
		{"nested path uses base", "Work/Project Plan.md", "project-plan"},
		{"unsafe chars collapse", "a/b__c!!d.md", "b-c-d"},
		{"leading and trailing dashes trimmed", "-oops-.md", "oops"},
		{"only unsafe chars falls back", "!!!.md", "note"},
		{"empty falls back", ".md", "note"},
		{"uppercase lowered", "MY NOTE.md", "my-note"},
		{"multiple spaces collapse", "a    b.md", "a-b"},
		{"long name truncated to 40", strings.Repeat("a", 60) + ".md", strings.Repeat("a", 40)},
		{"truncation avoids trailing dash", strings.Repeat("a", 39) + "-bbbb.md", strings.Repeat("a", 39)},
		{"non-md name still uses basename sans ext", "diagram.png", "diagram"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slug(tt.noteRel)
			if got != tt.want {
				t.Errorf("Slug(%q) = %q, want %q", tt.noteRel, got, tt.want)
			}
			if len(got) > maxSlugLen {
				t.Errorf("Slug(%q) = %q, longer than %d", tt.noteRel, got, maxSlugLen)
			}
		})
	}
}

func TestImport(t *testing.T) {
	now := time.Date(2026, 9, 29, 14, 3, 7, 0, time.UTC)

	t.Run("writes file and returns vault-root link", func(t *testing.T) {
		v := openVault(t)
		link, err := Import(v, "Work/Standup notes.md", []byte("fake-png-data"), "PNG", now)
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		re := regexp.MustCompile(`^!\[\]\(/attachments/standup-notes-20260929-140307-[a-z0-9]{4}\.png\)$`)
		if !re.MatchString(link) {
			t.Fatalf("Import link = %q, want match of %s", link, re)
		}

		// Extract the file name from the link and check its content and
		// that no temp file was left behind.
		name := strings.TrimSuffix(strings.TrimPrefix(link, "![](/attachments/"), ")")
		data, err := os.ReadFile(filepath.Join(v.Root, "attachments", name))
		if err != nil {
			t.Fatalf("read imported file: %v", err)
		}
		if string(data) != "fake-png-data" {
			t.Errorf("imported file content = %q, want %q", data, "fake-png-data")
		}

		entries, err := os.ReadDir(filepath.Join(v.Root, "attachments"))
		if err != nil {
			t.Fatalf("ReadDir attachments: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("attachments/ has %d entries, want 1 (no leftover temp file): %v", len(entries), entries)
		}
	})

	t.Run("random suffix varies", func(t *testing.T) {
		v := openVault(t)
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			link, err := Import(v, "note.md", []byte("x"), "png", now)
			if err != nil {
				t.Fatalf("Import: %v", err)
			}
			seen[link] = true
		}
		if len(seen) < 2 {
			t.Error("Import produced the same link every time")
		}
	})

	t.Run("normalizes extension case and leading dot", func(t *testing.T) {
		v := openVault(t)
		link, err := Import(v, "note.md", []byte("x"), ".JPG", now)
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		if !strings.HasSuffix(link, ".jpg)") {
			t.Errorf("Import link = %q, want lowercase .jpg extension", link)
		}
	})

	t.Run("rejects unsupported extension", func(t *testing.T) {
		v := openVault(t)
		_, err := Import(v, "note.md", []byte("x"), "bmp", now)
		if !errors.Is(err, ErrNotImage) {
			t.Fatalf("Import err = %v, want ErrNotImage", err)
		}
	})

	for _, ext := range []string{"png", "jpg", "jpeg", "gif", "webp"} {
		t.Run("accepts "+ext, func(t *testing.T) {
			v := openVault(t)
			if _, err := Import(v, "note.md", []byte("x"), ext, now); err != nil {
				t.Errorf("Import with ext %q: %v", ext, err)
			}
		})
	}
}

func TestImportPath(t *testing.T) {
	now := time.Date(2026, 9, 29, 14, 3, 7, 0, time.UTC)

	t.Run("imports an existing image file", func(t *testing.T) {
		v := openVault(t)
		src := filepath.Join(t.TempDir(), "photo.jpg")
		if err := os.WriteFile(src, []byte("jpeg-bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		link, err := ImportPath(v, "note.md", src, now)
		if err != nil {
			t.Fatalf("ImportPath: %v", err)
		}
		if !strings.HasPrefix(link, "![](/attachments/note-") || !strings.HasSuffix(link, ".jpg)") {
			t.Errorf("ImportPath link = %q", link)
		}
	})

	t.Run("rejects missing file", func(t *testing.T) {
		v := openVault(t)
		_, err := ImportPath(v, "note.md", filepath.Join(t.TempDir(), "missing.png"), now)
		if err == nil {
			t.Fatal("ImportPath: want error for missing file")
		}
	})

	t.Run("rejects non-regular file", func(t *testing.T) {
		v := openVault(t)
		dir := filepath.Join(t.TempDir(), "adir.png")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := ImportPath(v, "note.md", dir, now)
		if !errors.Is(err, ErrNotImage) {
			t.Fatalf("ImportPath err = %v, want ErrNotImage", err)
		}
	})

	t.Run("rejects unsupported extension", func(t *testing.T) {
		v := openVault(t)
		src := filepath.Join(t.TempDir(), "doc.txt")
		if err := os.WriteFile(src, []byte("text"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := ImportPath(v, "note.md", src, now)
		if !errors.Is(err, ErrNotImage) {
			t.Fatalf("ImportPath err = %v, want ErrNotImage", err)
		}
	})
}
