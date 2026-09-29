package attach

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ParsePastedPath extracts a usable image file path from a bracketed paste
// (spec §6.2 "Pasted path"). It trims surrounding whitespace, strips
// surrounding single or double quotes, strips a "file://" prefix (including
// any authority such as "localhost" before the path) and URL-decodes what
// follows, unescapes backslash-escaped characters (shell escaping such as
// "\ " -> " " and "\(" -> "(") on non-Windows platforms, and expands a
// leading "~/" to the user's home directory. It reports ok=false unless the
// result is a single line, has a supported image extension
// (case-insensitive), and exists(path) is true.
func ParsePastedPath(paste string, exists func(string) bool) (string, bool) {
	return parsePastedPath(paste, exists, runtime.GOOS)
}

// parsePastedPath is ParsePastedPath with the OS injectable for tests: on
// "windows", backslashes are path separators (as in "C:\Users\me\pic.png"
// or "\\server\share\pic.png"), never shell escapes, so unescapeShell must
// not run there.
func parsePastedPath(paste string, exists func(string) bool, goos string) (string, bool) {
	s := strings.Trim(paste, " \t\r\n")
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return "", false
	}

	s = stripQuotes(s)

	if rest, ok := strings.CutPrefix(s, "file://"); ok {
		rest = stripFileURLAuthority(rest, goos)
		if decoded, err := url.PathUnescape(rest); err == nil {
			s = decoded
		} else {
			s = rest
		}
	}

	if goos != "windows" {
		s = unescapeShell(s)
	}

	if rest, ok := strings.CutPrefix(s, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			s = filepath.Join(home, rest)
		}
	}

	if s == "" || strings.ContainsAny(s, "\r\n") {
		return "", false
	}
	if !hasImageExt(s) {
		return "", false
	}
	if exists == nil || !exists(s) {
		return "", false
	}
	return s, true
}

// stripFileURLAuthority removes a "file://" URL's authority component (the
// part between the scheme and the next "/", such as "localhost" in
// "file://localhost/Users/me/pic.png"), leaving a path that starts with
// "/". rest is what remains of the URL after the "file://" prefix itself
// has already been removed, so "file://localhost/Users/me/pic.png" ->
// "/Users/me/pic.png" and a URL with no authority
// ("file:///Users/me/pic.png" -> rest "/Users/me/pic.png") is left as is.
// On "windows", a resulting "/C:/..." drive path additionally has its
// leading "/" stripped, so "file:///C:/x.png" becomes "C:/x.png" rather
// than "/C:/x.png".
func stripFileURLAuthority(rest, goos string) string {
	if !strings.HasPrefix(rest, "/") {
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			rest = rest[i:]
		} else {
			rest = "/" + rest
		}
	}
	if goos == "windows" && len(rest) >= 4 && isDriveLetter(rest[1]) && rest[2] == ':' &&
		(rest[3] == '/' || rest[3] == '\\') {
		rest = rest[1:]
	}
	return rest
}

// isDriveLetter reports whether b is an ASCII letter, as used in a Windows
// drive letter such as "C:".
func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// stripQuotes removes a single matching pair of surrounding single or
// double quotes, if present.
func stripQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if (first == '"' || first == '\'') && first == last {
		return s[1 : len(s)-1]
	}
	return s
}

// unescapeShell drops a backslash before any character, turning
// shell-escaped input (such as "\ " or "\(") into its literal form.
func unescapeShell(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// hasImageExt reports whether p ends in a supported image extension,
// matched case-insensitively.
func hasImageExt(p string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
	return allowedExt[ext]
}

// SizeMB returns the size of the file at path in megabytes (1024*1024
// bytes), for comparison against images.max_import_mb (spec §6.2, §10).
func SizeMB(path string) (float64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("attach: size %q: %w", path, err)
	}
	return float64(fi.Size()) / (1024 * 1024), nil
}
