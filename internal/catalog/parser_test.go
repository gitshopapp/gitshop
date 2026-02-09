package catalog

import (
	"testing"
)

func TestParser_Parse(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "valid config",
			yaml: `
shop:
  name: "Test Shop"
  currency: "usd"
  shipping:
    flat_rate_cents: 900
    carrier: "USPS"
products:
  - sku: "TEST_V1"
    name: "Test Product"
    description: "A test product"
    unit_price_cents: 1000
    active: true
    options:
      - name: "quantity"
        label: "Quantity"
        type: "dropdown"
        required: true
        values: [1, 2, 3]
`,
			wantErr: false,
		},
		{
			name:    "invalid yaml",
			yaml:    "invalid: yaml: content:",
			wantErr: true,
		},
	}

	parser := NewParser()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parser.ParseFromString(tt.yaml)

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

			if config == nil {
				t.Error("expected config but got nil")
				return
			}

			if config.Shop.Name != "Test Shop" {
				t.Errorf("expected shop name 'Test Shop', got '%s'", config.Shop.Name)
			}

			if len(config.Products) != 1 {
				t.Errorf("expected 1 product, got %d", len(config.Products))
			}
		})
	}
}
