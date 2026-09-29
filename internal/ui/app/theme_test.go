package app

import (
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestStartupWarningsShownAsToasts(t *testing.T) {
	tests := []struct {
		name string
		opts func(t *testing.T) Options
	}{
		{"no vault", func(*testing.T) Options { return Options{} }},
		{"vault", testOptions},
		{"wizard", func(t *testing.T) Options {
			gittest.Isolate(t)
			return wizardOptions(t)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts(t)
			opts.StartupWarnings = []string{"boom", "bang"}
			m := start(t, opts, 100, 30)
			t.Cleanup(m.Shutdown)
			for _, w := range opts.StartupWarnings {
				if !hasToast(m, msgs.ToastWarn, w) {
					t.Errorf("no warning toast %q; toasts %q", w, toastTexts(m))
				}
			}
			if !strings.Contains(screen(m), "boom") {
				t.Errorf("warning not on screen:\n%s", screen(m))
			}
			// Init again (it must not repeat them): each shown once.
			drive(t, m, execOne(t, m, m.Init()))
			for _, w := range opts.StartupWarnings {
				n := 0
				for _, s := range toastTexts(m) {
					if s == w {
						n++
					}
				}
				if n != 1 {
					t.Errorf("warning %q shown %d times, want 1", w, n)
				}
			}
		})
	}
}

func TestThemeNameAtStart(t *testing.T) {
	tests := []struct {
		name, config, want string
	}{
		{"configured theme", "mine", "mine"},
		{"no configured theme", "", "catppuccin-mocha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			opts.Config.Theme = tt.config
			if m := New(opts); m.themeName != tt.want {
				t.Errorf("themeName = %q, want %q", m.themeName, tt.want)
			}
		})
	}
}
