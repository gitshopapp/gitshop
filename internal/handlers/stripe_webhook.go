package handlers

import (
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"

	"github.com/gitshopapp/gitshop/internal/cache"
	"github.com/gitshopapp/gitshop/internal/observability"
	stripewebhook "github.com/gitshopapp/gitshop/internal/stripe"
)

// stripeWebhookIdempotencyTTL is how long webhook event IDs are kept for deduplication
const stripeWebhookIdempotencyTTL = 24 * time.Hour

func (h *Handlers) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.loggerFromContext(ctx)
	meter := observability.MeterFromContext(ctx)
	meter.SetAttributes(attribute.String("webhook.provider", "stripe"))
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)

	event, err := stripewebhook.ReadWebhookEvent(r, h.config.StripeWebhookSecret)
	if err != nil {
		meter.Count("webhook.failed", 1, sentry.WithAttributes(
			attribute.String("webhook.reason", "invalid_payload"),
		))
		logger.Error("failed to read Stripe webhook payload", "error", err)
		http.Error(w, "Invalid webhook", http.StatusBadRequest)
		return
	}

	if event == nil || event.ID == "" {
		meter.Count("webhook.failed", 1, sentry.WithAttributes(
			attribute.String("webhook.reason", "missing_event_id"),
		))
		logger.Error("missing Stripe event ID")
		http.Error(w, "Missing event ID", http.StatusBadRequest)
		return
	}

	eventType := string(event.Type)
	if eventType == "" {
		eventType = "unknown"
	}
	meter.SetAttributes(attribute.String("webhook.event_type", eventType))
	meter.Count("webhook.received", 1)

	cacheKey := cache.WebhookKey("stripe", event.ID)
	_, err = h.cacheProvider.Get(ctx, cacheKey)
	if err == nil {
		meter.Count("webhook.duplicate", 1)
		logger.Info("webhook already processed", "event_id", event.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if h.stripeRouter == nil {
		meter.Count("webhook.failed", 1, sentry.WithAttributes(
			attribute.String("webhook.reason", "router_not_configured"),
		))
		logger.Error("stripe event router not configured")
		http.Error(w, "Webhook handler not configured", http.StatusInternalServerError)
		return
	}

	processErr := h.stripeRouter.Handle(ctx, event)
	if processErr == nil {
		meter.Count("webhook.processed", 1)
		if err := h.cacheProvider.Set(ctx, cacheKey, "processed", stripeWebhookIdempotencyTTL); err != nil {
			logger.Error("failed to mark webhook as processed in cache", "error", err)
		}
	}
	if processErr != nil {
		meter.Count("webhook.failed", 1)
		logger.Error("failed to process Stripe webhook", "error", processErr, "type", event.Type)
		http.Error(w, "Processing failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
