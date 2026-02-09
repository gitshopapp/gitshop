package handlers

import (
	"net/http"
	"time"

	"github.com/gitshopapp/gitshop/internal/cache"
	stripewebhook "github.com/gitshopapp/gitshop/internal/stripe"
)

// stripeWebhookIdempotencyTTL is how long webhook event IDs are kept for deduplication
const stripeWebhookIdempotencyTTL = 24 * time.Hour

func (h *Handlers) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.loggerFromContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)

	event, err := stripewebhook.ReadWebhookEvent(r, h.config.StripeWebhookSecret)
	if err != nil {
		logger.Error("failed to read Stripe webhook payload", "error", err)
		http.Error(w, "Invalid webhook", http.StatusBadRequest)
		return
	}

	if event == nil || event.ID == "" {
		logger.Error("missing Stripe event ID")
		http.Error(w, "Missing event ID", http.StatusBadRequest)
		return
	}

	cacheKey := cache.WebhookKey("stripe", event.ID)
	_, err = h.cacheProvider.Get(ctx, cacheKey)
	if err == nil {
		logger.Info("webhook already processed", "event_id", event.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if h.stripeRouter == nil {
		logger.Error("stripe event router not configured")
		http.Error(w, "Webhook handler not configured", http.StatusInternalServerError)
		return
	}

	processErr := h.stripeRouter.Handle(ctx, event)
	if processErr == nil {
		if err := h.cacheProvider.Set(ctx, cacheKey, "processed", stripeWebhookIdempotencyTTL); err != nil {
			logger.Error("failed to mark webhook as processed in cache", "error", err)
		}
	}
	if processErr != nil {
		logger.Error("failed to process Stripe webhook", "error", processErr, "type", event.Type)
		http.Error(w, "Processing failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
