package catalog

// Package catalog provides configuration validation.

import (
	"fmt"
	"regexp"
	"strings"
)

type Validator struct{}

func NewValidator() *Validator {
	return &Validator{}
}

var githubUsernameRegex = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)

// IsValidGitHubUsername validates a GitHub username format (1-39 chars, no leading/trailing hyphen).
func IsValidGitHubUsername(username string) bool {
	return githubUsernameRegex.MatchString(username)
}

func (v *Validator) Validate(config *GitShopConfig) error {
	if err := v.validateShop(&config.Shop); err != nil {
		return fmt.Errorf("shop validation failed: %w", err)
	}

	if len(config.Products) == 0 {
		return fmt.Errorf("at least one product is required")
	}

	skus := make(map[string]bool)
	for i, product := range config.Products {
		if err := v.validateProduct(&product); err != nil {
			return fmt.Errorf("product %d validation failed: %w", i, err)
		}

		if skus[product.SKU] {
			return fmt.Errorf("duplicate SKU: %s", product.SKU)
		}
		skus[product.SKU] = true
	}

	return nil
}

func (v *Validator) validateShop(shop *ShopConfig) error {
	if strings.TrimSpace(shop.Name) == "" {
		return fmt.Errorf("shop name is required")
	}

	if shop.Currency != "usd" {
		return fmt.Errorf("only USD currency is supported")
	}

	manager := strings.TrimSpace(shop.Manager)
	if manager != "" && !IsValidGitHubUsername(manager) {
		return fmt.Errorf("shop manager must be a valid GitHub username")
	}

	if shop.Shipping.FlatRateCents < 0 {
		return fmt.Errorf("shipping flat rate must be zero or positive")
	}

	if strings.TrimSpace(shop.Shipping.Carrier) == "" {
		return fmt.Errorf("shipping carrier is required")
	}

	return nil
}

func (v *Validator) validateProduct(product *ProductConfig) error {
	if strings.TrimSpace(product.SKU) == "" {
		return fmt.Errorf("product SKU is required")
	}

	if strings.TrimSpace(product.Name) == "" {
		return fmt.Errorf("product name is required")
	}

	if product.UnitPriceCents <= 0 {
		return fmt.Errorf("product unit price must be positive")
	}

	optionNames := make(map[string]bool)
	for i, option := range product.Options {
		if err := v.validateOption(&option); err != nil {
			return fmt.Errorf("option %d validation failed: %w", i, err)
		}

		if optionNames[option.Name] {
			return fmt.Errorf("duplicate option name: %s", option.Name)
		}
		optionNames[option.Name] = true
	}

	return nil
}

func (v *Validator) validateOption(option *ProductOption) error {
	if strings.TrimSpace(option.Name) == "" {
		return fmt.Errorf("option name is required")
	}

	if strings.TrimSpace(option.Label) == "" {
		return fmt.Errorf("option label is required")
	}

	if option.Type != "dropdown" && option.Type != "text" {
		return fmt.Errorf("only dropdown or text option types are supported")
	}

	if option.Type == "dropdown" && option.Values == nil {
		return fmt.Errorf("option values are required for dropdown options")
	}

	if option.Type == "dropdown" {
		if len(option.Values) == 0 {
			return fmt.Errorf("option values cannot be empty")
		}
	}

	return nil
}
