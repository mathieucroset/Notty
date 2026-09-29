package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var gitignoreLines = []string{".DS_Store", "Thumbs.db", "desktop.ini", "*.notty-tmp", ".notty/recovery/", ".notty/lock"}

func TestEnsureGitignore(t *testing.T) {
	all := strings.Join(gitignoreLines, "\n") + "\n"
	tests := []struct {
		name     string
		existing *string
		want     string
		changed  bool
	}{
		{"missing file", nil, all, true},
		{"empty file", ptr(""), all, true},
		{"keeps existing lines", ptr("node_modules/\n*.log\n"), "node_modules/\n*.log\n" + all, true},
		{"adds trailing newline before appending", ptr("build"), "build\n" + all, true},
		{"only missing lines appended", ptr(".notty/lock\nfoo\n.DS_Store\n"),
			".notty/lock\nfoo\n.DS_Store\nThumbs.db\ndesktop.ini\n*.notty-tmp\n.notty/recovery/\n", true},
		{"surrounding whitespace ignored", ptr("  .DS_Store  \n" + strings.Join(gitignoreLines[1:], "\n") + "\n"),
			"  .DS_Store  \n" + strings.Join(gitignoreLines[1:], "\n") + "\n", false},
		{"crlf kept", ptr("foo\r\n"), "foo\r\n" + strings.Join(gitignoreLines, "\r\n") + "\r\n", true},
		{"complete", ptr(all), all, false},
		{"complete without trailing newline", ptr(strings.TrimSuffix(all, "\n")), strings.TrimSuffix(all, "\n"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, ".gitignore")
			if tt.existing != nil {
				if err := os.WriteFile(p, []byte(*tt.existing), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := EnsureGitignore(root)
			if err != nil {
				t.Fatalf("EnsureGitignore: %v", err)
			}
			if changed != tt.changed {
				t.Errorf("changed = %v, want %v", changed, tt.changed)
			}
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.want {
				t.Errorf("content = %q, want %q", b, tt.want)
			}
			// Idempotent.
			changed, err = EnsureGitignore(root)
			if err != nil || changed {
				t.Errorf("second EnsureGitignore = %v, %v; want false, nil", changed, err)
			}
			if b2, _ := os.ReadFile(p); string(b2) != string(b) {
				t.Errorf("second call changed content to %q", b2)
			}
			assertNoTmp(t, root)
		})
	}
}

func ptr(s string) *string { return &s }
