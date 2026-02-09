package logging

import (
	"context"
	"io"
	"log/slog"
)

type contextKey string

const loggerKey contextKey = "logger"

// WithLogger returns a context that carries the provided logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, loggerKey, ensureLogger(logger))
}

// FromContext returns the logger stored in context or the fallback logger.
// If neither is available, it returns a no-op logger.
func FromContext(ctx context.Context, fallback *slog.Logger) *slog.Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok && logger != nil {
			return logger
		}
	}
	return ensureLogger(fallback)
}

func ensureLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
