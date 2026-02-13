package handlers

import (
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"

	"github.com/gitshopapp/gitshop/internal/cache"
	"github.com/gitshopapp/gitshop/internal/githubapp"
)

func (h *Handlers) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.loggerFromContext(ctx)
	meter := sentry.NewMeter(ctx).WithCtx(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)

	payload, err := githubapp.ReadWebhookPayload(r, h.config.GitHubWebhookSecret)
	if err != nil {
		meter.Count("webhook.failed", 1, sentry.WithAttributes(
			attribute.String("webhook.provider", "github"),
			attribute.String("webhook.reason", "invalid_payload"),
		))
		logger.Error("failed to read GitHub webhook payload", "error", err)
		http.Error(w, "Invalid webhook", http.StatusBadRequest)
		return
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if deliveryID == "" {
		meter.Count("webhook.failed", 1, sentry.WithAttributes(
			attribute.String("webhook.provider", "github"),
			attribute.String("webhook.reason", "missing_delivery_id"),
		))
		logger.Error("missing GitHub delivery ID")
		http.Error(w, "Missing delivery ID", http.StatusBadRequest)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		eventType = "unknown"
	}
	baseAttrs := []attribute.Builder{
		attribute.String("webhook.provider", "github"),
		attribute.String("webhook.event_type", eventType),
	}
	meter.Count("webhook.received", 1, sentry.WithAttributes(baseAttrs...))

	cacheKey := cache.WebhookKey("github", deliveryID)
	_, err = h.cacheProvider.Get(ctx, cacheKey)
	if err == nil {
		meter.Count("webhook.duplicate", 1, sentry.WithAttributes(baseAttrs...))
		logger.Info("webhook already processed", "delivery_id", deliveryID)
		w.WriteHeader(http.StatusOK)
		return
	}

	if eventType == "unknown" {
		meter.Count("webhook.failed", 1, sentry.WithAttributes(
			attribute.String("webhook.provider", "github"),
			attribute.String("webhook.event_type", eventType),
			attribute.String("webhook.reason", "missing_event_type"),
		))
		logger.Error("missing GitHub event type")
		http.Error(w, "Missing event type", http.StatusBadRequest)
		return
	}
	if h.githubRouter == nil {
		meter.Count("webhook.failed", 1, sentry.WithAttributes(
			attribute.String("webhook.provider", "github"),
			attribute.String("webhook.event_type", eventType),
			attribute.String("webhook.reason", "router_not_configured"),
		))
		logger.Error("github event router not configured")
		http.Error(w, "Webhook handler not configured", http.StatusInternalServerError)
		return
	}

	processErr := h.githubRouter.Handle(ctx, eventType, payload)

	if processErr == nil {
		meter.Count("webhook.processed", 1, sentry.WithAttributes(baseAttrs...))
		if err := h.cacheProvider.Set(ctx, cacheKey, "processed", 24*time.Hour); err != nil {
			logger.Error("failed to mark webhook as processed in cache", "error", err)
		}
	}

	if processErr != nil {
		meter.Count("webhook.failed", 1, sentry.WithAttributes(baseAttrs...))
		logger.Error("failed to process GitHub webhook", "error", processErr, "type", eventType)
		http.Error(w, "Processing failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
