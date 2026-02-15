package handlers

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"

	"github.com/gitshopapp/gitshop/internal/config"
	"github.com/gitshopapp/gitshop/internal/observability"
)

// SecurityHeaders sets baseline security headers for all responses.
func (h *Handlers) SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		headers.Set("Cross-Origin-Opener-Policy", "same-origin")
		headers.Set("Cross-Origin-Resource-Policy", "same-origin")

		next.ServeHTTP(w, r)
	})
}

// RequireSameOrigin blocks cross-origin state-changing requests.
func (h *Handlers) RequireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meter := observability.MeterFromContext(r.Context())
		meter.SetAttributes(attribute.String("component", "security.same_origin"))
		if !requestMutatesState(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		meter.Count("security.same_origin.checked", 1)

		originHeader := strings.TrimSpace(r.Header.Get("Origin"))
		refererHeader := strings.TrimSpace(r.Header.Get("Referer"))

		if originHeader == "" && refererHeader == "" {
			meter.Count("security.same_origin.blocked", 1, sentry.WithAttributes(attribute.String("reason", "missing_origin_and_referer")))
			h.loggerFromContext(r.Context()).Warn("blocked state-changing request without origin/referrer", "method", r.Method, "path", r.URL.Path)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if originHeader != "" {
			if ok, err := h.headerMatchesAllowedHost(originHeader, r); err != nil || !ok {
				meter.Count("security.same_origin.blocked", 1, sentry.WithAttributes(attribute.String("reason", "invalid_origin")))
				h.loggerFromContext(r.Context()).Warn("blocked state-changing request with invalid origin", "origin", originHeader, "error", err)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}

		if refererHeader != "" {
			if ok, err := h.headerMatchesAllowedHost(refererHeader, r); err != nil || !ok {
				meter.Count("security.same_origin.blocked", 1, sentry.WithAttributes(attribute.String("reason", "invalid_referer")))
				h.loggerFromContext(r.Context()).Warn("blocked state-changing request with invalid referer", "referer", refererHeader, "error", err)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func requestMutatesState(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (h *Handlers) headerMatchesAllowedHost(value string, r *http.Request) (bool, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return false, fmt.Errorf("failed to parse URL: %w", err)
	}
	if parsed.Host == "" {
		return false, fmt.Errorf("missing host")
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false, fmt.Errorf("missing hostname")
	}

	allowedHosts := allowedRequestHosts(h.config, r)
	_, ok := allowedHosts[host]
	return ok, nil
}

func allowedRequestHosts(cfg *config.Config, r *http.Request) map[string]struct{} {
	hosts := map[string]struct{}{}

	if r != nil {
		if host := normalizeHost(r.Host); host != "" {
			hosts[host] = struct{}{}
		}
	}

	if cfg != nil {
		if host := hostFromBaseURL(cfg.BaseURL); host != "" {
			hosts[host] = struct{}{}
		}
	}

	return hosts
}

func normalizeHost(hostport string) string {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return ""
	}

	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.ToLower(strings.TrimSpace(host))
	}
	return strings.ToLower(hostport)
}

func hostFromBaseURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
