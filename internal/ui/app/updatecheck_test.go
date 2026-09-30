package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/wizard"
	"github.com/mathieucroset/notty/internal/update"
)

// fakeReleases is a fake GitHub "latest release" endpoint counting hits.
type fakeReleases struct {
	srv  *httptest.Server
	hits atomic.Int32
}

func newFakeReleases(t *testing.T, status int, body string) *fakeReleases {
	t.Helper()
	f := &fakeReleases{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

var updateNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// withUpdateCheck points opts at the fake API and statePath, as release
// 0.1.0 installed outside Homebrew and go install.
func withUpdateCheck(opts Options, f *fakeReleases, statePath string) Options {
	opts.Version = "0.1.0"
	opts.Now = func() time.Time { return updateNow }
	opts.UpdateCheck = UpdateCheckOptions{
		StatePath: statePath,
		APIURL:    f.srv.URL,
		Client:    f.srv.Client(),
		Exe:       "/usr/local/bin/notty",
		GoBin:     "/home/me/go/bin",
	}
	return opts
}

// startCollecting is start, returning the messages Init produced.
func startCollecting(t *testing.T, opts Options, w, h int) (*Model, []tea.Msg) {
	t.Helper()
	m := New(opts)
	t.Cleanup(m.closeThemeWatcher)
	run(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	return m, drive(t, m, execOne(t, m, m.Init()))
}

// updateToasts returns the texts of the toasts about a new release.
func updateToasts(produced []tea.Msg) []string {
	var out []string
	for _, msg := range produced {
		if tm, ok := msg.(msgs.ToastMsg); ok && strings.Contains(tm.Text, "is available") {
			out = append(out, tm.Text)
		}
	}
	return out
}

func errorToasts(produced []tea.Msg) []string {
	var out []string
	for _, msg := range produced {
		if tm, ok := msg.(msgs.ToastMsg); ok && tm.Level != msgs.ToastInfo {
			out = append(out, tm.Text)
		}
	}
	return out
}

func TestUpdateCheckToastOnceThenMarker(t *testing.T) {
	f := newFakeReleases(t, 200, `{"tag_name":"v0.2.0"}`)
	statePath := filepath.Join(t.TempDir(), "state", "update-check.json")

	m, produced := startCollecting(t, withUpdateCheck(testOptions(t), f, statePath), 120, 30)
	if n := f.hits.Load(); n != 1 {
		t.Fatalf("first start made %d requests, want 1", n)
	}
	want := "Notty v0.2.0 is available — download it from github.com/mathieucroset/notty/releases"
	toasts := updateToasts(produced)
	if len(toasts) != 1 || toasts[0] != want {
		t.Errorf("toasts = %q, want [%q]", toasts, want)
	}
	for _, msg := range produced {
		if tm, ok := msg.(msgs.ToastMsg); ok && tm.Text == want && tm.Level != msgs.ToastInfo {
			t.Errorf("toast level = %v, want info", tm.Level)
		}
	}
	if s := screen(m); !strings.Contains(s, "↑ v0.2.0") {
		t.Errorf("status bar lacks the marker:\n%s", s)
	}
	st := update.LoadState(statePath)
	if st.Notified != "v0.2.0" || st.Latest != "v0.2.0" || !st.CheckedAt.Equal(updateNow) {
		t.Errorf("saved state = %+v", st)
	}

	// A second start the same day: the cache, the marker, no toast.
	m2, produced2 := startCollecting(t, withUpdateCheck(testOptions(t), f, statePath), 120, 30)
	if n := f.hits.Load(); n != 1 {
		t.Errorf("second start made a request (%d total)", n)
	}
	if toasts := updateToasts(produced2); len(toasts) != 0 {
		t.Errorf("second start toasts = %q", toasts)
	}
	if s := screen(m2); !strings.Contains(s, "↑ v0.2.0") {
		t.Errorf("second start lacks the marker:\n%s", s)
	}
}

func TestUpdateCheckCurrentIsNewest(t *testing.T) {
	f := newFakeReleases(t, 200, `{"tag_name":"v0.1.0"}`)
	opts := withUpdateCheck(testOptions(t), f, filepath.Join(t.TempDir(), "update-check.json"))
	m, produced := startCollecting(t, opts, 120, 30)
	if f.hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", f.hits.Load())
	}
	if toasts := updateToasts(produced); len(toasts) != 0 {
		t.Errorf("toasts = %q", toasts)
	}
	if s := screen(m); strings.Contains(s, "↑") {
		t.Errorf("marker shown for the current release:\n%s", s)
	}
}

func TestUpdateCheckSkipped(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Options)
	}{
		{"dev build", func(o *Options) { o.Version = "dev" }},
		{"no version", func(o *Options) { o.Version = "" }},
		{"describe build", func(o *Options) { o.Version = "v0.1.0-3-gabc1234" }},
		{"dirty build", func(o *Options) { o.Version = "v0.1.0-dirty" }},
		{"disabled in config", func(o *Options) { o.Config.UpdateCheck = false }},
		{"disabled by env", func(o *Options) { o.UpdateCheck.EnvDisabled = true }},
		{"no state path", func(o *Options) { o.UpdateCheck.StatePath = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeReleases(t, 200, `{"tag_name":"v0.2.0"}`)
			statePath := filepath.Join(t.TempDir(), "update-check.json")
			opts := withUpdateCheck(testOptions(t), f, statePath)
			tt.setup(&opts)
			m, produced := startCollecting(t, opts, 120, 30)
			if n := f.hits.Load(); n != 0 {
				t.Errorf("%d requests, want none", n)
			}
			if toasts := updateToasts(produced); len(toasts) != 0 {
				t.Errorf("toasts = %q", toasts)
			}
			if s := screen(m); strings.Contains(s, "↑") {
				t.Errorf("marker shown:\n%s", s)
			}
			if _, err := os.Stat(statePath); err == nil {
				t.Error("state file written")
			}
		})
	}
}

