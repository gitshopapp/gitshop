package catalog

import (
	"testing"
)

func TestPricer_ComputeSubtotal(t *testing.T) {
	config := &GitShopConfig{
		Products: []ProductConfig{
			{
				SKU:            "COFFEE_V1",
				Name:           "Coffee",
				UnitPriceCents: 1800,
				Active:         true,
				Options: []ProductOption{
					{
						Name:     "quantity",
						Required: true,
						Values:   []string{"1", "2", "3", "4", "5"},
					},
					{
						Name:     "grind",
						Required: true,
						Values:   []string{"Whole Bean", "Ground"},
					},
				},
			},
		},
	}

	tests := []struct {
		name      string
		sku       string
		options   map[string]any
		wantCents int
		wantErr   bool
	}{
		{
			name: "valid order",
			sku:  "COFFEE_V1",
			options: map[string]any{
				"quantity": 2,
				"grind":    "Whole Bean",
			},
			wantCents: 3600, // 1800 * 2
			wantErr:   false,
		},
		{
			name: "missing required option does not block pricing",
			sku:  "COFFEE_V1",
			options: map[string]any{
				"quantity": 1,
				// missing grind
			},
			wantCents: 1800,
			wantErr:   false,
		},
		{
			name: "invalid sku",
			sku:  "INVALID_SKU",
			options: map[string]any{
				"quantity": 1,
				"grind":    "Whole Bean",
			},
			wantErr: true,
		},
		{
			name: "invalid option value does not block pricing",
			sku:  "COFFEE_V1",
			options: map[string]any{
				"quantity": 1,
				"grind":    "Invalid Grind",
			},
			wantCents: 1800,
			wantErr:   false,
		},
	}

	pricer := NewPricer()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subtotal, err := pricer.ComputeSubtotal(config, tt.sku, tt.options)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if subtotal != tt.wantCents {
				t.Errorf("expected subtotal %d, got %d", tt.wantCents, subtotal)
			}
		})
	}
}
