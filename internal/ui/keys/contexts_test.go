package keys

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var namedKeys = map[string]rune{
	"enter": tea.KeyEnter, "tab": tea.KeyTab, "esc": tea.KeyEscape,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"space": tea.KeySpace, "backspace": tea.KeyBackspace, "home": tea.KeyHome,
	"end": tea.KeyEnd, "f1": tea.KeyF1,
}

// parseKey builds the KeyPressMsg for a single key press such as "q",
// "ctrl+k", "shift+tab" or "f1". ok is false for multi-key sequences
// ("gg", "]t", "ctrl+w h", ":w").
func parseKey(s string) (tea.KeyPressMsg, bool) {
	var mod tea.KeyMod
	rest := s
	for {
		switch {
		case strings.HasPrefix(rest, "ctrl+") && len(rest) > 5:
			mod |= tea.ModCtrl
			rest = rest[5:]
			continue
		case strings.HasPrefix(rest, "shift+") && len(rest) > 6:
			mod |= tea.ModShift
			rest = rest[6:]
			continue
		}
		break
	}
	if code, ok := namedKeys[rest]; ok {
		return tea.KeyPressMsg{Code: code, Mod: mod}, true
	}
	if r := []rune(rest); len(r) == 1 {
		if mod != 0 {
			return tea.KeyPressMsg{Code: r[0], Mod: mod}, true
		}
		return tea.KeyPressMsg{Code: r[0], Text: rest}, true
	}
	return tea.KeyPressMsg{}, false
}

// press builds a KeyPressMsg whose String() is the given key name.
func press(t *testing.T, s string) tea.KeyPressMsg {
	t.Helper()
	k, ok := parseKey(s)
	if !ok {
		t.Fatalf("press(%q): not a single key", s)
	}
	if got := k.String(); got != s {
		t.Fatalf("press(%q) builds a key whose String() is %q", s, got)
	}
	return k
}

var allContexts = []Context{
	Global, Sidebar, EditorNormal, EditorInsert, EditorVisual, EditorCommand,
	EditorPlain, EditorReadOnly, Preview, TasksView, TrashView, Resolver,
	ResolverEdit, History, ImageViewer, Wizard, WizardInput, Overlay,
}

// pane contexts are the non-overlay, non-full-screen contexts in which every
// global key wins.
var paneContexts = []Context{
	Global, Sidebar, EditorNormal, EditorInsert, EditorVisual, EditorCommand,
	EditorPlain, EditorReadOnly, Preview, TasksView, TrashView,
}

func TestRouteCtrlQQuitsEverywhere(t *testing.T) {
	for _, ctx := range allContexts {
		for _, overlay := range []bool{false, true} {
			a, global := Route(ctx, overlay, press(t, "ctrl+q"))
			if !global || a != Quit {
				t.Errorf("Route(%v, overlay=%v, ctrl+q) = (%v, %v), want (Quit, true)", ctx, overlay, a, global)
			}
		}
	}
}

func TestRouteGlobalKeysWinInPanes(t *testing.T) {
	want := map[string]Action{
		"ctrl+p": Finder,
		"ctrl+f": Search,
		"ctrl+k": Palette,
		"ctrl+b": ToggleSidebar,
		"ctrl+g": CycleView,
		"ctrl+s": Save,
		"ctrl+e": ExternalEditor,
		"ctrl+q": Quit,
		"f1":     Help,
	}
	for _, ctx := range paneContexts {
		for k, act := range want {
			a, global := Route(ctx, false, press(t, k))
			if !global || a != act {
				t.Errorf("Route(%v, false, %s) = (%v, %v), want (%v, true)", ctx, k, a, global, act)
			}
		}
	}
}

