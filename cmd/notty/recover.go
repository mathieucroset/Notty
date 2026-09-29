package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mathieucroset/notty/internal/logging"
	"github.com/mathieucroset/notty/internal/recovery"
)

// crashMessage handles a panic caught while the TUI ran, once the terminal
// is restored (spec §9): it saves the unsaved buffer held by snap to
// .notty/recovery/, logs the panic with its stack, and returns the message
// for the user, naming the recovery file and the log at logPath ("" when
// there is no log).
func crashMessage(snap *recovery.Snapshot, logPath string, now time.Time) string {
	v, stack, recorded := snap.Panic()
	var b strings.Builder
	if recorded {
		slog.Error("panic", "value", fmt.Sprint(v), "stack", string(stack))
		fmt.Fprintf(&b, "crashed on an internal error (%v), sorry.\n", v)
	} else {
		// Bubble Tea caught it and printed the trace itself.
		slog.Error("panic caught by Bubble Tea; its trace was printed to stderr")
		b.WriteString("crashed on an internal error, sorry.\n")
	}
	_, rel, _, _ := snap.Get()
	switch saved, err := snap.SaveBuffer(now); {
	case err != nil:
		slog.Error("could not save the unsaved buffer", "note", rel, "err", err)
		fmt.Fprintf(&b, "It could not save your unsaved changes to %s: %v\n", rel, err)
	case saved != "":
		slog.Info("unsaved buffer saved", "note", rel, "recovery", saved)
		fmt.Fprintf(&b, "Your unsaved changes to %s are in %s;\nNotty offers to restore them the next time it starts.\n", rel, saved)
	default:
		b.WriteString("No unsaved changes were lost.\n")
	}
	if logPath != "" {
		fmt.Fprintf(&b, "Details are in %s.\n", logPath)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// logPathIfAny is the log file in stateDir, or "" when there is none.
func logPathIfAny(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	p := logging.Path(stateDir)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}
