package catalog

// Package catalog provides price calculation functionality.

import (
	"fmt"
	"strconv"
	"strings"
)

type Pricer struct{}

func NewPricer() *Pricer {
	return &Pricer{}
}

func (p *Pricer) ComputeSubtotal(config *GitShopConfig, sku string, options map[string]any) (int, error) {
	product := p.findProduct(config, sku)
	if product == nil {
		return 0, fmt.Errorf("product with SKU %s not found", sku)
	}

	if !product.Active {
		return 0, fmt.Errorf("product with SKU %s is not active", sku)
	}

	quantity := p.getQuantity(options)
	return product.UnitPriceCents * quantity, nil
}

func (p *Pricer) GetShippingCents(config *GitShopConfig) int {
	return config.Shop.Shipping.FlatRateCents
}

func (p *Pricer) findProduct(config *GitShopConfig, sku string) *ProductConfig {
	for _, product := range config.Products {
		if product.SKU == sku {
			return &product
		}
	}
	return nil
}

func (p *Pricer) getQuantity(options map[string]any) int {
	if qty, exists := options["quantity"]; exists {
		switch v := qty.(type) {
		case int:
			if v > 0 {
				return v
			}
		case string:
			if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && i > 0 {
				return i
			}
		case float64:
			if v > 0 && v == float64(int(v)) {
				return int(v)
			}
		}
	}

	return 1 // Default quantity
}
