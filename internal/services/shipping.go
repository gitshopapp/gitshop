package services

import (
	"net/url"
	"strings"
)

const (
	ShippingProviderUSPS  = "usps"
	ShippingProviderFedEx = "fedex"
	ShippingProviderUPS   = "ups"
	ShippingProviderOther = "other"
)

// NormalizeShippingProvider returns a canonical provider key for known carriers.
func NormalizeShippingProvider(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "")
	normalized = replacer.Replace(normalized)

	switch normalized {
	case "usps", "unitedstatespostalservice":
		return ShippingProviderUSPS
	case "fedex", "federalexpress":
		return ShippingProviderFedEx
	case "ups", "unitedparcelservice":
		return ShippingProviderUPS
	case "other":
		return ShippingProviderOther
	default:
		return ""
	}
}

// CanonicalCarrierName maps a provider key to the display name.
func CanonicalCarrierName(provider string) string {
	switch NormalizeShippingProvider(provider) {
	case ShippingProviderUSPS:
		return "USPS"
	case ShippingProviderFedEx:
		return "FedEx"
	case ShippingProviderUPS:
		return "UPS"
	default:
		return ""
	}
}

// NormalizeCarrierName keeps custom carriers untouched and normalizes known ones.
func NormalizeCarrierName(carrier string) string {
	trimmed := strings.TrimSpace(carrier)
	if trimmed == "" {
		return ""
	}
	if canonical := CanonicalCarrierName(trimmed); canonical != "" {
		return canonical
	}
	return trimmed
}

// ResolveShippingCarrier selects the final carrier from provider + form values.
func ResolveShippingCarrier(provider, carrier, otherCarrier string) string {
	switch NormalizeShippingProvider(provider) {
	case ShippingProviderUSPS:
		return CanonicalCarrierName(ShippingProviderUSPS)
	case ShippingProviderFedEx:
		return CanonicalCarrierName(ShippingProviderFedEx)
	case ShippingProviderUPS:
		return CanonicalCarrierName(ShippingProviderUPS)
	case ShippingProviderOther:
		return strings.TrimSpace(otherCarrier)
	default:
		return NormalizeCarrierName(carrier)
	}
}

// BuildTrackingURL returns a provider-specific tracking URL. Unknown providers return empty.
func BuildTrackingURL(carrier, trackingNumber string) string {
	number := strings.TrimSpace(trackingNumber)
	if number == "" {
		return ""
	}

	escaped := url.QueryEscape(number)
	switch NormalizeShippingProvider(carrier) {
	case ShippingProviderUSPS:
		return "https://tools.usps.com/go/TrackConfirmAction?tLabels=" + escaped
	case ShippingProviderFedEx:
		return "https://www.fedex.com/fedextrack/?trknbr=" + escaped
	case ShippingProviderUPS:
		return "https://www.ups.com/track?tracknum=" + escaped
	default:
		return ""
	}
}
