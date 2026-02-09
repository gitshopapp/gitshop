package handlers

import (
	"net/http"
	"time"

	"github.com/gitshopapp/gitshop/internal/cache"
	"github.com/gitshopapp/gitshop/internal/githubapp"
)

func (h *Handlers) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.loggerFromContext(ctx)
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)

	payload, err := githubapp.ReadWebhookPayload(r, h.config.GitHubWebhookSecret)
	if err != nil {
		logger.Error("failed to read GitHub webhook payload", "error", err)
		http.Error(w, "Invalid webhook", http.StatusBadRequest)
		return
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if deliveryID == "" {
		logger.Error("missing GitHub delivery ID")
		http.Error(w, "Missing delivery ID", http.StatusBadRequest)
		return
	}

	cacheKey := cache.WebhookKey("github", deliveryID)
	_, err = h.cacheProvider.Get(ctx, cacheKey)
	if err == nil {
		logger.Info("webhook already processed", "delivery_id", deliveryID)
		w.WriteHeader(http.StatusOK)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		logger.Error("missing GitHub event type")
		http.Error(w, "Missing event type", http.StatusBadRequest)
		return
	}
	if h.githubRouter == nil {
		logger.Error("github event router not configured")
		http.Error(w, "Webhook handler not configured", http.StatusInternalServerError)
		return
	}

	processErr := h.githubRouter.Handle(ctx, eventType, payload)

	if processErr == nil {
		if err := h.cacheProvider.Set(ctx, cacheKey, "processed", 24*time.Hour); err != nil {
			logger.Error("failed to mark webhook as processed in cache", "error", err)
		}
	}

	if processErr != nil {
		logger.Error("failed to process GitHub webhook", "error", processErr, "type", eventType)
		http.Error(w, "Processing failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
