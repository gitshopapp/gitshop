package handlers

import (
	"net/http"
	"net/http/httptest"
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
