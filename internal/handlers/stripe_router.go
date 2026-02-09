package handlers

import (
	"context"
	"fmt"
	"log/slog"

	stripeapi "github.com/stripe/stripe-go/v84"

	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/services"
)

type StripeEventRouter struct {
	service *services.StripeService
	logger  *slog.Logger
}

func NewStripeEventRouter(service *services.StripeService, logger *slog.Logger) *StripeEventRouter {
	return &StripeEventRouter{
		service: service,
		logger:  logger,
	}
}

func (r *StripeEventRouter) Handle(ctx context.Context, event *stripeapi.Event) error {
	if event == nil {
		return fmt.Errorf("missing stripe event")
	}
	if event.Data == nil {
		return fmt.Errorf("missing stripe event data")
	}

	logger := logging.FromContext(ctx, r.logger)
	payload := event.Data.Raw

	switch event.Type {
	case "checkout.session.completed":
		return r.service.HandleCheckoutSessionCompleted(ctx, payload)
	case "checkout.session.expired":
		return r.service.HandleCheckoutSessionExpired(ctx, payload)
	case "payment_intent.payment_failed":
		return r.service.HandlePaymentIntentFailed(ctx, payload)
	default:
		logger.Info("unhandled Stripe event type", "type", event.Type)
		return nil
	}
}
