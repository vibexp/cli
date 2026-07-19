// Package logging provides the always-on, structured JSON-lines file logger for
// the CLI, with size-based rotation and a redaction layer so credentials can
// never reach the log. stdout is reserved for data; all logging goes to the
// file (and, with --debug, mirrored to stderr).
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// Rotation policy (matches epic #2): 5MB per file, 3 rotated backups.
const (
	maxSizeMB  = 5
	maxBackups = 3
)

// Options configures the logger.
type Options struct {
	// Dir is the directory to write cli.log into. Empty means the default
	// (~/.vibexp/logs).
	Dir string
	// Debug raises the level to Debug and mirrors output to stderr.
	Debug bool
}

// Logger is the initialized logger plus a Close to flush/close the sink.
type Logger struct {
	*slog.Logger
	closer io.Closer
}

// Close releases the underlying log file.
func (l *Logger) Close() error {
	if l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// DefaultDir returns the default log directory (~/.vibexp/logs).
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".vibexp", "logs"), nil
}

// newRotator builds the size-rotating file sink. Exposed for tests to assert
// the rotation policy.
func newRotator(dir string) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   filepath.Join(dir, "cli.log"),
		MaxSize:    maxSizeMB,
		MaxBackups: maxBackups,
		Compress:   false,
	}
}

// Init builds the always-on logger. It never fails hard on a missing home dir;
// callers that cannot open the log file get a stderr-only logger so the CLI
// still runs.
func Init(opts Options) (*Logger, error) {
	dir := opts.Dir
	if dir == "" {
		d, err := DefaultDir()
		if err != nil {
			return fallback(opts.Debug), nil
		}
		dir = d
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fallback(opts.Debug), nil
	}

	rot := newRotator(dir)
	var sink io.Writer = rot
	if opts.Debug {
		sink = io.MultiWriter(rot, os.Stderr)
	}

	level := slog.LevelInfo
	if opts.Debug {
		level = slog.LevelDebug
	}
	handler := slog.NewJSONHandler(sink, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactAttr,
	})
	return &Logger{Logger: slog.New(handler), closer: rot}, nil
}

// fallback returns a stderr-only logger used when the log file can't be opened.
func fallback(debug bool) *Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactAttr,
	})
	return &Logger{Logger: slog.New(handler)}
}
