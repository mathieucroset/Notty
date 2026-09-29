// Package help implements Notty's help overlay (spec §4.4): a full-screen,
// scrollable reference generated straight from the key tables in
// internal/ui/keys, so it can never drift from the real bindings.
package help

import "github.com/mathieucroset/notty/internal/ui/keys"

// Row is one keybinding entry in a help section.
type Row struct {
	Keys string
	Desc string
}

// Section is one titled group of keybindings in the help overlay.
type Section struct {
	Title string
	Rows  []Row
}

// sectionSpec pairs a human-readable title with the contexts whose bindings
// make up the section. Some sections combine several related contexts (the
// vim sub-modes, the resolver's two contexts, the wizard's two contexts) so
// the overlay reads as one coherent group per area of the app.
type sectionSpec struct {
	title string
	ctxs  []keys.Context
}

// sectionSpecs lists every keys.Context exactly once (spec §4.4), in the
// fixed, readable order the help overlay presents them.
var sectionSpecs = []sectionSpec{
	{"Global", []keys.Context{keys.Global}},
	{"Sidebar", []keys.Context{keys.Sidebar}},
	{"Editor · vim normal", []keys.Context{keys.EditorNormal, keys.EditorVisual, keys.EditorCommand}},
	{"Editor · insert", []keys.Context{keys.EditorInsert}},
	{"Editor · vim off", []keys.Context{keys.EditorPlain}},
	{"Editor · read-only", []keys.Context{keys.EditorReadOnly}},
	{"Preview", []keys.Context{keys.Preview}},
	{"Tasks", []keys.Context{keys.TasksView}},
	{"Trash", []keys.Context{keys.TrashView}},
	{"Resolver", []keys.Context{keys.Resolver, keys.ResolverEdit}},
	{"History", []keys.Context{keys.History}},
	{"Image viewer", []keys.Context{keys.ImageViewer}},
	{"Wizard", []keys.Context{keys.Wizard, keys.WizardInput}},
	{"Overlays", []keys.Context{keys.Overlay}},
}

// Sections returns the help overlay's sections, generated from
// keys.GlobalKeys and keys.Bindings for every context (spec §4.4), so the
// help overlay never drifts from the actual key tables.
func Sections() []Section {
	out := make([]Section, 0, len(sectionSpecs))
	for _, spec := range sectionSpecs {
		var rows []Row
		for _, ctx := range spec.ctxs {
			for _, b := range keys.Bindings(ctx) {
				h := b.Help()
				rows = append(rows, Row{Keys: h.Key, Desc: h.Desc})
			}
		}
		out = append(out, Section{Title: spec.title, Rows: rows})
	}
	return out
}
