package attach

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// withUnsupportedLink overrides linkFile to always fail with an error that
// is not fs.ErrExist, simulating a filesystem without hard link support
// (exFAT, FAT, some network shares).
func withUnsupportedLink(t *testing.T) {
	t.Helper()
	orig := linkFile
	linkFile = func(oldname, newname string) error {
		return errors.New("link: operation not supported by filesystem")
	}
	t.Cleanup(func() { linkFile = orig })
}

func TestImportFallsBackWhenHardLinksUnsupported(t *testing.T) {
	now := time.Date(2026, 9, 29, 14, 3, 7, 0, time.UTC)
	v := openVault(t)

	withUnsupportedLink(t)

	link, err := Import(v, "note.md", []byte("fallback-data"), "png", now)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	name := strings.TrimSuffix(strings.TrimPrefix(link, "![](/attachments/"), ")")
	data, err := os.ReadFile(v.Abs("attachments/" + name))
	if err != nil {
		t.Fatalf("read imported file: %v", err)
	}
	if string(data) != "fallback-data" {
		t.Errorf("imported file content = %q, want %q", data, "fallback-data")
	}

	// No leftover temp file.
	entries, err := os.ReadDir(v.Abs(attachDir))
	if err != nil {
		t.Fatalf("ReadDir attachments: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("attachments/ has %d entries, want 1 (no leftover temp file): %v", len(entries), entries)
	}
}

func TestImportFallbackStillDetectsCollisionAndRetries(t *testing.T) {
	now := time.Date(2026, 9, 29, 14, 3, 7, 0, time.UTC)
	v := openVault(t)

	collidingRel := "attachments/note-20260929-140307-aaaa.png"
	if err := v.Save(collidingRel, "existing-data"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	withUnsupportedLink(t)
	withFixedSuffixes(t, "aaaa", "bbbb")

	link, err := Import(v, "note.md", []byte("new-data"), "png", now)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if strings.Contains(link, "-aaaa.") {
		t.Fatalf("Import reused the colliding suffix: %s", link)
	}
	if !strings.Contains(link, "-bbbb.") {
		t.Fatalf("Import link = %q, want it to retry onto suffix bbbb", link)
	}

	data, err := os.ReadFile(v.Abs(collidingRel))
	if err != nil {
		t.Fatalf("read colliding file: %v", err)
	}
	if string(data) != "existing-data" {
		t.Fatalf("colliding file was overwritten: got %q, want %q", data, "existing-data")
	}
}
