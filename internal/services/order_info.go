package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/email"
)

// OrderInfoOverrides provides optional overrides when building order email data.
type OrderInfoOverrides struct {
	CustomerName    string
	CustomerEmail   string
	ShippingAddress string
	TrackingNumber  string
	TrackingURL     string
	TrackingCarrier string
	OrderDate       time.Time
}

// BuildOrderInfo builds a consistent OrderInfo payload for email templates.
func BuildOrderInfo(shop *db.Shop, order *db.Order, overrides OrderInfoOverrides) *email.OrderInfo {
	customerName := strings.TrimSpace(overrides.CustomerName)
	if customerName == "" && order != nil {
		customerName = strings.TrimSpace(order.CustomerName)
	}
	if customerName == "" && order != nil {
		customerName = strings.TrimSpace(order.GitHubUsername)
	}

	customerEmail := strings.TrimSpace(overrides.CustomerEmail)
	if customerEmail == "" && order != nil {
		customerEmail = strings.TrimSpace(order.CustomerEmail)
	}

	shippingAddress := strings.TrimSpace(overrides.ShippingAddress)
	if shippingAddress == "" && order != nil && order.ShippingAddress != nil {
		shippingAddress = formatMap(order.ShippingAddress)
	}

	orderDate := overrides.OrderDate
	if orderDate.IsZero() {
		orderDate = time.Now()
	}

	quantity := orderQuantity(nil)
	if order != nil {
		quantity = orderQuantity(order.Options)
	}

	unitPriceCents := 0
	if order != nil {
		unitPriceCents = order.SubtotalCents
		if quantity > 0 {
			unitPriceCents = order.SubtotalCents / quantity
		}
	}

	subtotal := 0
	shipping := 0
	total := 0
	sku := ""
	if order != nil {
		subtotal = order.SubtotalCents
		shipping = order.ShippingCents
		total = order.TotalCents
		sku = order.SKU
	}

	shopName := ""
	shopURL := ""
	if shop != nil {
		shopName = shop.GitHubRepoFullName
		if shopName != "" {
			shopURL = fmt.Sprintf("https://github.com/%s", shopName)
		}
	}

	orderNumber := 0
	options := map[string]any(nil)
	if order != nil {
		orderNumber = order.OrderNumber
		options = order.Options
	}

	return &email.OrderInfo{
		OrderNumber:         fmt.Sprintf("#%d", orderNumber),
		IssueURL:            issueURL(order),
		CustomerName:        customerName,
		CustomerEmail:       customerEmail,
		ShopName:            shopName,
		ShopURL:             shopURL,
		ProductName:         sku,
		Quantity:            quantity,
		UnitPrice:           formatPrice(unitPriceCents),
		TotalPrice:          formatPrice(total),
		ShippingAddress:     shippingAddress,
		ShippingAddressHTML: strings.ReplaceAll(shippingAddress, "\n", "<br>"),
		TrackingNumber:      overrides.TrackingNumber,
		TrackingURL:         overrides.TrackingURL,
		TrackingCarrier:     overrides.TrackingCarrier,
		OrderDate:           orderDate.Format("January 2, 2006"),
		Subtotal:            formatPrice(subtotal),
		Shipping:            formatPrice(shipping),
		Tax:                 "$0.00",
		Total:               formatPrice(total),
		Items: []email.OrderItem{
			{
				Name:       sku,
				SKU:        sku,
				Quantity:   quantity,
				UnitPrice:  formatPrice(unitPriceCents),
				TotalPrice: formatPrice(subtotal),
				Options:    formatMap(options),
			},
		},
	}
}

func formatPrice(cents int) string {
	dollars := float64(cents) / 100.0
	return fmt.Sprintf("$%.2f", dollars)
}

func formatMap(m map[string]any) string {
	if m == nil {
		return ""
	}
	if address := formatAddressMap(m); address != "" {
		return address
	}
	parts := make([]string, 0, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		parts = append(parts, fmt.Sprintf("%s: %v", k, v))
	}
	return strings.Join(parts, ", ")
}

func orderQuantity(options map[string]any) int {
	if options == nil {
		return 1
	}
	if qty, exists := options["quantity"]; exists {
		switch v := qty.(type) {
		case int:
			if v > 0 {
				return v
			}
		case int64:
			if v > 0 && v <= int64(^uint(0)>>1) {
				return int(v)
			}
		case float64:
			if v > 0 && v == float64(int(v)) {
				return int(v)
			}
		case string:
			if parsed := parseQuantity(v); parsed > 0 {
				return parsed
			}
		}
	}
	return 1
}

func issueURL(order *db.Order) string {
	if order == nil {
		return ""
	}
	return strings.TrimSpace(order.GitHubIssueURL)
}

func formatAddressMap(m map[string]any) string {
	var address struct {
		Line1      string `json:"line1"`
		Line2      string `json:"line2"`
		City       string `json:"city"`
		State      string `json:"state"`
		PostalCode string `json:"postal_code"`
		Country    string `json:"country"`
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	if err := json.Unmarshal(payload, &address); err != nil {
		return ""
	}

	if strings.TrimSpace(address.Line1) == "" {
		return ""
	}

	lines := []string{strings.TrimSpace(address.Line1)}
	if strings.TrimSpace(address.Line2) != "" {
		lines = append(lines, strings.TrimSpace(address.Line2))
	}

	cityStatePostal := strings.TrimSpace(strings.TrimSpace(address.City) + ", " + strings.TrimSpace(address.State) + " " + strings.TrimSpace(address.PostalCode))
	cityStatePostal = strings.Trim(cityStatePostal, ", ")
	if cityStatePostal != "" {
		lines = append(lines, cityStatePostal)
	}

	if strings.TrimSpace(address.Country) != "" {
		lines = append(lines, strings.TrimSpace(address.Country))
	}

	return strings.Join(lines, "\n")
}
