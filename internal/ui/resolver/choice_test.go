package resolver

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync"
)

func pngBytes(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestChoiceScreens(t *testing.T) {
	binary := File{Path: "att/a.bin", Kind: Binary, Ours: []byte{0, 1}, Theirs: []byte{0, 2}}
	modDel := File{Path: "n.md", Kind: ModifyDelete, Ours: []byte("edited here\n"), Deleted: "theirs"}
	delMod := File{Path: "n.md", Kind: ModifyDelete, Theirs: []byte("edited there\n"), Deleted: "ours"}
	trashOurs := File{Path: "Work/n.md", Kind: PathConflict, PathKind: gitsync.TrashVsMove,
		Original: "n.md", OursPath: ".trash/1/n.md", TheirsPath: "Work/n.md"}
	trashTheirs := File{Path: "Work/n.md", Kind: PathConflict, PathKind: gitsync.TrashVsMove,
		Original: "n.md", OursPath: "Work/n.md", TheirsPath: ".trash/1/n.md"}
	renamed := File{Path: "n.md", Kind: PathConflict, PathKind: gitsync.RenameRename,
		Original: "n.md", OursPath: "A/n.md", TheirsPath: "B/n.md"}
	bothTrashed := File{Path: "n.md", Kind: PathConflict, PathKind: gitsync.BothTrashed,
		Original: "n.md", OursPath: ".trash/1/n.md", TheirsPath: ".trash/2/n.md"}

	tests := []struct {
		name    string
		file    File
		keys    []string
		want    ChoiceKind
		visible []string
	}{
		{"binary default keeps yours", binary, nil, KeepOurs, []string{"1  Keep yours", "2  Keep theirs"}},
		{"binary theirs", binary, []string{"2"}, KeepTheirs, nil},
		{"binary out of range digit ignored", binary, []string{"2", "3"}, KeepTheirs, nil},
		{"binary back to yours", binary, []string{"2", "1"}, KeepOurs, nil},
		{"modify/delete keep edited", modDel, nil, KeepEdited, []string{"Edited on this computer; deleted on the other.", "edited here", "Keep edited", "Delete"}},
		{"modify/delete delete", modDel, []string{"2"}, Delete, nil},
		{"delete/modify shows their side", delMod, nil, KeepEdited, []string{"Deleted on this computer; edited on the other.", "edited there"}},
		{"trash vs move default keeps new path", trashOurs, nil, KeepAtNewPath, []string{"Moved to trash on this computer; moved to Work/n.md on the other.", "Keep at Work/n.md", "Keep in trash"}},
		{"trash vs move keep in trash", trashTheirs, []string{"2"}, KeepInTrash, []string{"Moved to Work/n.md on this computer; moved to trash on the other."}},
		{"rename/rename keep A", renamed, nil, KeepPathA, []string{"Keep A/n.md", "Keep B/n.md", "Keep both", "Originally n.md"}},
		{"rename/rename keep B", renamed, []string{"2"}, KeepPathB, nil},
		{"rename/rename keep both", renamed, []string{"3"}, KeepBoth, nil},
		{"both trashed", bothTrashed, nil, KeepBoth, []string{"Keep both trash items"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(t, 120, 40, tt.file)
			m, _ = press(t, m, tt.keys...)
			v := plain(m)
			for _, s := range tt.visible {
				if !strings.Contains(v, s) {
					t.Errorf("view lacks %q:\n%s", s, v)
				}
			}
			checkSize(t, m, 120, 40)
			m, out := press(t, m, "enter")
			got := only[ResolveChoiceMsg](out)
			if len(got) != 1 || got[0].Path != tt.file.Path || got[0].Choice != tt.want {
				t.Fatalf("enter produced %v, want ResolveChoiceMsg{%s %v}", out, tt.file.Path, tt.want)
			}
			if !strings.Contains(plain(m), "Saving…") {
				t.Error("pending state not shown")
			}
			// A second enter while pending does nothing.
			if _, out := press(t, m, "enter"); len(out) != 0 {
				t.Errorf("enter while pending produced %v", out)
			}
			m = m.SetError(tt.file.Path, "boom")
			if _, out := press(t, m, "enter"); len(only[ResolveChoiceMsg](out)) != 1 {
				t.Error("cannot retry after an error")
			}
		})
	}
}

func TestBinaryImagePreviews(t *testing.T) {
	f := File{Path: "att/pic.png", Kind: Binary,
		Ours:   pngBytes(t, 4, 2, color.RGBA{255, 0, 0, 255}),
		Theirs: pngBytes(t, 6, 3, color.RGBA{0, 0, 255, 255})}
	m := newModel(t, 120, 40, f)
	c := m.items[0].choice
	if c.images != imagesPending || c.previews[0] != nil {
		t.Fatal("images decoded eagerly")
	}
	m, cmd := m.LoadPreviews()
	if cmd == nil || m.items[0].choice.images != imagesLoading {
		t.Fatal("LoadPreviews did not start loading")
	}
	if !strings.Contains(plain(m), "loading preview") {
		t.Error("no loading placeholder")
	}
	if _, again := m.LoadPreviews(); again != nil {
		t.Error("LoadPreviews started a second load")
	}
	msg := cmd()
	if !Owns(msg) {
		t.Fatalf("preview message %T not owned", msg)
	}
	m, _ = m.Update(msg)
	c = m.items[0].choice
	if c.images != imagesLoaded || c.previews[0] == nil || c.previews[1] == nil {
		t.Fatal("previews not stored")
	}
	v := m.View()
	if !strings.Contains(v, "▀") || !strings.Contains(v, "38;2;255;0;0") || !strings.Contains(v, "38;2;0;0;255") {
		t.Error("half-block previews missing")
	}
	p := plain(m)
	if !strings.Contains(p, "4×2") || !strings.Contains(p, "6×3") {
		t.Errorf("image sizes missing:\n%s", p)
	}
	checkSize(t, m, 120, 40)
	checkSize(t, m.SetSize(80, 24), 80, 24)
	// Not an image by name: nothing to load.
	f.Path = "att/pic.bin"
	if _, cmd := newModel(t, 120, 40, f).LoadPreviews(); cmd != nil {
		t.Error("loading previews for a file whose name is not an image")
	}
	if decodePreview([]byte("not an image")) != nil {
		t.Error("garbage decoded")
	}
}

func TestPreviewsLoadOnSelection(t *testing.T) {
	img := File{Path: "pic.png", Kind: Binary, Ours: pngBytes(t, 2, 2, color.White), Theirs: pngBytes(t, 2, 2, color.Black)}
	m := newModel(t, 120, 40, textFile("a.md", base1, ours1, theirs1), img)
	m, cmd := m.Update(key("j"))
	if cmd == nil {
		t.Fatal("selecting an image file did not load its previews")
	}
	m, _ = drain(m, cmd)
	if m.items[1].choice.images != imagesLoaded || m.items[1].choice.previews[0] == nil {
		t.Fatal("previews not loaded after selection")
	}
	if _, cmd := m.Update(key("k")); cmd != nil {
		t.Error("moving away produced a command")
	}
}

func TestPreviewDownscaledAndCached(t *testing.T) {
	p := decodePreview(pngBytes(t, 2000, 10, color.White))
	if p == nil {
		t.Fatal("not decoded")
	}
	if p.w != 2000 || p.h != 10 {
		t.Errorf("original size %d×%d, want 2000×10", p.w, p.h)
	}
	if b := p.img.Bounds(); b.Dx() != previewMax || b.Dy() != 5 {
		t.Errorf("kept image is %v, want %d×5", b, previewMax)
	}
	first := p.render(20, 5, 8, 16)
	if again := p.render(20, 5, 8, 16); &again[0] != &first[0] {
		t.Error("same size re-rendered")
	}
	other := p.render(10, 5, 8, 16)
	if p.lastKey == [2]int{} || &other[0] == &first[0] {
		t.Error("a new size did not replace the cached rendering")
	}
}

func TestModifyDeleteScroll(t *testing.T) {
	var b strings.Builder
	for i := range 200 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%5))
		b.WriteString("\n")
	}
	m := newModel(t, 120, 30, File{Path: "n.md", Kind: ModifyDelete, Ours: []byte(b.String()), Deleted: "theirs"})
	m, _ = press(t, m, "ctrl+d")
	if m.items[0].choice.scroll == 0 {
		t.Fatal("ctrl+d did not scroll")
	}
	m, _ = press(t, m, "ctrl+u", "ctrl+u")
	if m.items[0].choice.scroll != 0 {
		t.Fatal("ctrl+u did not scroll back")
	}
	checkSize(t, m, 120, 30)
}
