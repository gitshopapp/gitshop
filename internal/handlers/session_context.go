package handlers

import (
	"context"
	"net/http"

	"github.com/gitshopapp/gitshop/internal/session"
)

func (h *Handlers) sessionFromRequest(ctx context.Context, r *http.Request) *session.Data {
	if ctx == nil {
		ctx = context.Background()
	}
	if sess := session.GetSessionFromContext(ctx); sess != nil {
		return sess
	}
	if h == nil || h.sessionManager == nil || r == nil {
		return nil
	}
	sess, err := h.sessionManager.GetSession(ctx, r)
	if err != nil {
		return nil
	}
	return sess
}
