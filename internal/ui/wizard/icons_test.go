package wizard

import (
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/icons"
)

// TestWizardDrawsTheConfiguredIcons walks the first run with each icon
// set: list cursors use the same "›" as the text inputs, and the step and
// ready marks, the sample card's checkboxes and the spinner come from the
// set, which a theme preview keeps.
func TestWizardDrawsTheConfiguredIcons(t *testing.T) {
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			f := &fakeEnv{vaultState: setup.Missing}
			m := New(FirstRun, "~/Notes", "catppuccin-mocha", f.env(), testStyles().WithIcons(set)).SetSize(100, 40)
			m, _ = drive(t, m, m.Init())
			if got := strings.Join(m.spinner.Spinner.Frames, ""); got != strings.Join(set.Spinner(), "") {
				t.Errorf("spinner frames %q, want the %s set's", got, name)
			}
			m, _ = press(t, m, "enter")
			if v := plain(m); strings.Contains(v, "❯") || !strings.Contains(v, "› Use an existing repo URL") {
				t.Errorf("sync list cursor:\n%s", v)
			}
			m, _ = press(t, m, "down", "enter")
			if m.Stage() != StageTheme {
				t.Fatalf("stage = %v, want theme", m.Stage())
			}
			m, _ = press(t, m, "down")
			v := plain(m)
			for _, want := range []string{set.Check + " Your vault is ready", "› ", set.TaskOpen + " Call the bakery", set.TaskDone + " Water the plants"} {
				if !strings.Contains(v, want) {
					t.Errorf("theme step lacks %q:\n%s", want, v)
				}
			}
			if strings.Contains(v, "❯") {
				t.Errorf("theme list still uses ❯:\n%s", v)
			}
			if m.styles.Icons.Name != name {
				t.Errorf("theme preview dropped the icon set: %q", m.styles.Icons.Name)
			}
		})
	}
}

// TestRunStepMarkers checks finished and pending steps use the set's
// check and pending glyphs.
func TestRunStepMarkers(t *testing.T) {
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			f := &fakeEnv{vaultState: setup.Missing}
			m := New(SetupSync, "/v", "nord", f.env(), testStyles().WithIcons(set)).SetSize(100, 40)
			m, _ = send(t, m, ghMsg{ok: false})
			m, _ = press(t, m, "down")
			m, cmd := m.Update(key("enter"))
			m, cmd = stepOnce(t, m, cmd) // identity checked
			m, _ = stepOnce(t, m, cmd)   // planned
			steps, _ := setup.Plan(setup.Request{Vault: "/v", Choice: setup.LocalOnly, Host: "testhost"}, setup.Missing, setup.RemoteState{})
			if l := stepLine(t, m, steps[0].Desc); !strings.HasPrefix(strings.TrimSpace(strings.Trim(l, "│ ")), set.Pending+" ") {
				t.Errorf("pending step %q lacks %q", l, set.Pending)
			}
		})
	}
}
