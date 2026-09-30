package attach

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParsePastedPath(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "my photo (1).png")
	if err := os.WriteFile(imgPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir available: %v", err)
	}
	homeImg := filepath.Join(home, "notty-test-home-img.png")
	if err := os.WriteFile(homeImg, []byte("x"), 0o644); err != nil {
		t.Skipf("cannot write to home dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(homeImg) })

	exists := func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}

	tests := []struct {
		name   string
		paste  string
		want   string
		wantOK bool
		// shellEscaped pastes rely on POSIX shell backslash escapes.
		shellEscaped bool
	}{
		{"plain existing path", imgPath, imgPath, true, false},
		{"double quoted", `"` + imgPath + `"`, imgPath, true, false},
		{"single quoted", "'" + imgPath + "'", imgPath, true, false},
		{"surrounding whitespace and newline trimmed", "  " + imgPath + "\n", imgPath, true, false},
		{"file:// prefix", "file://" + imgPath, imgPath, true, false},
		{"file:// with %20 encoding", "file://" + filepath.Join(dir, "my%20photo%20(1).png"), imgPath, true, false},
		{"shell escaped spaces", filepath.Join(dir, `my\ photo\ (1).png`), imgPath, true, true},
		{"shell escaped parens", filepath.Join(dir, `my photo \(1\).png`), imgPath, true, true},
		{"tilde expansion", "~/notty-test-home-img.png", homeImg, true, false},
		{"multi-line rejected", imgPath + "\nsecond line", "", false, false},
		{"non-image extension rejected", txtPath, "", false, false},
		{"non-existent path rejected", filepath.Join(dir, "missing.png"), "", false, false},
		{"empty input rejected", "", "", false, false},
		{"uppercase extension accepted", func() string {
			p := filepath.Join(dir, "upper.PNG")
			_ = os.WriteFile(p, []byte("x"), 0o644)
			return p
		}(), filepath.Join(dir, "upper.PNG"), true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shellEscaped && runtime.GOOS == "windows" {
				t.Skip(`on Windows "\" is a path separator, never a shell escape (see windows_paste_test.go)`)
			}
			got, ok := ParsePastedPath(tt.paste, exists)
			if ok != tt.wantOK {
				t.Fatalf("ParsePastedPath(%q) ok = %v, want %v (got %q)", tt.paste, ok, tt.wantOK, got)
			}
			if ok && got != tt.want {
				t.Errorf("ParsePastedPath(%q) = %q, want %q", tt.paste, got, tt.want)
			}
		})
	}
}

func TestSizeMB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	data := make([]byte, 2*1024*1024) // 2 MiB
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SizeMB(path)
	if err != nil {
		t.Fatalf("SizeMB: %v", err)
	}
	if got != 2 {
		t.Errorf("SizeMB = %v, want 2", got)
	}

	if _, err := SizeMB(filepath.Join(dir, "missing.bin")); err == nil {
		t.Error("SizeMB(missing) = nil error, want error")
	}
}
