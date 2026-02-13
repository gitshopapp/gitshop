package handlers

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/gitshopapp/gitshop/internal/logging"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// RequestLogger logs all incoming requests and injects a request-scoped logger into context.
func (h *Handlers) RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		route := routeLabel(r)

		requestID := requestIDFromRequest(r)
		w.Header().Set("X-Request-ID", requestID)

		logger := h.logger.With(
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_ip", clientIP(r),
		)

		if route != "" {
			logger = logger.With("route", route)
		}

		if userAgent := strings.TrimSpace(r.UserAgent()); userAgent != "" {
			logger = logger.With("user_agent", userAgent)
		}
		if referer := strings.TrimSpace(r.Referer()); referer != "" {
			logger = logger.With("referer", referer)
		}
		if r.ContentLength >= 0 {
			logger = logger.With("content_length", r.ContentLength)
		}

		ctx := logging.WithLogger(r.Context(), logger)
		r = r.WithContext(ctx)

		wrapped := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)

		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}
		durationMs := float64(time.Since(start).Milliseconds())

		metricRoute := route
		if metricRoute == "" {
			metricRoute = "unknown"
		}
		metricAttrs := []attribute.Builder{
			attribute.String("http.method", r.Method),
			attribute.String("http.route", metricRoute),
			attribute.Int("http.status_code", status),
		}
		meter := sentry.NewMeter(ctx).WithCtx(ctx)
		meter.Count("http.server.requests", 1, sentry.WithAttributes(metricAttrs...))
		meter.Distribution(
			"http.server.duration",
			durationMs,
			sentry.WithUnit(sentry.UnitMillisecond),
			sentry.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", metricRoute),
				attribute.String("http.status_class", fmt.Sprintf("%dxx", status/100)),
			),
		)
		if status >= http.StatusInternalServerError {
			meter.Count("http.server.errors", 1, sentry.WithAttributes(metricAttrs...))
		}

		if strings.HasPrefix(r.URL.Path, "/assets/") {
			// log asset requests as debug
			logger.Debug("asset request completed",
				"status", status,
				"duration_ms", int64(durationMs),
				"bytes", wrapped.bytes,
			)
		} else {
			logger.Info("request completed",
				"status", status,
				"duration_ms", int64(durationMs),
				"bytes", wrapped.bytes,
			)
		}
	})
}

func requestIDFromRequest(r *http.Request) string {
	if r == nil {
		return newRequestID()
	}
	if requestID := strings.TrimSpace(r.Header.Get("X-Request-ID")); requestID != "" {
		return requestID
	}
	if requestID := strings.TrimSpace(r.Header.Get("X-Request-Id")); requestID != "" {
		return requestID
	}
	return newRequestID()
}

func newRequestID() string {
	return uuid.NewString()
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-Ip")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func routeLabel(r *http.Request) string {
	if r == nil {
		return ""
	}
	route := mux.CurrentRoute(r)
	if route == nil {
		return ""
	}
	if name := route.GetName(); name != "" {
		return name
	}
	if template, err := route.GetPathTemplate(); err == nil && template != "" {
		return template
	}
	return ""
}
