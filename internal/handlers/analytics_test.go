package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gitshopapp/gitshop/internal/config"
	"github.com/gitshopapp/gitshop/ui/utils"
)

func TestAnalyticsContext(t *testing.T) {
	t.Parallel()

	t.Run("configured", func(t *testing.T) {
		t.Parallel()

		h := &Handlers{
			config: &config.Config{
				UmamiWebsiteID: "test-site-id",
			},
		}

		var captured utils.UmamiConfig
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured = utils.UmamiFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		handler := h.AnalyticsContext(next)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if captured.WebsiteID != "test-site-id" {
			t.Errorf("WebsiteID = %q, want %q", captured.WebsiteID, "test-site-id")
		}
		if captured.ScriptURL != "/stats.js" {
			t.Errorf("ScriptURL = %q, want %q", captured.ScriptURL, "/stats.js")
		}
		if captured.HostURL != "/um" {
			t.Errorf("HostURL = %q, want %q", captured.HostURL, "/um")
		}
		if !captured.Enabled() {
			t.Errorf("expected Umami to be enabled")
		}
	})

	t.Run("unconfigured", func(t *testing.T) {
		t.Parallel()

		h := &Handlers{
			config: &config.Config{},
		}

		var captured utils.UmamiConfig
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured = utils.UmamiFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		handler := h.AnalyticsContext(next)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if captured.Enabled() {
			t.Errorf("expected Umami to be disabled")
		}
	})
}

func TestUmamiScript_NotFoundWhenUnconfigured(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		config:     &config.Config{},
		umamiCache: &umamiScriptCache{},
	}

	req := httptest.NewRequest(http.MethodGet, "/stats.js", nil)
	rec := httptest.NewRecorder()

	h.UmamiScript(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestUmamiScript_ProxyAndCache(t *testing.T) {
	t.Parallel()

	var upstreamHits int32
	mockScript := "/* mock umami tracker */ console.log('tracked');"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamHits, 1)
		w.Header().Set("Content-Type", "application/javascript")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(mockScript)); err != nil {
			t.Errorf("failed to write mock script: %v", err)
		}
	}))
	defer upstream.Close()

	h := &Handlers{
		config: &config.Config{
			UmamiWebsiteID: "site-123",
			UmamiScriptURL: upstream.URL + "/script.js",
		},
		httpClient: upstream.Client(),
		umamiCache: &umamiScriptCache{},
	}

	// First request: should fetch from upstream and cache
	req := httptest.NewRequest(http.MethodGet, "/stats.js", nil)
	rec := httptest.NewRecorder()
	h.UmamiScript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != mockScript {
		t.Errorf("got body %q, want %q", rec.Body.String(), mockScript)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Error("expected non-empty ETag")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/javascript; charset=utf-8" {
		t.Errorf("expected javascript content-type, got %q", ct)
	}
	if hits := atomic.LoadInt32(&upstreamHits); hits != 1 {
		t.Errorf("expected 1 upstream hit, got %d", hits)
	}

	// Second request: served from cache without upstream hit
	req2 := httptest.NewRequest(http.MethodGet, "/stats.js", nil)
	rec2 := httptest.NewRecorder()
	h.UmamiScript(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	if hits := atomic.LoadInt32(&upstreamHits); hits != 1 {
		t.Errorf("expected still 1 upstream hit, got %d", hits)
	}

	// Third request: conditional request with matching ETag should return 304
	req3 := httptest.NewRequest(http.MethodGet, "/stats.js", nil)
	req3.Header.Set("If-None-Match", etag)
	rec3 := httptest.NewRecorder()
	h.UmamiScript(rec3, req3)

	if rec3.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified, got %d", rec3.Code)
	}
}

func TestUmamiSend_NotFoundWhenUnconfigured(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		config: &config.Config{},
	}

	req := httptest.NewRequest(http.MethodPost, "/um/api/send", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	h.UmamiSend(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestUmamiSend_ProxySuccess(t *testing.T) {
	t.Parallel()

	var receivedBody string
	var receivedUA string
	var receivedIP string
	var receivedWebsiteID string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/send" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		receivedBody = string(body)
		receivedUA = r.Header.Get("User-Agent")
		receivedIP = r.Header.Get("X-Forwarded-For")
		receivedWebsiteID = r.Header.Get("x-umami-website-id")

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-umami-cache", "cache-token-abc")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"cache":"cache-token-abc"}`)); err != nil {
			t.Errorf("failed to write upstream response: %v", err)
		}
	}))
	defer upstream.Close()

	h := &Handlers{
		config: &config.Config{
			UmamiWebsiteID:  "site-123",
			UmamiGatewayURL: upstream.URL,
		},
		httpClient: upstream.Client(),
	}

	payload := `{"type":"event","payload":{"website":"site-123","url":"/"}}`
	req := httptest.NewRequest(http.MethodPost, "/um/api/send", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "TestBrowser/1.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.195")

	rec := httptest.NewRecorder()
	h.UmamiSend(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"cache":"cache-token-abc"}` {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
	if rec.Header().Get("x-umami-cache") != "cache-token-abc" {
		t.Errorf("unexpected x-umami-cache header: %s", rec.Header().Get("x-umami-cache"))
	}
	if receivedBody != payload {
		t.Errorf("upstream received body = %q, want %q", receivedBody, payload)
	}
	if receivedUA != "TestBrowser/1.0" {
		t.Errorf("upstream received UA = %q, want TestBrowser/1.0", receivedUA)
	}
	if receivedIP != "203.0.113.195" {
		t.Errorf("upstream received IP = %q, want 203.0.113.195", receivedIP)
	}
	if receivedWebsiteID != "site-123" {
		t.Errorf("upstream received website ID = %q, want site-123", receivedWebsiteID)
	}
}
