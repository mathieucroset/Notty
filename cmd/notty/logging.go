package main

import (
	"fmt"

	"github.com/mathieucroset/notty/internal/logging"
)

// setupLogging opens the log in the state directory (spec §9) and returns
// the func closing it. Without a usable log Notty still runs, saying so.
func setupLogging(e env) func() {
	if e.stateDir == "" {
		return func() {}
	}
	_, closeLog, err := logging.Setup(e.stateDir)
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "notty: not logging: %v\n", err)
		return func() {}
	}
	return func() { _ = closeLog() }
}
