package catalog

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSyncTemplateContent_RemovesStaleAndDuplicateOptions(t *testing.T) {
	t.Parallel()

	syncer := NewTemplateSyncer(nil)
	config := &GitShopConfig{
		Products: []ProductConfig{
			{
				SKU:            "COFFEE_BLEND_V1",
				Name:           "Coffee Blend V1",
				UnitPriceCents: 1600,
				Active:         true,
				Options: []ProductOption{
					{
						Name:     "grind",
						Label:    "Grind",
						Type:     "dropdown",
						Required: true,
						Values:   []string{"Ground", "Whole Bean"},
					},
					{
						Name:     "roast",
						Label:    "Roast",
						Type:     "dropdown",
						Required: true,
						Values:   []string{"Light", "Medium", "Dark"},
					},
				},
			},
		},
	}

	existing := `# gitshop:order-template
name: "Custom Store Order"
description: Keep this description
title: "[ORDER] "
labels: ["gitshop:order", "gitshop:status:pending-payment"]
body:
  - type: markdown
    attributes:
      value: "Welcome"
  - type: dropdown
    id: product
    attributes:
      label: Product
      options:
        - "Coffee Blend V1 - $16.00 (SKU:COFFEE_BLEND_V1)"
    validations:
      required: true
  - type: dropdown
    id: quantity
    attributes:
      label: Quantity
      options: ["1", "2", "3", "4", "5"]
    validations:
      required: true
  - type: dropdown
    id: size
    attributes:
      label: Size
      options: ["S", "M", "L"]
    validations:
      required: true
  - type: dropdown
    id: grind
    attributes:
      label: Grind
      options: ["Ground", "Whole Bean"]
    validations:
      required: true
  - type: markdown
    attributes:
      value: "Note block should remain"
  - type: dropdown
    id: grind
    attributes:
      label: Grind
      options: ["Ground", "Whole Bean"]
    validations:
      required: true
  - type: dropdown
    id: gift_wrap
    attributes:
      label: Gift Wrap
      options: ["No", "Yes"]
    validations:
      required: false
`

	synced, err := syncer.SyncTemplateContent(existing, config)
	if err != nil {
		t.Fatalf("SyncTemplateContent returned error: %v", err)
	}
	if synced == existing {
		t.Fatalf("expected synced template content to change")
	}

	if strings.Count(synced, "id: grind") != 1 {
		t.Fatalf("expected exactly one grind field, got %d", strings.Count(synced, "id: grind"))
	}
	if strings.Contains(synced, "id: size") {
		t.Fatalf("expected stale size option to be removed")
	}
	if !strings.Contains(synced, "id: roast") {
		t.Fatalf("expected new roast option to be added")
	}
	if !strings.Contains(synced, "id: gift_wrap") {
		t.Fatalf("expected custom non-managed field to remain")
	}
	var parsed struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(synced), &parsed); err != nil {
		t.Fatalf("failed to parse synced template YAML: %v", err)
	}
	if parsed.Name != "Custom Store Order" {
		t.Fatalf("expected top-level name to remain unchanged, got %q", parsed.Name)
	}
}

func TestBuildTemplateContent_GeneratesYAML(t *testing.T) {
	t.Parallel()

	syncer := NewTemplateSyncer(nil)
	config := &GitShopConfig{
		Products: []ProductConfig{
			{
				SKU:            "COFFEE_BLEND_V1",
				Name:           "Coffee Blend V1",
				UnitPriceCents: 1600,
				Active:         true,
				Options: []ProductOption{
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

	template, err := syncer.BuildTemplateContent(config)
	if err != nil {
		t.Fatalf("BuildTemplateContent returned error: %v", err)
	}
	if !strings.HasPrefix(template, "# gitshop:order-template") {
		t.Fatalf("expected marker prefix")
	}
	var parsed struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal([]byte(strings.TrimPrefix(template, "# gitshop:order-template\n")), &parsed); err != nil {
		t.Fatalf("failed to parse generated template YAML: %v", err)
	}
	if parsed.Name == "" {
		t.Fatalf("expected template name to be set")
	}
}

func TestExtractProductSKUsFromTemplateBody_AllowsLowercase(t *testing.T) {
	t.Parallel()

	template := `body:
  - type: dropdown
    id: product
    attributes:
      options:
        - "GitShop Blend v2 — $16.00 (SKU:GITSHOP_BLEND_v1)"`

	var parsed struct {
		Body yaml.Node `yaml:"body"`
	}
	if err := yaml.Unmarshal([]byte(template), &parsed); err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	skus := extractProductSKUsFromTemplateBody(&parsed.Body)
	if len(skus) != 1 || skus[0] != "GITSHOP_BLEND_v1" {
		t.Fatalf("unexpected skus: %v", skus)
	}
}
