package handlers

import (
	"net/http"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/observability"
)

// MetricsContext adds a request-scoped, pre-attributed meter to the context.
func (h *Handlers) MetricsContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := requestIDFromRequest(r)

		attrs := []attribute.Builder{
			attribute.String("http.request_id", requestID),
			attribute.String("http.method", r.Method),
			attribute.String("network.client.ip", clientIP(r)),
		}
		if route := routeLabel(r); route != "" {
			attrs = append(attrs, attribute.String("http.route", route))
		}
		if userAgent := strings.TrimSpace(r.UserAgent()); userAgent != "" {
			attrs = append(attrs, attribute.String("http.user_agent", userAgent))
		}
		if referer := strings.TrimSpace(r.Referer()); referer != "" {
			attrs = append(attrs, attribute.String("http.referer", referer))
		}
		if r.ContentLength >= 0 {
			attrs = append(attrs, attribute.Int64("http.request_content_length", r.ContentLength))
		}

		if sess := h.sessionFromRequest(ctx, r); sess != nil {
			if sess.UserID > 0 {
				attrs = append(attrs, attribute.Int64("user.id", sess.UserID))
			}
			if username := strings.TrimSpace(sess.GitHubUsername); username != "" {
				attrs = append(attrs, attribute.String("user.username", username))
			}
			if sess.InstallationID != 0 {
				attrs = append(attrs, attribute.Int64("github.installation_id", sess.InstallationID))
			}
			if sess.ShopID != uuid.Nil {
				attrs = append(attrs, attribute.String("shop.id", sess.ShopID.String()))
			}
		}

		meter := sentry.NewMeter(ctx).WithCtx(ctx)
		meter.SetAttributes(attrs...)

		ctx = observability.WithMeter(ctx, meter)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
