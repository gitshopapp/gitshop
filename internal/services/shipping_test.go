package services

import "testing"

func TestResolveShippingCarrier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		provider     string
		carrier      string
		otherCarrier string
		want         string
	}{
		{
			name:     "known provider usps",
			provider: "usps",
			want:     "USPS",
		},
		{
			name:     "known provider fedex",
			provider: "FedEx",
			want:     "FedEx",
		},
		{
			name:     "known provider ups",
			provider: "UPS",
			want:     "UPS",
		},
		{
			name:         "other provider uses custom value",
			provider:     "other",
			otherCarrier: "DHL",
			want:         "DHL",
		},
		{
			name:    "legacy carrier fallback normalizes known names",
			carrier: "fedex",
			want:    "FedEx",
		},
		{
			name:    "legacy carrier fallback keeps custom names",
			carrier: "OnTrac",
			want:    "OnTrac",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveShippingCarrier(tc.provider, tc.carrier, tc.otherCarrier)
			if got != tc.want {
				t.Fatalf("ResolveShippingCarrier() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildTrackingURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		carrier        string
		trackingNumber string
		want           string
	}{
		{
			name:           "usps url",
			carrier:        "USPS",
			trackingNumber: "9400111899223856925034",
			want:           "https://tools.usps.com/go/TrackConfirmAction?tLabels=9400111899223856925034",
		},
		{
			name:           "fedex url",
			carrier:        "FedEx",
			trackingNumber: "123456789012",
			want:           "https://www.fedex.com/fedextrack/?trknbr=123456789012",
		},
		{
			name:           "ups url",
			carrier:        "ups",
			trackingNumber: "1Z999AA10123456784",
			want:           "https://www.ups.com/track?tracknum=1Z999AA10123456784",
		},
		{
			name:           "other carrier has no url",
			carrier:        "DHL",
			trackingNumber: "12345",
			want:           "",
		},
		{
			name:           "empty tracking number has no url",
			carrier:        "USPS",
			trackingNumber: "",
			want:           "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := BuildTrackingURL(tc.carrier, tc.trackingNumber)
			if got != tc.want {
				t.Fatalf("BuildTrackingURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
