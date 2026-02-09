package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/catalog"
)

func TestFindTemplatePriceMismatches(t *testing.T) {
	t.Parallel()

	config := &catalog.GitShopConfig{
		Products: []catalog.ProductConfig{
			{
				SKU:            "COFFEE_BEANS",
				Name:           "Coffee Beans",
				UnitPriceCents: 2000,
				Active:         true,
			},
		},
	}

	template := `
body:
  - type: dropdown
    id: product
    attributes:
      options:
        - "Coffee Beans - $19.00 (SKU:COFFEE_BEANS)"
`

	mismatches := findTemplatePriceMismatches(template, config)
	if len(mismatches) != 1 {
		t.Fatalf("expected 1 mismatch, got %d (%v)", len(mismatches), mismatches)
	}
	if !strings.Contains(mismatches[0], "COFFEE_BEANS") {
		t.Fatalf("expected mismatch to include SKU, got %q", mismatches[0])
	}
}

func TestFindTemplateSKUs_AllowsLowercase(t *testing.T) {
	t.Parallel()

	template := `
body:
  - type: dropdown
    id: product
    attributes:
      options:
        - "GitShop Blend v2 — $16.00 (SKU:GITSHOP_BLEND_v1)"
`

	skus := findTemplateSKUs(template)
	if _, ok := skus["GITSHOP_BLEND_v1"]; !ok {
		t.Fatalf("expected lowercase SKU suffix to be preserved, got %v", skus)
	}
}

func TestFindTemplateOptionMismatches_DuplicateFieldID(t *testing.T) {
	t.Parallel()

	config := &catalog.GitShopConfig{
		Products: []catalog.ProductConfig{
			{
				SKU:            "COFFEE_BLEND_V1",
				Name:           "Coffee Blend V1",
				UnitPriceCents: 1600,
				Active:         true,
				Options: []catalog.ProductOption{
					{
						Name:     "grind",
						Label:    "Grind",
						Type:     "dropdown",
						Required: true,
						Values:   []string{"Ground", "Whole Bean"},
					},
				},
			},
		},
	}

	template := `
body:
  - type: dropdown
    id: product
    attributes:
      options:
        - "Coffee Blend V1 — $16.00 (SKU:COFFEE_BLEND_V1)"
  - type: dropdown
    id: quantity
    attributes:
      options:
        - "1"
        - "2"
        - "3"
        - "4"
        - "5"
  - type: dropdown
    id: grind
    attributes:
      label: Grind
      options:
        - Ground
        - Whole Bean
  - type: dropdown
    id: grind
    attributes:
      label: Grind
      options:
        - Ground
        - Whole Bean
`

	mismatches := findTemplateOptionMismatches(template, config)
	if len(mismatches) == 0 {
		t.Fatalf("expected mismatches, got none")
	}

	found := false
	for _, mismatch := range mismatches {
		if strings.Contains(mismatch, "duplicate option field id: grind") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected duplicate field mismatch, got %v", mismatches)
	}
}

func TestTemplateHasLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		label    string
		want     bool
	}{
		{
			name: "inline label list",
			template: `
# gitshop:order-template
labels: ["gitshop:order", "gitshop:status:pending-payment"]
`,
			label: "gitshop:order",
			want:  true,
		},
		{
			name: "block label list",
			template: `
# gitshop:order-template
labels:
  - gitshop:order
  - gitshop:status:pending-payment
`,
			label: "gitshop:order",
			want:  true,
		},
		{
			name: "label missing",
			template: `
# gitshop:order-template
labels:
  - gitshop:status:pending-payment
`,
			label: "gitshop:order",
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := templateHasLabel(tt.template, tt.label)
			if got != tt.want {
				t.Fatalf("templateHasLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAdminService_ShipOrder_InvalidInput(t *testing.T) {
	t.Parallel()

	service := &AdminService{}

	err := service.ShipOrder(t.Context(), ShipOrderInput{})
	if !errors.Is(err, ErrAdminInvalidShipmentInput) {
		t.Fatalf("expected ErrAdminInvalidShipmentInput, got %v", err)
	}
}

func TestAdminService_ShipOrder_RequiresTrackingAndCarrier(t *testing.T) {
	t.Parallel()

	service := &AdminService{}

	err := service.ShipOrder(t.Context(), ShipOrderInput{
		ShopID:  uuid.New(),
		OrderID: uuid.New(),
	})
	if !errors.Is(err, ErrAdminInvalidShipmentInput) {
		t.Fatalf("expected ErrAdminInvalidShipmentInput, got %v", err)
	}
}
