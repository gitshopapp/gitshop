package utils

import "context"

// UmamiConfig holds configuration for Umami analytics.
type UmamiConfig struct {
	WebsiteID string
	ScriptURL string
}

// Enabled reports whether Umami analytics is configured with a valid website ID and script URL.
func (u UmamiConfig) Enabled() bool {
	return u.WebsiteID != "" && u.ScriptURL != ""
}

type umamiContextKey struct{}

// WithUmamiConfig returns a new context containing the provided UmamiConfig.
func WithUmamiConfig(ctx context.Context, cfg UmamiConfig) context.Context {
	return context.WithValue(ctx, umamiContextKey{}, cfg)
}

// UmamiFromContext extracts UmamiConfig from ctx, or returns an empty UmamiConfig if none is present.
func UmamiFromContext(ctx context.Context) UmamiConfig {
	if ctx == nil {
		return UmamiConfig{}
	}
	if cfg, ok := ctx.Value(umamiContextKey{}).(UmamiConfig); ok {
		return cfg
	}
	return UmamiConfig{}
}
