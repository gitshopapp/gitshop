package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/db"
)

type ShopSelectionItem struct {
	ShopID       uuid.UUID
	RepoFullName string
	Ready        bool
}

type ShopSwitcherOption struct {
	ShopID       uuid.UUID
	RepoFullName string
}

type ShopSwitcher struct {
	ActiveShopID uuid.UUID
	Options      []ShopSwitcherOption
}

func (s *AdminService) IsStripeReady(ctx context.Context, shop *db.Shop) bool {
	if s == nil || shop == nil || shop.StripeConnectAccountID == "" || s.stripePlatform == nil || s.shopStore == nil {
		return false
	}

	account, err := s.stripePlatform.GetAccount(ctx, shop.StripeConnectAccountID)
	if err != nil {
		s.loggerFromContext(ctx).Warn("failed to verify stripe account", "error", err, "shop_id", shop.ID)
		return false
	}

	if err := s.shopStore.UpdateStripeConnectDetails(ctx, shop.ID, shop.StripeConnectAccountID, account.DetailsSubmitted, account.ChargesEnabled, account.PayoutsEnabled); err != nil {
		s.loggerFromContext(ctx).Warn("failed to persist stripe account details", "error", err, "shop_id", shop.ID)
	}

	return account.ChargesEnabled && account.PayoutsEnabled
}

func (s *AdminService) IsOnboardingComplete(ctx context.Context, shop *db.Shop) bool {
	if s == nil || shop == nil || s.githubClient == nil || s.parser == nil || s.validator == nil {
		return false
	}

	stripeReady := s.IsStripeReady(ctx, shop)
	status := s.BuildSetupStatus(ctx, shop)

	return stripeReady &&
		IsEmailConfigured(shop) &&
		status.Labels.Ready &&
		status.YAML.Exists &&
		status.Template.Exists
}

func (s *AdminService) GetInstallationShops(ctx context.Context, installationID int64) ([]*db.Shop, error) {
	if s == nil || s.shopStore == nil {
		return nil, fmt.Errorf("%w: shop store unavailable", ErrAdminServiceUnavailable)
	}
	if installationID <= 0 {
		return []*db.Shop{}, nil
	}

	shops, err := s.shopStore.GetConnectedShopsByInstallationID(ctx, installationID)
	if err != nil {
		return nil, err
	}
	return shops, nil
}

func (s *AdminService) CountInstallationShops(ctx context.Context, installationID int64) (int, error) {
	if s == nil || s.shopStore == nil {
		return 0, fmt.Errorf("%w: shop store unavailable", ErrAdminServiceUnavailable)
	}
	if installationID <= 0 {
		return 0, nil
	}

	return s.shopStore.CountShopsByInstallationID(ctx, installationID)
}

func (s *AdminService) GetShopForInstallation(ctx context.Context, installationID int64, shopID uuid.UUID) (*db.Shop, error) {
	if s == nil || s.shopStore == nil {
		return nil, fmt.Errorf("%w: shop store unavailable", ErrAdminServiceUnavailable)
	}
	if installationID <= 0 || shopID == uuid.Nil {
		return nil, fmt.Errorf("%w: shop does not belong to installation", ErrAdminShopNotFound)
	}

	shop, err := s.shopStore.GetByID(ctx, shopID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAdminShopNotFound, err)
	}
	if shop.GitHubInstallationID != installationID {
		return nil, fmt.Errorf("%w: shop does not belong to installation", ErrAdminShopNotFound)
	}
	return shop, nil
}

func (s *AdminService) BuildShopSelectionItems(ctx context.Context, shops []*db.Shop) []ShopSelectionItem {
	if len(shops) == 0 {
		return []ShopSelectionItem{}
	}

	items := make([]ShopSelectionItem, 0, len(shops))
	for _, shop := range shops {
		if shop == nil {
			continue
		}
		items = append(items, ShopSelectionItem{
			ShopID:       shop.ID,
			RepoFullName: shop.GitHubRepoFullName,
			Ready:        s.IsOnboardingComplete(ctx, shop),
		})
	}
	return items
}

func (s *AdminService) BuildShopSwitcher(ctx context.Context, installationID int64, activeShopID uuid.UUID) (*ShopSwitcher, error) {
	shops, err := s.GetInstallationShops(ctx, installationID)
	if err != nil {
		return nil, err
	}
	if len(shops) <= 1 {
		return nil, nil
	}

	options := make([]ShopSwitcherOption, 0, len(shops))
	for _, shop := range shops {
		if shop == nil {
			continue
		}
		options = append(options, ShopSwitcherOption{
			ShopID:       shop.ID,
			RepoFullName: shop.GitHubRepoFullName,
		})
	}

	return &ShopSwitcher{
		ActiveShopID: activeShopID,
		Options:      options,
	}, nil
}
