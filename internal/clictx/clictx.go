// Package clictx carries the resolved runtime and logger on a context.Context
// so command packages can read them without importing the root cli package
// (which would create an import cycle). The root command's persistent pre-run
// populates these; command RunE functions read them.
package clictx

import (
	"context"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/logging"
)

// ctxKey is an unexported key type to avoid collisions across packages.
type ctxKey int

const (
	runtimeKey ctxKey = iota
	loggerKey
)

// WithRuntime returns a copy of ctx carrying the resolved runtime.
func WithRuntime(ctx context.Context, rt *config.Runtime) context.Context {
	return context.WithValue(ctx, runtimeKey, rt)
}

// Runtime extracts the resolved runtime from ctx, or nil if absent.
func Runtime(ctx context.Context) *config.Runtime {
	rt, _ := ctx.Value(runtimeKey).(*config.Runtime)
	return rt
}

// WithLogger returns a copy of ctx carrying the logger.
func WithLogger(ctx context.Context, l *logging.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// Logger extracts the logger from ctx, or nil if absent.
func Logger(ctx context.Context) *logging.Logger {
	l, _ := ctx.Value(loggerKey).(*logging.Logger)
	return l
}
