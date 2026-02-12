package handlers

import (
	"context"

	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/session"
	"github.com/gitshopapp/gitshop/ui/views"
)

func (h *Handlers) buildShopSwitcher(ctx context.Context, sess *session.Data) *views.ShopSwitcherProps {
	if sess == nil || sess.InstallationID <= 0 {
		return nil
	}

	switcher, err := h.adminService.BuildShopSwitcher(ctx, sess.InstallationID, sess.ShopID)
	if err != nil {
		h.loggerFromContext(ctx).Error("failed to build shop switcher", "error", err, "installation_id", sess.InstallationID)
		return nil
	}
	if switcher == nil || len(switcher.Options) == 0 {
		return nil
	}

	options := make([]views.ShopSwitcherOption, 0, len(switcher.Options))
	for _, shop := range switcher.Options {
		options = append(options, views.ShopSwitcherOption{
			ID:    shop.ShopID.String(),
			Label: shop.RepoFullName,
		})
	}

	activeID := ""
	if switcher.ActiveShopID != uuid.Nil {
		activeID = switcher.ActiveShopID.String()
	}

	return &views.ShopSwitcherProps{
		ActiveID: activeID,
		Options:  options,
	}
}
