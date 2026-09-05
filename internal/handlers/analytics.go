package handlers

import (
	"net/http"
	"strings"

	"github.com/gitshopapp/gitshop/ui/utils"
)

const defaultUmamiScriptURL = "https://cloud.umami.is/script.js"

// AnalyticsContext attaches public analytics configuration (such as Umami) to the request context.
func (h *Handlers) AnalyticsContext(next http.Handler) http.Handler {
	websiteID := ""
	scriptURL := ""
	if h.config != nil {
		websiteID = strings.TrimSpace(h.config.UmamiWebsiteID)
		scriptURL = strings.TrimSpace(h.config.UmamiScriptURL)
	}
	if websiteID != "" && scriptURL == "" {
		scriptURL = defaultUmamiScriptURL
	}
	umamiCfg := utils.UmamiConfig{
		WebsiteID: websiteID,
		ScriptURL: scriptURL,
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := utils.WithUmamiConfig(r.Context(), umamiCfg)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
