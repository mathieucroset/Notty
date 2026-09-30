package gittest

import "testing"

// TestIsolateDisablesBackgroundHousekeeping: git may start gc or maintenance
// in the background after a push or fetch; if it is still writing when the
// test ends, t.TempDir's cleanup fails with "directory not empty".
func TestIsolateDisablesBackgroundHousekeeping(t *testing.T) {
	Isolate(t)
	tests := []struct{ key, want string }{
		{"gc.auto", "0"},
		{"gc.autoDetach", "false"},
		{"maintenance.auto", "false"},
		{"receive.autogc", "false"},
		{"fetch.writeCommitGraph", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := Git(t, "", "config", "--global", "--get", tt.key); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
