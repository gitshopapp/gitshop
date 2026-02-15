package handlers

import (
	"net/http"
	"strings"

	"github.com/gitshopapp/gitshop/ui/views"
)

const defaultGitHubAppInstallURL = "https://github.com/apps/gitshopapp"

func (h *Handlers) Landing(w http.ResponseWriter, r *http.Request) {
	gitHubAppURL := defaultGitHubAppInstallURL
	if h.config != nil && strings.TrimSpace(h.config.GitHubAppURL) != "" {
		gitHubAppURL = h.config.GitHubAppURL
	}

	if err := views.LandingPage(views.LandingPageProps{
		GitHubAppURL: gitHubAppURL,
	}).Render(r.Context(), w); err != nil {
		h.loggerFromContext(r.Context()).Error("failed to render landing page", "error", err)
		http.Error(w, "Failed to render landing page", http.StatusInternalServerError)
	}
}
