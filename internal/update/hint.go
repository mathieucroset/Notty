package update

import (
	"path"
	"strings"
)

// Upgrade hints, one per install method. The download hint is short so it
// doesn't split badly in a toast.
const (
	hintBrew     = "brew upgrade notty"
	hintGo       = "go install github.com/mathieucroset/notty/cmd/notty@latest"
	hintDownload = "download it from github.com/mathieucroset/notty/releases"
)

// brewPrefixes and brewDirs mark a binary installed by Homebrew.
var (
	brewPrefixes = []string{"/opt/homebrew/", "/usr/local/Homebrew/", "/home/linuxbrew/.linuxbrew/"}
	brewDirs     = []string{"/Cellar/", "/Caskroom/"}
)

// UpgradeHint returns how to upgrade a binary at exe (os.Executable(),
// symlinks resolved), using goBin (symlinks resolved too) on the OS goos
// ("windows", "darwin", "linux", …): Homebrew, go install, or a download.
// Paths are compared with string handling rather than filepath, so the
// result does not depend on the OS running it: for goos "windows", "\"
// and "/" are both separators and the go bin comparison ignores case.
func UpgradeHint(exe, goBin, goos string) string {
	if exe == "" {
		return hintDownload
	}
	p := cleanPath(exe, goos)
	for _, prefix := range brewPrefixes {
		if strings.HasPrefix(p, prefix) {
			return hintBrew
		}
	}
	for _, dir := range brewDirs {
		if strings.Contains(p, dir) {
			return hintBrew
		}
	}
	if goBin != "" {
		dir, bin := path.Dir(p), cleanPath(goBin, goos)
		if goos == "windows" {
			dir, bin = strings.ToLower(dir), strings.ToLower(bin)
		}
		if dir == bin {
			return hintGo
		}
	}
	return hintDownload
}

// cleanPath cleans p as a slash-separated path, first turning Windows
// separators into slashes when goos is "windows".
func cleanPath(p, goos string) string {
	if goos == "windows" {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	return path.Clean(p)
}
