package update

import "testing"

func TestUpgradeHint(t *testing.T) {
	const (
		brew     = "brew upgrade notty"
		goget    = "go install github.com/mathieucroset/notty/cmd/notty@latest"
		download = "download it from github.com/mathieucroset/notty/releases"
	)
	tests := []struct {
		name            string
		exe, goBin, gos string
		want            string
	}{
		{"macOS cask, Apple silicon", "/opt/homebrew/Caskroom/notty/0.1.0/notty", "/Users/me/go/bin", "darwin", brew},
		{"macOS cask, Intel", "/usr/local/Caskroom/notty/0.1.0/notty", "/Users/me/go/bin", "darwin", brew},
		{"macOS cellar", "/usr/local/Cellar/notty/0.1.0/bin/notty", "/Users/me/go/bin", "darwin", brew},
		{"homebrew prefix", "/opt/homebrew/bin/notty", "", "darwin", brew},
		{"intel homebrew repo", "/usr/local/Homebrew/bin/notty", "", "darwin", brew},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/bin/notty", "/home/me/go/bin", "linux", brew},
		{"linuxbrew cellar", "/home/linuxbrew/.linuxbrew/Cellar/notty/0.1.0/bin/notty", "", "linux", brew},
		{"go bin", "/home/me/go/bin/notty", "/home/me/go/bin", "linux", goget},
		{"go bin with trailing slash", "/home/me/go/bin/notty", "/home/me/go/bin/", "linux", goget},
		{"go bin, unclean path", "/home/me/go/./bin/notty", "/home/me/go/bin", "linux", goget},
		{"go bin subdir is not go bin", "/home/me/go/bin/sub/notty", "/home/me/go/bin", "linux", download},
		{"go bin is case-sensitive on linux", "/home/me/Go/bin/notty", "/home/me/go/bin", "linux", download},
		{"other", "/usr/local/bin/notty", "/home/me/go/bin", "linux", download},
		{"no go bin", "/usr/local/bin/notty", "", "linux", download},
		{"no exe", "", "", "linux", download},
		{"no exe, go bin set", "", "/home/me/go/bin", "linux", download},
		{"usr local but not homebrew", "/usr/local/bin/notty", "", "darwin", download},
		{"windows go bin", `C:\Users\me\go\bin\notty.exe`, `C:\Users\me\go\bin`, "windows", goget},
		{"windows mixed separators and case", `c:\users\ME\go\bin\notty.exe`, `C:/Users/me/go/bin/`, "windows", goget},
		{"windows other", `C:\Program Files\notty\notty.exe`, `C:\Users\me\go\bin`, "windows", download},
		{"backslash is not a separator off windows", `C:\Users\me\go\bin\notty.exe`, `C:\Users\me\go\bin`, "linux", download},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UpgradeHint(tt.exe, tt.goBin, tt.gos); got != tt.want {
				t.Errorf("UpgradeHint(%q, %q, %q) = %q, want %q", tt.exe, tt.goBin, tt.gos, got, tt.want)
			}
		})
	}
}
