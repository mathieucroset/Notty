package attach

import "testing"

// TestParsePastedPathWindowsBackslashes verifies that on Windows,
// backslashes are treated as path separators (never shell escapes), so a
// drive path or a UNC share survives unchanged, unlike on other platforms
// where a lone backslash is shell-escape syntax.
func TestParsePastedPathWindowsBackslashes(t *testing.T) {
	exists := func(string) bool { return true }

	tests := []struct {
		name   string
		paste  string
		goos   string
		want   string
		wantOK bool
	}{
		{"windows drive path kept verbatim", `C:\Users\me\pic.png`, "windows", `C:\Users\me\pic.png`, true},
		{"windows UNC share kept verbatim", `\\server\share\pic.png`, "windows", `\\server\share\pic.png`, true},
		{"windows quoted drive path", `"C:\Users\me\pic.png"`, "windows", `C:\Users\me\pic.png`, true},
		{"non-windows backslash-escaped space unescaped", `/tmp/my\ pic.png`, "linux", `/tmp/my pic.png`, true},
		{"non-windows drive-style backslashes get mangled", `C:\Users\me\pic.png`, "linux", `C:Usersmepic.png`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePastedPath(tt.paste, exists, tt.goos)
			if ok != tt.wantOK {
				t.Fatalf("parsePastedPath(%q, %s) ok = %v, want %v (got %q)", tt.paste, tt.goos, ok, tt.wantOK, got)
			}
			if ok && got != tt.want {
				t.Errorf("parsePastedPath(%q, %s) = %q, want %q", tt.paste, tt.goos, got, tt.want)
			}
		})
	}
}
