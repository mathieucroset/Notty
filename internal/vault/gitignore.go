package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// gitignoreEntries are the lines EnsureGitignore guarantees (spec §3).
var gitignoreEntries = []string{
	".DS_Store",
	"Thumbs.db",
	"desktop.ini",
	"*" + tmpSuffix,
	".notty/recovery/",
	".notty/lock",
}

// EnsureGitignore makes sure the .gitignore at root lists the OS junk
// files, save temp files, .notty/recovery/ and .notty/lock. Missing lines
// are appended (matching ignores surrounding whitespace); existing content
// is kept byte for byte, and the file ends with a newline, CRLF if it
// already uses CRLF. It reports whether the file was written.
func EnsureGitignore(root string) (changed bool, err error) {
	p := filepath.Join(root, ".gitignore")
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("vault: read .gitignore: %w", err)
	}
	content := string(b)
	have := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, e := range gitignoreEntries {
		if !have[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	var sb strings.Builder
	sb.WriteString(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		sb.WriteString(nl)
	}
	for _, e := range missing {
		sb.WriteString(e + nl)
	}
	perm, keepMode := fs.FileMode(0o644), false
	if fi, err := os.Stat(p); err == nil {
		perm, keepMode = fi.Mode().Perm(), true
	}
	tmp := p + tmpSuffix
	if err := writeSynced(tmp, sb.String(), perm, keepMode); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("vault: write .gitignore: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("vault: write .gitignore: %w", err)
	}
	syncDir(root)
	return true, nil
}
