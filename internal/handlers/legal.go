package handlers

import (
	"net/http"

	"github.com/gitshopapp/gitshop/ui/views"
)

func (h *Handlers) TermsOfUse(w http.ResponseWriter, r *http.Request) {
	if err := views.TermsPage().Render(r.Context(), w); err != nil {
		h.loggerFromContext(r.Context()).Error("failed to render terms page", "error", err)
		http.Error(w, "Failed to render terms page", http.StatusInternalServerError)
	}
}

func (h *Handlers) PrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	if err := views.PrivacyPolicyPage().Render(r.Context(), w); err != nil {
		h.loggerFromContext(r.Context()).Error("failed to render privacy policy page", "error", err)
		http.Error(w, "Failed to render privacy policy page", http.StatusInternalServerError)
	}
}
