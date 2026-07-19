// Package output is the shared rendering engine every command uses. It is
// TTY-aware (aligned color tables on a terminal, tab-separated values when
// piped), honors --format=json|yaml|table|text and VIBEXP_FORMAT, applies a
// built-in gojq filter (--jq), and keeps a strict contract: stdout carries only
// data, and --format=json is the raw API response body byte-for-byte.
package output

import (
	"os"

	"golang.org/x/term"
)

// IsTerminal reports whether f is a terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// ColorEnabled reports whether ANSI color should be emitted: only on a TTY, and
// never when NO_COLOR is set or TERM=dumb.
func ColorEnabled(isTTY bool, getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	if getenv("NO_COLOR") != "" {
		return false
	}
	if getenv("TERM") == "dumb" {
		return false
	}
	return isTTY
}
