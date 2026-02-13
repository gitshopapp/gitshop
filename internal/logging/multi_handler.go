package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
)

// MultiHandler fans out slog records to multiple handlers.
func MultiHandler(handlers ...slog.Handler) slog.Handler {
	filtered := make([]slog.Handler, 0, len(handlers))
	for _, handler := range handlers {
		if handler != nil {
			filtered = append(filtered, handler)
		}
	}
	if len(filtered) == 0 {
		return slog.NewTextHandler(io.Discard, nil)
	}
	return multiHandler(filtered)
}

type multiHandler []slog.Handler

func (h multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var handleErr error
	for _, handler := range h {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}
		handleErr = errors.Join(handleErr, handler.Handle(ctx, record))
	}
	return handleErr
}

func (h multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, 0, len(h))
	for _, handler := range h {
		next = append(next, handler.WithAttrs(attrs))
	}
	return multiHandler(next)
}

func (h multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, 0, len(h))
	for _, handler := range h {
		next = append(next, handler.WithGroup(name))
	}
	return multiHandler(next)
}
