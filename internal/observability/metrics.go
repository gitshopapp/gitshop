package observability

import (
	"context"

	"github.com/getsentry/sentry-go"
)

type meterContextKey struct{}

// WithMeter returns a context carrying the provided meter.
func WithMeter(ctx context.Context, meter sentry.Meter) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if meter == nil {
		meter = sentry.NewMeter(ctx)
	}
	return context.WithValue(ctx, meterContextKey{}, meter.WithCtx(ctx))
}

// MeterFromContext returns the request-scoped meter from context or a new one.
func MeterFromContext(ctx context.Context) sentry.Meter {
	if ctx == nil {
		ctx = context.Background()
	}
	if meter, ok := ctx.Value(meterContextKey{}).(sentry.Meter); ok && meter != nil {
		return meter.WithCtx(ctx)
	}
	return sentry.NewMeter(ctx).WithCtx(ctx)
}