func TestUpdateCheckFailureIsSilent(t *testing.T) {
	for _, status := range []int{404, 403, 429, 500} {
		f := newFakeReleases(t, status, `{"message":"nope"}`)
		statePath := filepath.Join(t.TempDir(), "update-check.json")
		m, produced := startCollecting(t, withUpdateCheck(testOptions(t), f, statePath), 120, 30)
		if f.hits.Load() != 1 {
			t.Errorf("%d: hits = %d, want 1", status, f.hits.Load())
		}
		if toasts := updateToasts(produced); len(toasts) != 0 {
			t.Errorf("%d: toasts = %q", status, toasts)
		}
		if errs := errorToasts(produced); len(errs) != 0 {
			t.Errorf("%d: error toasts = %q", status, errs)
		}
		if s := screen(m); strings.Contains(s, "↑") {
			t.Errorf("%d: marker shown:\n%s", status, s)
		}
		// The failure is remembered: no retry until tomorrow.
		if st := update.LoadState(statePath); !st.CheckedAt.Equal(updateNow) {
			t.Errorf("%d: CheckedAt = %v, want %v", status, st.CheckedAt, updateNow)
		}
		startCollecting(t, withUpdateCheck(testOptions(t), f, statePath), 120, 30)
		if f.hits.Load() != 1 {
			t.Errorf("%d: retried within the day (%d hits)", status, f.hits.Load())
		}
	}
}

func TestUpdateCheckFailureKeepsTheCachedMarker(t *testing.T) {
	f := newFakeReleases(t, 500, ``)
	statePath := filepath.Join(t.TempDir(), "update-check.json")
	old := update.State{CheckedAt: updateNow.Add(-48 * time.Hour), Latest: "v0.2.0", Notified: "v0.2.0"}
	if err := old.Save(statePath); err != nil {
		t.Fatal(err)
	}
	m, produced := startCollecting(t, withUpdateCheck(testOptions(t), f, statePath), 120, 30)
	if f.hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", f.hits.Load())
	}
	if toasts := updateToasts(produced); len(toasts) != 0 {
		t.Errorf("toasts = %q", toasts)
	}
	if s := screen(m); !strings.Contains(s, "↑ v0.2.0") {
		t.Errorf("cached marker lost:\n%s", s)
	}
}

