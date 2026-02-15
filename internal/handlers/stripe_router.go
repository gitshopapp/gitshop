package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	stripeapi "github.com/stripe/stripe-go/v84"

	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/observability"
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
	span := sentry.StartSpan(
		ctx,
		"handler.stripe_router.handle",
		sentry.WithOpName("handler.stripe_router"),
		sentry.WithDescription("StripeEventRouter.Handle"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := observability.MeterFromContext(ctx)
	meter.SetAttributes(attribute.String("webhook.provider", "stripe"))
	meter.Count("webhook.router.received", 1)
	recordFailed := func(reason string) {
		meter.Count("webhook.router.failed", 1, sentry.WithAttributes(attribute.String("reason", reason)))
	}

	if event == nil {
		recordFailed("missing_event")
		return fmt.Errorf("missing stripe event")
	}
	if event.Data == nil {
		recordFailed("missing_event_data")
		return fmt.Errorf("missing stripe event data")
	}
	meter.SetAttributes(attribute.String("webhook.event_type", string(event.Type)))

	logger := logging.FromContext(ctx, r.logger)
	payload := event.Data.Raw

	switch event.Type {
	case "checkout.session.completed":
		if err := r.service.HandleCheckoutSessionCompleted(ctx, payload); err != nil {
			recordFailed("checkout_session_completed_failed")
			return err
		}
		meter.Count("webhook.router.processed", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	case "checkout.session.expired":
		if err := r.service.HandleCheckoutSessionExpired(ctx, payload); err != nil {
			recordFailed("checkout_session_expired_failed")
			return err
		}
		meter.Count("webhook.router.processed", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	case "payment_intent.payment_failed":
		if err := r.service.HandlePaymentIntentFailed(ctx, payload); err != nil {
			recordFailed("payment_intent_failed_handler_failed")
			return err
		}
		meter.Count("webhook.router.processed", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	default:
		logger.Info("unhandled Stripe event type", "type", event.Type)
		meter.Count("webhook.router.unhandled", 1)
		span.Status = sentry.SpanStatusOK
		return nil
	}
}
