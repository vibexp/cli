package cli

import (
	"context"

	"github.com/vibexp/cli/internal/config"
	"github.com/vibexp/cli/internal/logging"
)

// ctxKey is an unexported type for context keys defined in this package, to
// avoid collisions with keys from other packages.
type ctxKey int

const (
	runtimeKey ctxKey = iota
	loggerKey
)

// withRuntime returns a copy of ctx carrying the resolved runtime.
func withRuntime(ctx context.Context, rt *config.Runtime) context.Context {
	return context.WithValue(ctx, runtimeKey, rt)
}

// RuntimeFrom extracts the resolved runtime from ctx, or nil if absent.
func RuntimeFrom(ctx context.Context) *config.Runtime {
	rt, _ := ctx.Value(runtimeKey).(*config.Runtime)
	return rt
}

// withLogger returns a copy of ctx carrying the logger.
func withLogger(ctx context.Context, l *logging.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// LoggerFrom extracts the logger from ctx, or nil if absent.
func LoggerFrom(ctx context.Context) *logging.Logger {
	l, _ := ctx.Value(loggerKey).(*logging.Logger)
	return l
}