func TestUpdateCheckRunsOnce(t *testing.T) {
	f := newFakeReleases(t, 200, `{"tag_name":"v0.2.0"}`)
	statePath := filepath.Join(t.TempDir(), "update-check.json")
	m, _ := startCollecting(t, withUpdateCheck(testOptions(t), f, statePath), 120, 30)
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	// Init runs again when the wizard opens a vault; the check does not.
	produced := drive(t, m, execOne(t, m, m.Init()))
	if n := f.hits.Load(); n != 1 {
		t.Errorf("hits = %d, want 1", n)
	}
	if toasts := updateToasts(produced); len(toasts) != 0 {
		t.Errorf("toasts on the second Init = %q", toasts)
	}
}

func TestUpdateMarkerSurvivesThemeChange(t *testing.T) {
	f := newFakeReleases(t, 200, `{"tag_name":"v0.2.0"}`)
	opts := withUpdateCheck(testOptions(t), f, filepath.Join(t.TempDir(), "update-check.json"))
	m, _ := startCollecting(t, opts, 120, 30)
	if err := m.applyTheme("nord"); err != nil {
		t.Fatal(err)
	}
	if s := screen(m); !strings.Contains(s, "↑ v0.2.0") {
		t.Errorf("marker lost after a theme change:\n%s", s)
	}
}

func TestUpdateHintInToast(t *testing.T) {
	f := newFakeReleases(t, 200, `{"tag_name":"v0.2.0"}`)
	opts := withUpdateCheck(testOptions(t), f, filepath.Join(t.TempDir(), "update-check.json"))
	opts.UpdateCheck.Exe = "/home/me/go/bin/notty"
	_, produced := startCollecting(t, opts, 120, 30)
	want := "Notty v0.2.0 is available — go install github.com/mathieucroset/notty/cmd/notty@latest"
	if toasts := updateToasts(produced); len(toasts) != 1 || toasts[0] != want {
		t.Errorf("toasts = %q, want [%q]", toasts, want)
	}
}

// TestUpdateCheckAfterWizard checks that the check waits for the wizard to
// open the vault, and honours the vault's settings loaded then.
func TestUpdateCheckAfterWizard(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		hits     int32
	}{
		{"enabled", "", 1},
		{"disabled in the vault settings", "update_check = false\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gittest.Isolate(t)
			setIdentity(t)
			f := newFakeReleases(t, 200, `{"tag_name":"v0.2.0"}`)
			opts := withUpdateCheck(wizardOptions(t), f, filepath.Join(t.TempDir(), "update-check.json"))
			if tt.settings != "" {
				p := filepath.Join(opts.Config.Vault, ".notty", "settings.toml")
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(tt.settings), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			m := start(t, opts, 100, 30)
			t.Cleanup(m.Shutdown)
			if n := f.hits.Load(); n != 0 {
				t.Fatalf("checked during the wizard (%d requests)", n)
			}
			run(t, m, keyMsg("enter")) // vault folder
			run(t, m, keyMsg("j"))     // Existing URL → Local only
			run(t, m, keyMsg("enter"))
			waitFor(t, m, func() bool { return m.wizard != nil && m.wizard.Stage() == wizard.StageTheme })
			run(t, m, keyMsg("enter")) // theme
			finishWizard(t, m)
			waitFor(t, m, func() bool { return m.sync.State == msgs.SyncLocalOnly })
			if n := f.hits.Load(); n != tt.hits {
				t.Errorf("hits = %d, want %d", n, tt.hits)
			}
			if got, want := strings.Contains(screen(m), "↑ v0.2.0"), tt.hits > 0; got != want {
				t.Errorf("marker shown = %v, want %v:\n%s", got, want, screen(m))
			}
		})
	}
}
