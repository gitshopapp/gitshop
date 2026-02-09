package session

import (
	"context"
	"testing"
)

func TestNewStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{name: "default provider", provider: "", wantErr: false},
		{name: "memory provider", provider: "memory", wantErr: false},
		{name: "unsupported provider", provider: "unsupported", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, err := NewStore(context.Background(), Config{Provider: tt.provider})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if store == nil {
				t.Fatalf("expected store, got nil")
			}
			if err := store.Close(); err != nil {
				t.Fatalf("expected close without error, got %v", err)
			}
		})
	}
}
