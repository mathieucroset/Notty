package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readLog(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetupCreatesTheLog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "notty")
	logger, closeLog, err := Setup(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hello", "k", "v")
	slog.Warn("via default")
	logger.Debug("hidden")
	if err := closeLog(); err != nil {
		t.Fatal(err)
	}
	got := readLog(t, Path(dir))
	for _, want := range []string{"level=INFO msg=hello k=v", "level=WARN msg=\"via default\""} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "hidden") {
		t.Errorf("debug line logged without NOTTY_DEBUG: %q", got)
	}
}

func TestSetupAppends(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("earlier line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, closeLog, err := Setup(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("later")
	_ = closeLog()
	got := readLog(t, Path(dir))
	if !strings.HasPrefix(got, "earlier line\n") || !strings.Contains(got, "later") {
		t.Errorf("log = %q, want the old line kept and the new one appended", got)
	}
	if _, err := os.Stat(Path(dir) + ".1"); err == nil {
		t.Error("a small log was rotated")
	}
}

func TestSetupRotatesABigLog(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", MaxSize+1)
	if err := os.WriteFile(Path(dir), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(dir)+".1", []byte("oldest"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, closeLog, err := Setup(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("fresh")
	_ = closeLog()
	if got := readLog(t, Path(dir)+".1"); got != big {
		t.Errorf("notty.log.1 holds %d bytes, want the %d of the rotated log", len(got), len(big))
	}
	if got := readLog(t, Path(dir)); strings.Contains(got, "xxx") || !strings.Contains(got, "fresh") {
		t.Errorf("notty.log = %.40q, want a fresh log", got)
	}
}

func TestSetupDebugLevel(t *testing.T) {
	t.Setenv("NOTTY_DEBUG", "1")
	dir := t.TempDir()
	logger, closeLog, err := Setup(dir)
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("details")
	_ = closeLog()
	if got := readLog(t, Path(dir)); !strings.Contains(got, "level=DEBUG msg=details") {
		t.Errorf("log = %q, want the debug line", got)
	}
}

func TestCloseRestoresTheDefaultLogger(t *testing.T) {
	before := slog.Default()
	_, closeLog, err := Setup(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if slog.Default() == before {
		t.Error("Setup did not install its logger as the default")
	}
	_ = closeLog()
	if slog.Default() != before {
		t.Error("close did not restore the previous default logger")
	}
}

func TestSetupFailsOnAnUnusableDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Setup(filepath.Join(file, "notty")); err == nil {
		t.Error("Setup succeeded under a regular file")
	}
}
