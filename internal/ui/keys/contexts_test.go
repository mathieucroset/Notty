package keys

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// press builds a KeyPressMsg whose String() is the given key name.
func press(t *testing.T, s string) tea.KeyPressMsg {
	t.Helper()
	var k tea.KeyPressMsg
	switch s {
	case "f1":
		k = tea.KeyPressMsg{Code: tea.KeyF1}
	case "tab":
		k = tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		k = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "esc":
		k = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		k = tea.KeyPressMsg{Code: tea.KeyEnter}
	default:
		if len(s) > 5 && s[:5] == "ctrl+" {
			k = tea.KeyPressMsg{Code: rune(s[5]), Mod: tea.ModCtrl}
		} else {
			k = tea.KeyPressMsg{Code: rune(s[0]), Text: s}
		}
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

// TestRouteMatrix checks every context against representative keys. g marks
// a global action; "-" means the key goes to the context.
func TestRouteMatrix(t *testing.T) {
	keysUnderTest := []string{"ctrl+q", "f1", "ctrl+g", "ctrl+s", "ctrl+k", "ctrl+b", "q", "?", "tab", "esc"}
	// Columns follow keysUnderTest.
	matrix := map[Context][]string{
		Global:         {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		Sidebar:        {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		EditorNormal:   {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		EditorInsert:   {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		EditorVisual:   {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		EditorCommand:  {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		EditorPlain:    {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		EditorReadOnly: {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		Preview:        {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		TasksView:      {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		TrashView:      {"g", "g", "g", "g", "g", "g", "-", "-", "-", "-"},
		Resolver:       {"g", "g", "-", "-", "-", "-", "-", "-", "-", "-"},
		ResolverEdit:   {"g", "g", "-", "-", "-", "-", "-", "-", "-", "-"},
		History:        {"g", "g", "-", "-", "-", "-", "-", "-", "-", "-"},
		ImageViewer:    {"g", "g", "-", "-", "-", "-", "-", "-", "-", "-"},
		Wizard:         {"g", "-", "-", "-", "-", "-", "-", "-", "-", "-"},
		WizardInput:    {"g", "-", "-", "-", "-", "-", "-", "-", "-", "-"},
		Overlay:        {"g", "g", "-", "-", "-", "-", "-", "-", "-", "-"},
	}
	if len(matrix) != len(allContexts) {
		t.Fatalf("matrix covers %d contexts, want %d", len(matrix), len(allContexts))
	}
	for ctx, row := range matrix {
		for i, k := range keysUnderTest {
			_, global := Route(ctx, false, press(t, k))
			if want := row[i] == "g"; global != want {
				t.Errorf("Route(%v, false, %s): global = %v, want %v", ctx, k, global, want)
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
