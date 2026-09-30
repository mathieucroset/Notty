package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/vault"
)

// quickSyncWait is how long notty -q's background sync waits for the vault
// lock: long enough for another capture's sync to finish; while Notty is
// open it gives up, and the TUI syncs the change itself.
const quickSyncWait = "60s"

// runQuick is `notty -q <text> [--folder X]`: it appends "- (<context>)
// <text>" to <folder>/Inbox.md without taking the vault lock, then starts
// a background `notty sync` in a git vault. rest is what the global flags
// left; afterDash means they ended with "--", so rest is all text. It
// exits 0 once the line is written, 1 when it cannot be, and 2 on a usage
// error.
func runQuick(rest []string, afterDash bool, vaultFlag, folderFlag string, e env) int {
	flags, vaultPath := subcommandFlags("-q", vaultFlag, e)
	folder := flags.String("folder", folderFlag, "vault-relative `folder` of the Inbox")
	words := rest
	if !afterDash {
		var err error
		if words, err = parseInterspersed(flags, rest); err != nil {
			return flagExit(err)
		}
	}
	text := quickText(words)
	if text == "" {
		_, _ = fmt.Fprintf(e.stderr, "notty -q: expected the note text\n%s", usage)
		return 2
	}
	dir, err := vault.ValidateFolderPath(*folder)
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 2
	}

	// The vault must be set up already: -q never runs the wizard nor
	// creates a vault (amendment A2).
	root, err := vaultRoot(*vaultPath, e)
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	if _, err := os.Stat(e.configPath); err != nil || !isDir(root) {
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", errNeedsSetup)
		return 1
	}
	v, err := vault.Open(root)
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	// An existing folder typed in another case is that folder, as on a
	// case-insensitive filesystem: use its real case for the Inbox path.
	dir = v.RealFolderCase(dir)

	var repo *gitsync.Repo
	if _, err := e.lookPath("git"); err == nil {
		if r := gitsync.Open(v.Root); r.IsRepo() {
			repo = r
		}
	}
	// An Inbox conflicted in an unfinished merge is left alone: appending
	// to its conflict markers would confuse the resolver (amendment B5).
	target := path.Join(dir, vault.InboxName)
	if repo != nil && inConflict(repo, target) {
		_, _ = fmt.Fprintf(e.stderr, "notty: %s has an unresolved sync conflict; open Notty to resolve it\n", target)
		return 1
	}

	var cwd string
	if e.getwd != nil {
		cwd, _ = e.getwd()
	}
	home, _ := os.UserHomeDir()
	rel, err := v.AppendToInbox(dir, quickLine(text, quickContext(cwd, home, v.Root)))
	if err != nil {
		slog.Error("quick note not written", "err", err)
		_, _ = fmt.Fprintf(e.stderr, "notty: %v\n", err)
		return 1
	}
	slog.Info("quick note added", "note", rel)
	if repo == nil {
		_, _ = fmt.Fprintf(e.stdout, "✓ Added to %s\n", rel)
		return 0
	}

	// The child waits for the lock, so a capture while Notty or another
	// capture's sync runs is synced after it (amendment A5).
	args := []string{"--vault", v.Root, "sync", "--quiet", "--wait", quickSyncWait}
	if err := e.startBackground(args); err != nil {
		slog.Warn("background sync not started", "err", err)
		_, _ = fmt.Fprintf(e.stdout, "✓ Added to %s\n", rel)
		_, _ = fmt.Fprintf(e.stderr, "notty: background sync not started: %v\n", err)
		return 0
	}
	_, _ = fmt.Fprintf(e.stdout, "✓ Added to %s (syncing in the background)\n", rel)
	return 0
}

// inConflict reports whether rel is an unmerged path of a merge in
// progress in repo.
func inConflict(repo *gitsync.Repo, rel string) bool {
	if !repo.MergeInProgress() {
		return false
	}
	conflicts, err := repo.ConflictedFiles()
	if err != nil {
		slog.Warn("listing conflicts failed", "err", err)
		return false
	}
	for _, c := range conflicts {
		if c.Path == rel || (caseInsensitiveFS() && strings.EqualFold(c.Path, rel)) {
			return true
		}
	}
	return false
}

// caseInsensitiveFS reports whether file names usually ignore case here
// (Windows and macOS by default), so paths differing only in case name the
// same file.
func caseInsensitiveFS() bool { return runtime.GOOS == "windows" || runtime.GOOS == "darwin" }

// quickText joins the words of a quick note with single spaces into one
// line: line breaks become spaces, and surrounding whitespace is trimmed.
func quickText(words []string) string {
	s := strings.Join(words, " ")
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
	return strings.TrimSpace(s)
}

// quickLine formats a quick note as a list item, prefixed with its context
// in parentheses unless context is empty.
func quickLine(text, context string) string {
	if context == "" {
		return "- " + text
	}
	return "- (" + context + ") " + text
}

// quickContext returns the context of a quick note taken in the working
// directory cwd: its name, or "" when it is unknown, a filesystem or volume
// root, the home directory or the vault root. Directories are compared
// after resolving symlinks, and ignoring case on Windows.
func quickContext(cwd, home, root string) string {
	if cwd == "" {
		return ""
	}
	logical := filepath.Clean(cwd)
	resolved := resolvePath(logical)
	if isFSRoot(logical) || isFSRoot(resolved) {
		return ""
	}
	for _, p := range []string{home, root} {
		if p != "" && samePath(resolved, resolvePath(p)) {
			return ""
		}
	}
	return filepath.Base(logical)
}

// isFSRoot reports whether the clean path p is a filesystem root ("/") or
// a volume root (`C:\`, `\\server\share\`).
func isFSRoot(p string) bool { return filepath.Dir(p) == p }

// samePath compares two clean paths, ignoring case on Windows.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// flagSet reports whether the flag name was given on the command line.
func flagSet(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// quickFlags adds -q/--quick and --folder to the global flag set.
func quickFlags(flags *flag.FlagSet) (quick *bool, folder *string) {
	quick = flags.Bool("q", false, "capture a quick note: append the text to the Inbox")
	flags.BoolVar(quick, "quick", false, "same as -q")
	folder = flags.String("folder", "", "with -q, the vault-relative `folder` of the Inbox")
	return quick, folder
}
