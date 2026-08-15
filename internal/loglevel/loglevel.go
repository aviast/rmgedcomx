// Package loglevel defines the server's logging levels shared across
// packages. slog itself has no trace level, so trace is one step below Debug.
package loglevel

import (
	"fmt"
	"log/slog"
	"strings"
)

// Trace is more verbose than slog.LevelDebug. The server uses it for full
// request and response details on every request.
const Trace slog.Level = slog.LevelDebug - 4

// Parse converts a command-line log level into its slog equivalent.
func Parse(value string) (slog.Level, error) {
	if strings.EqualFold(strings.TrimSpace(value), "trace") {
		return Trace, nil
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return 0, fmt.Errorf("invalid log level %q: %w", value, err)
	}
	return level, nil
}
