// Package stripe provides Stripe webhook validation.
package stripe

import (
	"fmt"
	"io"
	"net/http"

	stripeapi "github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"
)

func ReadWebhookEvent(r *http.Request, secret string) (*stripeapi.Event, error) {
	signature := r.Header.Get("Stripe-Signature")
	if signature == "" {
		return nil, fmt.Errorf("missing stripe signature header")
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	event, err := webhook.ConstructEvent(payload, signature, secret)
	if err != nil {
		return nil, fmt.Errorf("webhook signature validation failed: %w", err)
	}

	return &event, nil
}
