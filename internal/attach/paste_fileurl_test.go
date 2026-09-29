package attach

import "testing"

// TestParsePastedPathFileURLAuthority verifies that a "file://" URL's
// authority (the host component before the path, such as "localhost") is
// stripped rather than treated as part of the path, and that a Windows
// drive path keeps its "C:/..." form (no extra leading "/") whether or not
// an authority was present.
func TestParsePastedPathFileURLAuthority(t *testing.T) {
	tests := []struct {
		name  string
		paste string
		goos  string
		want  string
	}{
		{"localhost authority stripped", "file://localhost/Users/me/pic.png", "linux", "/Users/me/pic.png"},
		{"empty authority kept", "file:///Users/me/pic.png", "linux", "/Users/me/pic.png"},
		{"windows drive with empty authority", "file:///C:/x.png", "windows", "C:/x.png"},
		{"windows drive with localhost authority", "file://localhost/C:/x.png", "windows", "C:/x.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen string
			exists := func(p string) bool {
				seen = p
				return true
			}
			got, ok := parsePastedPath(tt.paste, exists, tt.goos)
			if !ok {
				t.Fatalf("parsePastedPath(%q, %s) ok = false, want true", tt.paste, tt.goos)
			}
			if got != tt.want {
				t.Errorf("parsePastedPath(%q, %s) = %q, want %q", tt.paste, tt.goos, got, tt.want)
			}
			if seen != tt.want {
				t.Errorf("exists() was called with %q, want %q", seen, tt.want)
			}
		})
	}
}
