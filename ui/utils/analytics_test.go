package utils

import (
	"context"
	"testing"
)

func TestUmamiConfig_Enabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  UmamiConfig
		want bool
	}{
		{
			name: "empty config",
			cfg:  UmamiConfig{},
			want: false,
		},
		{
			name: "missing script url",
			cfg:  UmamiConfig{WebsiteID: "abc-123"},
			want: false,
		},
		{
			name: "missing website id",
			cfg:  UmamiConfig{ScriptURL: "https://cloud.umami.is/script.js"},
			want: false,
		},
		{
			name: "valid config",
			cfg:  UmamiConfig{WebsiteID: "abc-123", ScriptURL: "https://cloud.umami.is/script.js"},
			want: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cfg.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUmamiFromContext(t *testing.T) {
	t.Parallel()

	var nilCtx context.Context
	if got := UmamiFromContext(nilCtx); got.Enabled() {
		t.Errorf("expected empty config from nil context, got %v", got)
	}

	ctx := context.Background()
	if got := UmamiFromContext(ctx); got.Enabled() {
		t.Errorf("expected empty config from empty context, got %v", got)
	}

	cfg := UmamiConfig{WebsiteID: "abc-123", ScriptURL: "https://cloud.umami.is/script.js", HostURL: "/um"}
	ctxWithCfg := WithUmamiConfig(ctx, cfg)
	if got := UmamiFromContext(ctxWithCfg); got != cfg {
		t.Errorf("UmamiFromContext() = %v, want %v", got, cfg)
	}
}
