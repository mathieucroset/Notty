package attach

import (
	"os"
	"strings"
	"testing"
	"time"
)

// withFixedSuffixes overrides newSuffix to return each of suffixes in turn,
// then fall back to the real random generator, so a test can force Import's
// first attempt (or first few) to collide with an existing file.
func withFixedSuffixes(t *testing.T, suffixes ...string) {
	t.Helper()
	orig := newSuffix
	i := 0
	newSuffix = func() string {
		if i < len(suffixes) {
			s := suffixes[i]
			i++
			return s
		}
		return orig()
	}
	t.Cleanup(func() { newSuffix = orig })
}

// withConstantSuffix overrides newSuffix to always return s, so every
// attempt Import makes collides.
func withConstantSuffix(t *testing.T, s string) {
	t.Helper()
	orig := newSuffix
	newSuffix = func() string { return s }
	t.Cleanup(func() { newSuffix = orig })
}

func TestImportNeverOverwritesOnCollision(t *testing.T) {
	now := time.Date(2026, 9, 29, 14, 3, 7, 0, time.UTC)
	v := openVault(t)

	// Pre-create the file the first generated name (with suffix "aaaa")
	// would collide with.
	collidingRel := "attachments/note-20260929-140307-aaaa.png"
	if err := v.Save(collidingRel, "existing-data"); err != nil {
		t.Fatalf("Save: %v", err)
	}

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

	newData, err := os.ReadFile(v.Abs("attachments/note-20260929-140307-bbbb.png"))
	if err != nil {
		t.Fatalf("read new file: %v", err)
	}
	if string(newData) != "new-data" {
		t.Errorf("new file content = %q, want %q", newData, "new-data")
	}
}

func TestImportGivesUpAfterRepeatedCollisions(t *testing.T) {
	now := time.Date(2026, 9, 29, 14, 3, 7, 0, time.UTC)
	v := openVault(t)

	collidingRel := "attachments/note-20260929-140307-aaaa.png"
	if err := v.Save(collidingRel, "existing-data"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	withConstantSuffix(t, "aaaa")

	if _, err := Import(v, "note.md", []byte("new-data"), "png", now); err == nil {
		t.Fatal("Import: want an error when every generated name collides")
	}

	data, err := os.ReadFile(v.Abs(collidingRel))
	if err != nil {
		t.Fatalf("read colliding file: %v", err)
	}
	if string(data) != "existing-data" {
		t.Fatalf("colliding file was overwritten: got %q, want %q", data, "existing-data")
	}

	entries, err := os.ReadDir(v.Abs(attachDir))
	if err != nil {
		t.Fatalf("ReadDir attachments: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("attachments/ has %d entries, want 1 (no leftover temp files): %v", len(entries), entries)
	}
}
