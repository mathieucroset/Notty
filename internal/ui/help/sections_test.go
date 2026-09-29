package help

import (
	"testing"

	"github.com/mathieucroset/notty/internal/ui/keys"
)

// bindingKey identifies a key.Binding by its displayed help text, which is
// unique enough for this test's purposes.
type bindingKey struct{ keys, desc string }

func TestSectionsCoverEveryContextBinding(t *testing.T) {
	sections := Sections()

	present := make(map[bindingKey]bool)
	for _, sec := range sections {
		for _, r := range sec.Rows {
			present[bindingKey{r.Keys, r.Desc}] = true
		}
	}

	for _, ctx := range keys.Contexts() {
		for _, b := range keys.Bindings(ctx) {
			h := b.Help()
			bk := bindingKey{h.Key, h.Desc}
			if !present[bk] {
				t.Errorf("Sections() is missing binding %q (%q) from context %s", h.Key, h.Desc, ctx)
			}
		}
	}
}

func TestSectionSpecsPartitionEveryContextExactlyOnce(t *testing.T) {
	seen := make(map[keys.Context]int)
	for _, spec := range sectionSpecs {
		for _, ctx := range spec.ctxs {
			seen[ctx]++
		}
	}
	for _, ctx := range keys.Contexts() {
		if seen[ctx] != 1 {
			t.Errorf("context %s appears %d times across sectionSpecs, want exactly 1", ctx, seen[ctx])
		}
	}
	if len(seen) != len(keys.Contexts()) {
		t.Errorf("sectionSpecs cover %d contexts, want %d", len(seen), len(keys.Contexts()))
	}
}

func TestSectionsTitlesInFixedOrder(t *testing.T) {
	want := []string{
		"Global", "Sidebar", "Editor · vim normal", "Editor · insert",
		"Editor · vim off", "Editor · read-only", "Preview", "Tasks",
		"Trash", "Resolver", "History", "Image viewer", "Wizard", "Overlays",
	}
	sections := Sections()
	if len(sections) != len(want) {
		t.Fatalf("got %d sections, want %d", len(sections), len(want))
	}
	for i, w := range want {
		if sections[i].Title != w {
			t.Errorf("section[%d].Title = %q, want %q", i, sections[i].Title, w)
		}
	}
}

func TestSectionsRowsNonEmpty(t *testing.T) {
	for _, sec := range Sections() {
		if len(sec.Rows) == 0 {
			t.Errorf("section %q has no rows", sec.Title)
		}
		for _, r := range sec.Rows {
			if r.Keys == "" || r.Desc == "" {
				t.Errorf("section %q has a row with an empty Keys or Desc: %+v", sec.Title, r)
			}
		}
	}
}