func TestRouteLayering(t *testing.T) {
	tests := []struct {
		name       string
		ctx        Context
		overlay    bool
		key        string
		wantAction Action
		wantGlobal bool
	}{
		// ctrl+g is global only outside overlays and full-screen views.
		{"ctrl+g editor", EditorNormal, false, "ctrl+g", CycleView, true},
		{"ctrl+g insert", EditorInsert, false, "ctrl+g", CycleView, true},
		{"ctrl+g sidebar overlay open", Sidebar, true, "ctrl+g", None, false},
		{"ctrl+g overlay ctx", Overlay, false, "ctrl+g", None, false},
		{"ctrl+g resolver", Resolver, false, "ctrl+g", None, false},
		{"ctrl+g history", History, false, "ctrl+g", None, false},
		{"ctrl+g image viewer", ImageViewer, false, "ctrl+g", None, false},
		{"ctrl+g wizard", Wizard, false, "ctrl+g", None, false},

		// F1 passes through overlays and full-screen views, but not the wizard.
		{"f1 overlay", Overlay, false, "f1", Help, true},
		{"f1 editor overlay open", EditorNormal, true, "f1", Help, true},
		{"f1 resolver", Resolver, false, "f1", Help, true},
		{"f1 resolver edit", ResolverEdit, false, "f1", Help, true},
		{"f1 history", History, false, "f1", Help, true},
		{"f1 image viewer", ImageViewer, false, "f1", Help, true},
		{"f1 wizard", Wizard, false, "f1", None, false},
		{"f1 wizard input", WizardInput, false, "f1", None, false},
		{"f1 wizard with overlay", Wizard, true, "f1", None, false},

		// Printable keys go to the context.
		{"q wizard input", WizardInput, false, "q", None, false},
		{"q wizard", Wizard, false, "q", None, false},
		{"q sidebar", Sidebar, false, "q", None, false},
		{"? insert", EditorInsert, false, "?", None, false},
		{"? normal", EditorNormal, false, "?", None, false},
		{"j sidebar", Sidebar, false, "j", None, false},

		// ctrl+s saves everywhere except inside overlays; in resolver edit
		// it accepts the edit (context).
		{"ctrl+s insert", EditorInsert, false, "ctrl+s", Save, true},
		{"ctrl+s resolver edit", ResolverEdit, false, "ctrl+s", None, false},
		{"ctrl+s resolver", Resolver, false, "ctrl+s", None, false},
		{"ctrl+s overlay", EditorNormal, true, "ctrl+s", None, false},

		// ctrl+k is the palette in panes but moves up inside overlays.
		{"ctrl+k editor", EditorNormal, false, "ctrl+k", Palette, true},
		{"ctrl+k overlay ctx", Overlay, false, "ctrl+k", None, false},
		{"ctrl+k overlay open", Sidebar, true, "ctrl+k", None, false},

		// Other keys stay in context.
		{"tab normal", EditorNormal, false, "tab", None, false},
		{"shift+tab insert", EditorInsert, false, "shift+tab", None, false},
		{"esc overlay", Overlay, false, "esc", None, false},
		{"enter wizard input", WizardInput, false, "enter", None, false},
		{"ctrl+c normal", EditorNormal, false, "ctrl+c", None, false},
		{"ctrl+p wizard input", WizardInput, false, "ctrl+p", None, false},
		{"ctrl+p resolver edit", ResolverEdit, false, "ctrl+p", None, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, global := Route(tt.ctx, tt.overlay, press(t, tt.key))
			if a != tt.wantAction || global != tt.wantGlobal {
				t.Errorf("Route(%v, %v, %s) = (%v, %v), want (%v, %v)",
					tt.ctx, tt.overlay, tt.key, a, global, tt.wantAction, tt.wantGlobal)
			}
		})
	}
}

