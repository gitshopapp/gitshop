package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gitshopapp/gitshop/ui/utils"
)

const defaultUmamiScriptURL = "https://cloud.umami.is/script.js"
const defaultUmamiGatewayURL = "https://gateway.umami.is"
const umamiScriptTTL = 1 * time.Hour
const umamiProxyPath = "/stats.js"
const umamiHostURL = "/um"
const maxUmamiPayloadBytes = 64 * 1024 // 64 KB

type umamiScriptCache struct {
	mu        sync.RWMutex
	content   []byte
	etag      string
	updatedAt time.Time
}

// AnalyticsContext attaches public analytics configuration (such as Umami) to the request context.
func (h *Handlers) AnalyticsContext(next http.Handler) http.Handler {
	websiteID := ""
	if h.config != nil {
		websiteID = strings.TrimSpace(h.config.UmamiWebsiteID)
	}

	scriptURL := ""
	hostURL := ""
	if websiteID != "" {
		scriptURL = umamiProxyPath
		hostURL = umamiHostURL
	}

	umamiCfg := utils.UmamiConfig{
		WebsiteID: websiteID,
		ScriptURL: scriptURL,
		HostURL:   hostURL,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := utils.WithUmamiConfig(r.Context(), umamiCfg)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UmamiScript proxies the upstream Umami tracking script to bypass ad blockers.
func (h *Handlers) UmamiScript(w http.ResponseWriter, r *http.Request) {
	if h.config == nil || strings.TrimSpace(h.config.UmamiWebsiteID) == "" {
		http.NotFound(w, r)
		return
	}

	content, etag := h.getUmamiScript()
	if len(content) == 0 {
		var err error
		content, etag, err = h.fetchAndCacheUmamiScript(r.Context())
		if err != nil {
			content, etag = h.getStaleUmamiScript()
			if len(content) == 0 {
				h.loggerFromContext(r.Context()).Error("failed to fetch umami script", "error", err)
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				return
			}
		}
	}

	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(content); err != nil {
		h.loggerFromContext(r.Context()).Error("failed to write umami script response", "error", err)
	}
}

// UmamiSend proxies analytics collection events to the upstream Umami gateway.
func (h *Handlers) UmamiSend(w http.ResponseWriter, r *http.Request) {
	if h.config == nil || strings.TrimSpace(h.config.UmamiWebsiteID) == "" {
		http.NotFound(w, r)
		return
	}

	gatewayURL := defaultUmamiGatewayURL
	if h.config != nil && strings.TrimSpace(h.config.UmamiGatewayURL) != "" {
		gatewayURL = strings.TrimSpace(h.config.UmamiGatewayURL)
	}
	upstreamURL := strings.TrimRight(gatewayURL, "/") + "/api/send"

	body, err := io.ReadAll(io.LimitReader(r.Body, maxUmamiPayloadBytes))
	if err != nil {
		h.loggerFromContext(r.Context()).Warn("failed to read umami send body", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		h.loggerFromContext(r.Context()).Error("failed to create upstream umami send request", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if ua := r.Header.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	clientIP := r.Header.Get("X-Forwarded-For")
	if clientIP == "" {
		clientIP = r.Header.Get("X-Real-IP")
	}
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}
	if clientIP != "" {
		req.Header.Set("X-Forwarded-For", clientIP)
	}

	if wid := r.Header.Get("x-umami-website-id"); wid != "" {
		req.Header.Set("x-umami-website-id", wid)
	} else if h.config != nil && h.config.UmamiWebsiteID != "" {
		req.Header.Set("x-umami-website-id", h.config.UmamiWebsiteID)
	}

	if host := r.Header.Get("x-umami-hostname"); host != "" {
		req.Header.Set("x-umami-hostname", host)
	}
	if cacheVal := r.Header.Get("x-umami-cache"); cacheVal != "" {
		req.Header.Set("x-umami-cache", cacheVal)
	}

	client := h.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		h.loggerFromContext(r.Context()).Error("failed to proxy umami send request", "error", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && h.logger != nil {
			h.logger.Warn("failed to close upstream umami send response body", "error", closeErr)
		}
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxUmamiPayloadBytes))
	if err != nil {
		h.loggerFromContext(r.Context()).Error("failed to read upstream umami send response", "error", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	if cacheVal := resp.Header.Get("x-umami-cache"); cacheVal != "" {
		w.Header().Set("x-umami-cache", cacheVal)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}

	w.WriteHeader(resp.StatusCode)
	if len(respBody) > 0 {
		if _, writeErr := w.Write(respBody); writeErr != nil {
			h.loggerFromContext(r.Context()).Error("failed to write umami send response", "error", writeErr)
		}
	}
}

func (h *Handlers) fetchAndCacheUmamiScript(ctx context.Context) ([]byte, string, error) {
	upstreamURL := defaultUmamiScriptURL
	if h.config != nil && strings.TrimSpace(h.config.UmamiScriptURL) != "" {
		upstreamURL = strings.TrimSpace(h.config.UmamiScriptURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request for umami script: %w", err)
	}

	client := h.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch umami script: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && h.logger != nil {
			h.logger.Warn("failed to close umami script response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status from umami script endpoint: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read umami script body: %w", err)
	}

	hash := sha256.Sum256(body)
	etag := fmt.Sprintf(`"%x"`, hash)

	if h.umamiCache != nil {
		h.umamiCache.mu.Lock()
		h.umamiCache.content = body
		h.umamiCache.etag = etag
		h.umamiCache.updatedAt = time.Now()
		h.umamiCache.mu.Unlock()
	}

	return body, etag, nil
}

func (h *Handlers) getUmamiScript() ([]byte, string) {
	if h.umamiCache == nil {
		return nil, ""
	}
	h.umamiCache.mu.RLock()
	defer h.umamiCache.mu.RUnlock()

	if len(h.umamiCache.content) > 0 && time.Since(h.umamiCache.updatedAt) < umamiScriptTTL {
		return h.umamiCache.content, h.umamiCache.etag
	}
	return nil, ""
}

func (h *Handlers) getStaleUmamiScript() ([]byte, string) {
	if h.umamiCache == nil {
		return nil, ""
	}
	h.umamiCache.mu.RLock()
	defer h.umamiCache.mu.RUnlock()

	return h.umamiCache.content, h.umamiCache.etag
}
