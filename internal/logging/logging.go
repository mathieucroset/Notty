// Package logging sets up Notty's log file (spec §9): notty.log in the state
// directory, rotated to notty.log.1 at startup once it exceeds MaxSize.
package logging

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// FileName is the log file's name inside the state directory.
const FileName = "notty.log"

// MaxSize is the size above which the log is rotated at startup (5 MB).
const MaxSize = 5 << 20

// Path is the log file in stateDir.
func Path(stateDir string) string { return filepath.Join(stateDir, FileName) }

// Setup opens stateDir/notty.log for appending, creating the directory,
// after moving a log bigger than MaxSize to notty.log.1. It returns a text
// logger at Info level (Debug when NOTTY_DEBUG=1), which it also installs
// as slog's default so packages can log without plumbing, and a close func
// that restores the previous default and closes the file.
func Setup(stateDir string) (*slog.Logger, func() error, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("logging: %w", err)
	}
	p := Path(stateDir)
	if err := rotate(p); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("logging: %w", err)
	}
	level := slog.LevelInfo
	if os.Getenv("NOTTY_DEBUG") == "1" {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level}))
	prev := slog.Default()
	slog.SetDefault(logger)
	var once sync.Once
	var closeErr error
	closeLog := func() error {
		once.Do(func() {
			slog.SetDefault(prev)
			closeErr = f.Close()
		})
		return closeErr
	}
	return logger, closeLog, nil
}

// rotate moves the log at p to p.1, replacing it, when p exceeds MaxSize.
func rotate(p string) error {
	info, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("logging: %w", err)
	}
	if info.Size() <= MaxSize {
		return nil
	}
	if err := os.Rename(p, p+".1"); err != nil {
		return fmt.Errorf("logging: rotate: %w", err)
	}
	return nil
}
