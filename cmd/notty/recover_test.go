package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/recovery"
)

// captureLog sends slog's default logger to a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestCrashMessage(t *testing.T) {
	at := time.Date(2026, 9, 29, 14, 3, 7, 0, time.Local)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ideas.md"), []byte("# Ideas\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	recFile := filepath.Join(root, ".notty", "recovery", "ideas-20260929-140307.md")
	tests := []struct {
		name     string
		root     string
		dirty    bool
		panicked bool
		logPath  string
		want     []string
		wantLog  []string
		saved    bool
	}{
		{"unsaved edits", root, true, true, "/state/notty.log",
			[]string{"crashed", "boom", "unsaved changes to ideas.md", recFile, "next time", "/state/notty.log"},
			[]string{"level=ERROR", "boom", "stack=", "recovery=" + recFile}, true},
		{"nothing unsaved", root, false, true, "/state/notty.log",
			[]string{"crashed", "boom", "No unsaved changes were lost", "/state/notty.log"},
			[]string{"level=ERROR", "boom"}, false},
		{"save fails", notDir, true, true, "/state/notty.log",
			[]string{"crashed", "could not save your unsaved changes to ideas.md", "/state/notty.log"},
			[]string{"level=ERROR", "could not save"}, false},
		{"no log", root, false, true, "",
			[]string{"crashed", "boom"}, nil, false},
		{"caught by Bubble Tea", root, false, false, "/state/notty.log",
			[]string{"crashed", "No unsaved changes were lost"},
			[]string{"level=ERROR"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logBuf := captureLog(t)
			snap := &recovery.Snapshot{}
			snap.Set(tt.root, "ideas.md", "# Ideas\n\nunsaved\n", tt.dirty)
			if tt.panicked {
				snap.RecordPanic("boom", []byte("goroutine 1 [running]:\nmain.explode()"))
			}
			msg := crashMessage(snap, tt.logPath, at)
			for _, w := range tt.want {
				if !strings.Contains(msg, w) {
					t.Errorf("message %q lacks %q", msg, w)
				}
			}
			if tt.logPath == "" && strings.Contains(msg, "notty.log") {
				t.Errorf("message %q names a log that is not there", msg)
			}
			for _, w := range tt.wantLog {
				if !strings.Contains(logBuf.String(), w) {
					t.Errorf("log %q lacks %q", logBuf.String(), w)
				}
			}
			_, err := os.Stat(recFile)
			if saved := err == nil; saved != tt.saved {
				t.Errorf("recovery file written = %v, want %v", saved, tt.saved)
			}
			_ = os.Remove(recFile)
		})
	}
}

func TestLogPathIfAny(t *testing.T) {
	dir := t.TempDir()
	if got := logPathIfAny(dir); got != "" {
		t.Errorf("logPathIfAny without a log = %q", got)
	}
	if got := logPathIfAny(""); got != "" {
		t.Errorf("logPathIfAny(\"\") = %q", got)
	}
	p := filepath.Join(dir, "notty.log")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := logPathIfAny(dir); got != p {
		t.Errorf("logPathIfAny = %q, want %q", got, p)
	}
}
