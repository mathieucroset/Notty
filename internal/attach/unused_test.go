package attach

import (
	"reflect"
	"testing"
)

func TestUnused(t *testing.T) {
	v := openVault(t)

	write := func(rel, content string) {
		t.Helper()
		if err := v.Save(rel, content); err != nil {
			t.Fatalf("Save %q: %v", rel, err)
		}
	}

	// Files sitting in attachments/.
	write("attachments/root-ref.png", "a")
	write("attachments/note-rel.png", "b")
	write("attachments/trashed-ref.png", "c")
	write("attachments/orphan.png", "d")

	// A root note referencing an attachment with a root-relative link.
	write("note.md", "![](/attachments/root-ref.png)\n")

	// A note referencing an attachment with a note-relative link that
	// resolves into attachments/.
	write("second.md", "![](attachments/note-rel.png)\n")

	// A note that gets trashed; its relative link resolves into
	// attachments/ only when resolved against its ORIGINAL path.
	write("Sub/third.md", "![](../attachments/trashed-ref.png)\n")
	if _, err := v.Trash("Sub/third.md"); err != nil {
		t.Fatalf("Trash: %v", err)
	}

	got, err := Unused(v)
	if err != nil {
		t.Fatalf("Unused: %v", err)
	}
	want := []string{"attachments/orphan.png"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Unused = %v, want %v", got, want)
	}
}

func TestUnusedNoAttachmentsFolder(t *testing.T) {
	v := openVault(t)
	if err := v.Save("note.md", "no images here\n"); err != nil {
		t.Fatal(err)
	}
	got, err := Unused(v)
	if err != nil {
		t.Fatalf("Unused: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Unused = %v, want empty", got)
	}
}

func TestUnusedTrashedFolder(t *testing.T) {
	v := openVault(t)

	write := func(rel, content string) {
		t.Helper()
		if err := v.Save(rel, content); err != nil {
			t.Fatalf("Save %q: %v", rel, err)
		}
	}

	write("attachments/keep.png", "a")
	write("attachments/drop.png", "b")
	write("Proj/note.md", "![](../attachments/keep.png)\n")

	if _, err := v.Trash("Proj"); err != nil {
		t.Fatalf("Trash: %v", err)
	}

	got, err := Unused(v)
	if err != nil {
		t.Fatalf("Unused: %v", err)
	}
	want := []string{"attachments/drop.png"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Unused = %v, want %v", got, want)
	}
}