// TestRouteMatrix checks all 9 global keys in all 18 contexts, with and
// without an overlay open. The expectations are spelled out per context
// rather than derived from the implementation's rules.
func TestRouteMatrix(t *testing.T) {
	globals := []struct {
		key    string
		action Action
	}{
		{"ctrl+p", Finder}, {"ctrl+f", Search}, {"ctrl+k", Palette},
		{"ctrl+b", ToggleSidebar}, {"ctrl+g", CycleView}, {"ctrl+s", Save},
		{"ctrl+e", ExternalEditor}, {"ctrl+q", Quit}, {"f1", Help},
	}
	all := map[Action]bool{Finder: true, Search: true, Palette: true, ToggleSidebar: true,
		CycleView: true, Save: true, ExternalEditor: true, Quit: true, Help: true}
	quitHelp := map[Action]bool{Quit: true, Help: true}
	quitOnly := map[Action]bool{Quit: true}

	// passes[ctx][overlayOpen] is the set of actions that route globally.
	passes := map[Context][2]map[Action]bool{
		Global:         {all, quitHelp},
		Sidebar:        {all, quitHelp},
		EditorNormal:   {all, quitHelp},
		EditorInsert:   {all, quitHelp},
		EditorVisual:   {all, quitHelp},
		EditorCommand:  {all, quitHelp},
		EditorPlain:    {all, quitHelp},
		EditorReadOnly: {all, quitHelp},
		Preview:        {all, quitHelp},
		TasksView:      {all, quitHelp},
		TrashView:      {all, quitHelp},
		Resolver:       {quitHelp, quitHelp},
		ResolverEdit:   {quitHelp, quitHelp}, // ctrl+s accepts the edit; F1 passes
		History:        {quitHelp, quitHelp},
		ImageViewer:    {quitHelp, quitHelp},
		Wizard:         {quitOnly, quitOnly},
		WizardInput:    {quitOnly, quitOnly},
		Overlay:        {quitHelp, quitHelp},
	}
	if len(passes) != len(allContexts) {
		t.Fatalf("matrix covers %d contexts, want %d", len(passes), len(allContexts))
	}
	for ctx, byOverlay := range passes {
		for i, overlay := range []bool{false, true} {
			for _, g := range globals {
				a, global := Route(ctx, overlay, press(t, g.key))
				wantGlobal := byOverlay[i][g.action]
				wantAction := None
				if wantGlobal {
					wantAction = g.action
				}
				if global != wantGlobal || a != wantAction {
					t.Errorf("Route(%v, overlay=%v, %s) = (%v, %v), want (%v, %v)",
						ctx, overlay, g.key, a, global, wantAction, wantGlobal)
				}
			}
		}
	}
}

// TestBindingsReachable walks every single-key binding of every context and
// checks the routing layer does not swallow it: context keys reach the
// context, and the global table's keys route globally in Global.
func TestBindingsReachable(t *testing.T) {
	for _, ctx := range allContexts {
		for _, b := range Bindings(ctx) {
			for _, ks := range b.Keys() {
				k, ok := parseKey(ks)
				if !ok {
					continue // multi-key sequence, handled by the context itself
				}
				if got := k.String(); got != ks {
					t.Errorf("%v binding %q parses to key %q", ctx, ks, got)
					continue
				}
				a, global := Route(ctx, false, k)
				switch {
				case ctx == Global:
					if !global {
						t.Errorf("global binding %q does not route globally", ks)
					}
				case global && a != Quit:
					// Only ctrl+q (listed in the wizard tables) may be both a
					// context binding and a global action.
					t.Errorf("%v binding %q is swallowed by global action %v", ctx, ks, a)
				}
			}
		}
	}
}

func TestBindingsCoverEveryContext(t *testing.T) {
	for _, ctx := range allContexts {
		bs := Bindings(ctx)
		if len(bs) == 0 {
			t.Errorf("Bindings(%v) is empty", ctx)
		}
		for _, b := range bs {
			h := b.Help()
			if h.Key == "" || h.Desc == "" {
				t.Errorf("Bindings(%v) has a binding without help: %+v", ctx, h)
			}
		}
		if ctx.String() == "" || ctx.String() == "Unknown" {
			t.Errorf("Context %d has no name", int(ctx))
		}
	}
}

func TestSidebarBindingsMatchSpec(t *testing.T) {
	want := []string{"j", "k", "h", "l", "enter", "tab", "n", "N", "r", "m", "d", "p", "H", "#", "esc", "c", "!", "?", "q"}
	have := map[string]bool{}
	for _, b := range Bindings(Sidebar) {
		for _, k := range b.Keys() {
			have[k] = true
		}
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("sidebar bindings missing %q", k)
		}
	}
}

func TestGlobalKeysHaveHelp(t *testing.T) {
	for _, a := range []Action{Finder, Search, Palette, ToggleSidebar, CycleView, Save, ExternalEditor, Quit, Help} {
		b, ok := GlobalKeys[a]
		if !ok {
			t.Errorf("GlobalKeys missing %v", a)
			continue
		}
		if len(b.Keys()) == 0 || b.Help().Desc == "" {
			t.Errorf("GlobalKeys[%v] incomplete", a)
		}
	}
}
