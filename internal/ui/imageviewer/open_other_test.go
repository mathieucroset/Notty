//go:build !windows

package imageviewer

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpenCommand(t *testing.T) {
	dir := t.TempDir()
	tricky := filepath.Join(dir, "a & b; $(rm -rf x) 'c'.png")
	rel := "-rf.png" // would look like a flag if passed as is
	absRel, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		goos, path string
		want       []string
	}{
		{"linux", tricky, []string{"xdg-open", tricky}},
		{"freebsd", tricky, []string{"xdg-open", tricky}},
		{"darwin", tricky, []string{"open", tricky}},
		{"linux", rel, []string{"xdg-open", absRel}},
	}
	for _, tt := range tests {
		cmd := openCommand(tt.goos, tt.path)
		if !reflect.DeepEqual(cmd.Args, tt.want) {
			t.Errorf("openCommand(%q, %q).Args = %q, want %q", tt.goos, tt.path, cmd.Args, tt.want)
		}
		if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
			t.Errorf("openCommand(%q) must not share the terminal", tt.goos)
		}
	}
}
