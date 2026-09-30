package links

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckImageFile(t *testing.T) {
	type setup func(t *testing.T, vault, outside string) string // returns the path to check
	file := func(rel string) setup {
		return func(t *testing.T, vault, _ string) string {
			t.Helper()
			p := filepath.Join(vault, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	link := func(name string, target func(vault, outside string) string) setup {
		return func(t *testing.T, vault, outside string) string {
			t.Helper()
			if runtime.GOOS == "windows" {
				t.Skip("creating symlinks needs a privilege on Windows")
			}
			tgt := target(vault, outside)
			if err := os.WriteFile(tgt, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(vault, name)
			if err := os.Symlink(tgt, p); err != nil {
				t.Fatal(err)
			}
			return p
		}
	}
	tests := []struct {
		name  string
		setup setup
		want  error // nil: may be opened
	}{
		{"png", file("pic.png"), nil},
		{"upper-case jpeg in a folder", file("attachments/Photo.JPEG"), nil},
		{"gif", file("a.gif"), nil},
		{"webp", file("a.webp"), nil},
		{"batch file", file("run.bat"), ErrNotImage},
		{"macOS .command", file("run.command"), ErrNotImage},
		{"executable", file("setup.exe"), ErrNotImage},
		{"no extension", file("script"), ErrNotImage},
		{"double extension", file("pic.png.cmd"), ErrNotImage},
		{"symlink to a script", link("pic.png", func(v, _ string) string { return filepath.Join(v, "script.command") }), ErrNotImage},
		{"symlink to an image outside the vault", link("pic.png", func(_, o string) string { return filepath.Join(o, "secret.png") }), ErrOutsideVault},
		{"symlink to an image inside the vault", link("alias.png", func(v, _ string) string { return filepath.Join(v, "real.png") }), nil},
		{"directory named like an image", func(t *testing.T, vault, _ string) string {
			p := filepath.Join(vault, "dir.png")
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
			return p
		}, ErrNotImage},
		{"outside the vault", func(t *testing.T, _, outside string) string {
			p := filepath.Join(outside, "o.png")
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}, ErrOutsideVault},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vault, outside := t.TempDir(), t.TempDir()
			p := tc.setup(t, vault, outside)
			err := CheckImageFile(vault, p)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("CheckImageFile(%s) = %v, want nil", p, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("CheckImageFile(%s) = %v, want %v", p, err, tc.want)
			}
		})
	}
}
