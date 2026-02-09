package catalog

import "testing"

func TestValidator_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  *GitShopConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &GitShopConfig{
				Shop: ShopConfig{
					Name:     "Test Shop",
					Currency: "usd",
					Shipping: ShippingConfig{FlatRateCents: 500, Carrier: "USPS"},
				},
				Products: []ProductConfig{
					{
						SKU:            "COFFEE_V1",
						Name:           "Coffee",
						UnitPriceCents: 1500,
						Active:         true,
						Options: []ProductOption{
							{Name: "quantity", Label: "Quantity", Type: "dropdown", Required: true, Values: []string{"1", "2"}},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "dropdown option values required",
			config: &GitShopConfig{
				Shop: ShopConfig{
					Name:     "Test Shop",
					Currency: "usd",
					Shipping: ShippingConfig{FlatRateCents: 500, Carrier: "USPS"},
				},
				Products: []ProductConfig{
					{
						SKU:            "COFFEE_V1",
						Name:           "Coffee",
						UnitPriceCents: 1500,
						Active:         true,
						Options: []ProductOption{
							{Name: "quantity", Label: "Quantity", Type: "dropdown", Required: true, Values: nil},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid manager",
			config: &GitShopConfig{
				Shop: ShopConfig{
					Name:     "Test Shop",
					Currency: "usd",
					Manager:  "-bad",
					Shipping: ShippingConfig{FlatRateCents: 500, Carrier: "USPS"},
				},
				Products: []ProductConfig{
					{
						SKU:            "COFFEE_V1",
						Name:           "Coffee",
						UnitPriceCents: 1500,
						Active:         true,
					},
				},
			},
			wantErr: true,
		},
	}

	validator := NewValidator()
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validator.Validate(tc.config)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
