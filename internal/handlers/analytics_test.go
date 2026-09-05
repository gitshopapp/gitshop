package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitshopapp/gitshop/internal/config"
	"github.com/gitshopapp/gitshop/ui/utils"
)

func TestAnalyticsContext(t *testing.T) {
	t.Parallel()

	h := &Handlers{
		config: &config.Config{
			UmamiWebsiteID: "test-site-id",
			UmamiScriptURL: "https://cloud.umami.is/script.js",
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
	if captured.ScriptURL != "https://cloud.umami.is/script.js" {
		t.Errorf("ScriptURL = %q, want %q", captured.ScriptURL, "https://cloud.umami.is/script.js")
	}
	if !captured.Enabled() {
		t.Errorf("expected Umami to be enabled")
	}
}
