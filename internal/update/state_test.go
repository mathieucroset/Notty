package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func TestLoadState(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		body *string // nil: no file
		want State
	}{
		{"missing", nil, State{}},
		{"corrupt", ptr(`{"checked_at":`), State{}},
		{"not an object", ptr(`[1,2]`), State{}},
		{"empty", ptr(``), State{}},
		{"valid", ptr(`{"checked_at":"2026-09-30T12:00:00Z","latest":"v0.2.0","notified":"v0.1.1"}`),
			State{CheckedAt: t0, Latest: "v0.2.0", Notified: "v0.1.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".json")
			if tt.body != nil {
				if err := os.WriteFile(path, []byte(*tt.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := LoadState(path)
			if !got.CheckedAt.Equal(tt.want.CheckedAt) || got.Latest != tt.want.Latest || got.Notified != tt.want.Notified {
				t.Errorf("LoadState = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestSaveRoundTrips(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "notty")
	path := filepath.Join(dir, "update-check.json")
	want := State{CheckedAt: t0, Latest: "v0.2.0", Notified: "v0.2.0"}
	if err := want.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := LoadState(path)
	if !got.CheckedAt.Equal(want.CheckedAt) || got.Latest != want.Latest || got.Notified != want.Notified {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}

	// Overwriting keeps a single, complete file and leaves no temp files.
	next := State{CheckedAt: t0.Add(time.Hour), Latest: "v0.3.0", Notified: "v0.2.0"}
	if err := next.Save(path); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if got := LoadState(path); got.Latest != "v0.3.0" {
		t.Errorf("after overwrite Latest = %q", got.Latest)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "update-check.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("state dir holds %v, want only update-check.json", names)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("file mode = %o, want 600", perm)
		}
		dinfo, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := dinfo.Mode().Perm(); perm != 0o700 {
			t.Errorf("dir mode = %o, want 700", perm)
		}
	}
}

func TestSaveRemovesStaleTempFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	files := []struct {
		name string
		age  time.Duration
		kept bool
	}{
		{".update-check-111.tmp", 2 * time.Minute, false},   // left by a crash
		{".update-check-222.tmp", 48 * time.Hour, false},    // long gone
		{".update-check-333.tmp", 10 * time.Second, true},   // another instance saving now
		{".notty-config-444.tmp", 48 * time.Hour, true},     // not ours
		{"update-check-555.tmp", 48 * time.Hour, true},      // not ours
		{".update-check-666.tmp.bak", 48 * time.Hour, true}, // not ours
		{"notty.log", 48 * time.Hour, true},                 // not ours
		{"repo-1234abcd.json", 48 * time.Hour, true},        // not ours
		{".update-check-777.tmpx", 48 * time.Hour, true},    // not ours
		{".update-check-.tmp", 2 * time.Minute, false},      // empty random part
		{".update-check-888.tmp", 61 * time.Second, false},  // just over a minute
		{".update-check-999.tmp", 59 * time.Second, true},   // just under a minute
		{".update-check-000.tmp", -time.Hour, true},         // from the future
		{".update-check-aaa.tmp", time.Minute + time.Hour, false},
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(-f.age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "update-check.json")
	if err := (State{Latest: "v0.2.0"}).Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, f := range files {
		_, err := os.Stat(filepath.Join(dir, f.name))
		if kept := err == nil; kept != f.kept {
			t.Errorf("%s (age %v): kept = %v, want %v", f.name, f.age, kept, f.kept)
		}
	}
	if got := LoadState(path); got.Latest != "v0.2.0" {
		t.Errorf("state not saved: %+v", got)
	}
}

func TestSaveFailsWhenTheDirIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (State{Latest: "v0.2.0"}).Save(filepath.Join(blocker, "update-check.json")); err == nil {
		t.Error("Save under a regular file returned no error")
	}
}

func TestDue(t *testing.T) {
	tests := []struct {
		name      string
		checkedAt time.Time
		want      bool
	}{
		{"never checked", time.Time{}, true},
		{"just checked", t0, false},
		{"within 24h", t0.Add(-23*time.Hour - 59*time.Minute), false},
		{"exactly 24h", t0.Add(-24 * time.Hour), true},
		{"over 24h", t0.Add(-72 * time.Hour), true},
		{"in the future", t0.Add(time.Minute), true},
	}
	for _, tt := range tests {
		if got := Due(State{CheckedAt: tt.checkedAt, Latest: "v0.2.0"}, t0); got != tt.want {
			t.Errorf("%s: Due = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestFailed(t *testing.T) {
	s := State{CheckedAt: t0.Add(-48 * time.Hour), Latest: "v0.2.0", Notified: "v0.2.0"}
	got := Failed(s, t0)
	want := State{CheckedAt: t0, Latest: "v0.2.0", Notified: "v0.2.0"}
	if got != want {
		t.Errorf("Failed = %+v, want %+v", got, want)
	}
	if Due(got, t0.Add(time.Hour)) {
		t.Error("a failed check is retried within the day")
	}
}

func TestApply(t *testing.T) {
	old := t0.Add(-2 * time.Hour)
	rel := func(v string) *Release { return &Release{Version: v} }
	tests := []struct {
		name      string
		s         State
		fetched   *Release
		current   string
		available string
		toast     bool
		want      State
	}{
		{
			name:      "first run, newer release: toast",
			fetched:   rel("v0.2.0"),
			current:   "0.1.0",
			available: "v0.2.0",
			toast:     true,
			want:      State{CheckedAt: t0, Latest: "v0.2.0", Notified: "v0.2.0"},
		},
		{
			name:    "first run, current is newest: nothing",
			fetched: rel("v0.1.0"),
			current: "0.1.0",
			want:    State{CheckedAt: t0, Latest: "v0.1.0"},
		},
		{
			name:      "within 24h uses the cache: marker, no second toast",
			s:         State{CheckedAt: old, Latest: "v0.2.0", Notified: "v0.2.0"},
			current:   "0.1.0",
			available: "v0.2.0",
			want:      State{CheckedAt: old, Latest: "v0.2.0", Notified: "v0.2.0"},
		},
		{
			name:      "cache with a newer release never notified: toast",
			s:         State{CheckedAt: old, Latest: "v0.2.0"},
			current:   "0.1.0",
			available: "v0.2.0",
			toast:     true,
			want:      State{CheckedAt: old, Latest: "v0.2.0", Notified: "v0.2.0"},
		},
		{
			name:      "same version fetched again, already notified: marker only",
			s:         State{CheckedAt: t0.Add(-25 * time.Hour), Latest: "v0.2.0", Notified: "v0.2.0"},
			fetched:   rel("v0.2.0"),
			current:   "0.1.0",
			available: "v0.2.0",
			want:      State{CheckedAt: t0, Latest: "v0.2.0", Notified: "v0.2.0"},
		},
		{
			name:      "newer again: toast again",
			s:         State{CheckedAt: t0.Add(-25 * time.Hour), Latest: "v0.2.0", Notified: "v0.2.0"},
			fetched:   rel("v0.3.0"),
			current:   "0.1.0",
			available: "v0.3.0",
			toast:     true,
			want:      State{CheckedAt: t0, Latest: "v0.3.0", Notified: "v0.3.0"},
		},
		{
			name:    "upgraded since: nothing",
			s:       State{CheckedAt: old, Latest: "v0.2.0", Notified: "v0.2.0"},
			current: "0.2.0",
			want:    State{CheckedAt: old, Latest: "v0.2.0", Notified: "v0.2.0"},
		},
		{
			name:    "running a newer build than the cache: nothing",
			s:       State{CheckedAt: old, Latest: "v0.2.0"},
			current: "v0.3.0",
			want:    State{CheckedAt: old, Latest: "v0.2.0"},
		},
		{
			name:      "fetch failure keeps the cache",
			s:         Failed(State{CheckedAt: t0.Add(-30 * time.Hour), Latest: "v0.2.0", Notified: "v0.2.0"}, t0),
			current:   "0.1.0",
			available: "v0.2.0",
			want:      State{CheckedAt: t0, Latest: "v0.2.0", Notified: "v0.2.0"},
		},
		{
			name:    "empty cache, no fetch: nothing",
			current: "0.1.0",
			want:    State{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Apply(tt.s, tt.fetched, tt.current, t0)
			if got.Available.Version != tt.available {
				t.Errorf("Available = %q, want %q", got.Available.Version, tt.available)
			}
			if got.Toast != tt.toast {
				t.Errorf("Toast = %v, want %v", got.Toast, tt.toast)
			}
			if got.State != tt.want {
				t.Errorf("State = %+v, want %+v", got.State, tt.want)
			}
		})
	}
}
