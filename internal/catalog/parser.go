package catalog

// Package catalog provides gitshop.yaml parsing functionality.

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type GitShopConfig struct {
	Shop     ShopConfig      `yaml:"shop"`
	Products []ProductConfig `yaml:"products"`
}

type ShopConfig struct {
	Name     string         `yaml:"name"`
	Currency string         `yaml:"currency"`
	Manager  string         `yaml:"manager"`
	Shipping ShippingConfig `yaml:"shipping"`
}

type ShippingConfig struct {
	FlatRateCents int    `yaml:"flat_rate_cents"`
	Carrier       string `yaml:"carrier"`
}

type ProductConfig struct {
	SKU            string          `yaml:"sku"`
	Name           string          `yaml:"name"`
	Description    string          `yaml:"description"`
	UnitPriceCents int             `yaml:"unit_price_cents"`
	Active         bool            `yaml:"active"`
	Options        []ProductOption `yaml:"options"`
}

type ProductOption struct {
	Name     string   `yaml:"name"`
	Label    string   `yaml:"label"`
	Type     string   `yaml:"type"`
	Required bool     `yaml:"required"`
	Values   []string `yaml:"values"`
}

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(content []byte) (*GitShopConfig, error) {
	var config GitShopConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &config, nil
}

func (p *Parser) ParseFromString(content string) (*GitShopConfig, error) {
	return p.Parse([]byte(content))
}
