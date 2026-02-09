package services

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/db"
)

func TestAdminService_IsOnboardingComplete_NilShop(t *testing.T) {
	t.Parallel()

	service := &AdminService{}
	if service.IsOnboardingComplete(t.Context(), nil) {
		t.Fatal("expected onboarding to be incomplete for nil shop")
	}
}

func TestAdminService_GetInstallationShops_ServiceUnavailable(t *testing.T) {
	t.Parallel()

	service := &AdminService{}
	_, err := service.GetInstallationShops(t.Context(), 123)
	if !errors.Is(err, ErrAdminServiceUnavailable) {
		t.Fatalf("expected ErrAdminServiceUnavailable, got %v", err)
	}
}

func TestAdminService_GetShopForInstallation_InvalidInput(t *testing.T) {
	t.Parallel()

	service := &AdminService{
		shopStore: &db.ShopStore{},
	}

	_, err := service.GetShopForInstallation(t.Context(), 0, uuid.New())
	if !errors.Is(err, ErrAdminShopNotFound) {
		t.Fatalf("expected ErrAdminShopNotFound for empty installation id, got %v", err)
	}

	_, err = service.GetShopForInstallation(t.Context(), 123, uuid.Nil)
	if !errors.Is(err, ErrAdminShopNotFound) {
		t.Fatalf("expected ErrAdminShopNotFound for nil shop id, got %v", err)
	}
}

func TestAdminService_BuildShopSelectionItems_Empty(t *testing.T) {
	t.Parallel()

	service := &AdminService{}
	items := service.BuildShopSelectionItems(t.Context(), nil)
	if len(items) != 0 {
		t.Fatalf("expected empty selection items, got %d", len(items))
	}
}

func TestAdminService_EnsureRepoLabels_NilShop(t *testing.T) {
	t.Parallel()

	service := &AdminService{}
	err := service.EnsureRepoLabels(t.Context(), nil)
	if err == nil {
		t.Fatal("expected error for nil shop")
	}
}

func TestAdminService_EnsureGitShopYAML_NilShop(t *testing.T) {
	t.Parallel()

	service := &AdminService{}
	_, err := service.EnsureGitShopYAML(t.Context(), nil)
	if err == nil {
		t.Fatal("expected error for nil shop")
	}
}

func TestAdminService_GetRecentOrders_ServiceUnavailable(t *testing.T) {
	t.Parallel()

	service := &AdminService{}
	_, err := service.GetRecentOrders(t.Context(), uuid.New(), 20)
	if !errors.Is(err, ErrAdminServiceUnavailable) {
		t.Fatalf("expected ErrAdminServiceUnavailable, got %v", err)
	}
}

func TestAdminService_GetRecentOrders_InvalidShopID(t *testing.T) {
	t.Parallel()

	service := &AdminService{
		orderStore: &db.OrderStore{},
	}

	_, err := service.GetRecentOrders(t.Context(), uuid.Nil, 20)
	if !errors.Is(err, ErrAdminShopNotFound) {
		t.Fatalf("expected ErrAdminShopNotFound, got %v", err)
	}
}
