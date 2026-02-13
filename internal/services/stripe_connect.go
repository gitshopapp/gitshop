package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/cache"
	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/stripe"
)

const stripeOnboardingStateTTL = 30 * time.Minute

var (
	ErrStripeConnectUnavailable   = errors.New("stripe connect service unavailable")
	ErrStripeConnectInvalidState  = errors.New("invalid stripe onboarding state")
	ErrStripeConnectShopNotFound  = errors.New("shop not found")
	ErrStripeConnectNoAccount     = errors.New("stripe account not connected")
	ErrStripeConnectCreateAccount = errors.New("failed to create stripe account")
	ErrStripeConnectCreateLink    = errors.New("failed to create stripe onboarding link")
	ErrStripeConnectGetAccount    = errors.New("failed to retrieve stripe account")
)

type StripeConnectStatus struct {
	Connected        bool
	Status           string
	AccountID        string
	DetailsSubmitted bool
	ChargesEnabled   bool
	PayoutsEnabled   bool
	Error            string
}

type CompleteOnboardingResult struct {
	ShopID           uuid.UUID
	AccountID        string
	Connected        bool
	DetailsSubmitted bool
	ChargesEnabled   bool
	PayoutsEnabled   bool
}

type StripeConnectService struct {
	shopStore      *db.ShopStore
	stripePlatform *stripe.PlatformClient
	cacheProvider  cache.Provider
	logger         *slog.Logger
}

func NewStripeConnectService(shopStore *db.ShopStore, stripePlatform *stripe.PlatformClient, cacheProvider cache.Provider, logger *slog.Logger) *StripeConnectService {
	return &StripeConnectService{
		shopStore:      shopStore,
		stripePlatform: stripePlatform,
		cacheProvider:  cacheProvider,
		logger:         logger,
	}
}

func (s *StripeConnectService) loggerFromContext(ctx context.Context) *slog.Logger {
	return logging.FromContext(ctx, s.logger)
}

func (s *StripeConnectService) StartOnboarding(ctx context.Context, shopID uuid.UUID, baseURL string) (string, error) {
	span := sentry.StartSpan(
		ctx,
		"service.stripe_connect.start_onboarding",
		sentry.WithOpName("service.stripe_connect"),
		sentry.WithDescription("StartOnboarding"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := sentry.NewMeter(ctx).WithCtx(ctx)
	action := "start"
	recordFailed := func(reason string) {
		meter.Count("stripe.connect.onboarding.failed", 1, sentry.WithAttributes(
			attribute.String("action", action),
			attribute.String("reason", reason),
		))
	}
	meter.Count("stripe.connect.onboarding.received", 1, sentry.WithAttributes(
		attribute.String("action", action),
	))

	if s == nil || s.stripePlatform == nil {
		recordFailed("service_unavailable")
		return "", ErrStripeConnectUnavailable
	}
	if s.shopStore == nil || s.cacheProvider == nil {
		recordFailed("dependencies_missing")
		return "", fmt.Errorf("stripe connect service dependencies are not configured")
	}
	if shopID == uuid.Nil {
		recordFailed("invalid_shop_id")
		return "", fmt.Errorf("%w: empty shop id", ErrStripeConnectShopNotFound)
	}

	shop, err := s.shopStore.GetByID(ctx, shopID)
	if err != nil {
		recordFailed("shop_lookup_failed")
		return "", fmt.Errorf("%w: %w", ErrStripeConnectShopNotFound, err)
	}

	accountID := shop.StripeConnectAccountID
	if accountID == "" {
		account, createErr := s.stripePlatform.CreateAccount(ctx, "US")
		if createErr != nil {
			recordFailed("create_account_failed")
			return "", fmt.Errorf("%w: %w", ErrStripeConnectCreateAccount, createErr)
		}
		accountID = account.ID
		meter.Count("stripe.connect.account.created", 1)

		if err := s.shopStore.UpdateStripeConnectAccount(ctx, shop.ID, accountID); err != nil {
			recordFailed("persist_account_failed")
			return "", fmt.Errorf("failed to persist stripe account id: %w", err)
		}
	}

	state, err := generateStripeOnboardingState()
	if err != nil {
		recordFailed("state_generation_failed")
		return "", fmt.Errorf("failed to generate onboarding state: %w", err)
	}

	cacheKey := stripeOnboardStateCacheKey(state)
	if err := s.cacheProvider.Set(ctx, cacheKey, shop.ID.String(), stripeOnboardingStateTTL); err != nil {
		recordFailed("state_store_failed")
		return "", fmt.Errorf("failed to store onboarding state: %w", err)
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	returnURL := fmt.Sprintf("%s/admin/stripe/onboard/callback?state=%s", baseURL, url.QueryEscape(state))
	refreshURL := fmt.Sprintf("%s/admin/setup?stripe=refresh", baseURL)

	link, err := s.stripePlatform.CreateAccountLink(ctx, accountID, returnURL, refreshURL)
	if err != nil {
		recordFailed("create_link_failed")
		return "", fmt.Errorf("%w: %w", ErrStripeConnectCreateLink, err)
	}
	meter.Count("stripe.connect.onboarding.processed", 1, sentry.WithAttributes(
		attribute.String("action", action),
		attribute.String("outcome", "link_created"),
	))

	return link.URL, nil
}

func (s *StripeConnectService) CompleteOnboarding(ctx context.Context, state string) (CompleteOnboardingResult, error) {
	span := sentry.StartSpan(
		ctx,
		"service.stripe_connect.complete_onboarding",
		sentry.WithOpName("service.stripe_connect"),
		sentry.WithDescription("CompleteOnboarding"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	result := CompleteOnboardingResult{}
	meter := sentry.NewMeter(ctx).WithCtx(ctx)
	action := "complete"
	recordFailed := func(reason string) {
		meter.Count("stripe.connect.onboarding.failed", 1, sentry.WithAttributes(
			attribute.String("action", action),
			attribute.String("reason", reason),
		))
	}
	meter.Count("stripe.connect.onboarding.received", 1, sentry.WithAttributes(
		attribute.String("action", action),
	))

	if s == nil || s.stripePlatform == nil {
		recordFailed("service_unavailable")
		return result, ErrStripeConnectUnavailable
	}

	state = strings.TrimSpace(state)
	if state == "" {
		recordFailed("invalid_state")
		return result, fmt.Errorf("%w: state is required", ErrStripeConnectInvalidState)
	}
	if s.shopStore == nil || s.cacheProvider == nil {
		recordFailed("dependencies_missing")
		return result, fmt.Errorf("stripe connect service dependencies are not configured")
	}

	cacheKey := stripeOnboardStateCacheKey(state)
	shopIDStr, err := s.cacheProvider.Get(ctx, cacheKey)
	if err != nil {
		recordFailed("state_lookup_failed")
		return result, fmt.Errorf("%w: %w", ErrStripeConnectInvalidState, err)
	}

	shopID, err := uuid.Parse(shopIDStr)
	if err != nil {
		recordFailed("invalid_shop_id")
		return result, fmt.Errorf("%w: %w", ErrStripeConnectInvalidState, err)
	}

	shop, err := s.shopStore.GetByID(ctx, shopID)
	if err != nil || shop.StripeConnectAccountID == "" {
		recordFailed("shop_lookup_failed")
		return result, fmt.Errorf("%w: %v", ErrStripeConnectShopNotFound, err)
	}

	account, err := s.stripePlatform.GetAccount(ctx, shop.StripeConnectAccountID)
	if err != nil {
		recordFailed("get_account_failed")
		return result, fmt.Errorf("%w: %w", ErrStripeConnectGetAccount, err)
	}

	if err := s.shopStore.UpdateStripeConnectDetails(ctx, shopID, shop.StripeConnectAccountID, account.DetailsSubmitted, account.ChargesEnabled, account.PayoutsEnabled); err != nil {
		meter.Count("stripe.connect.onboarding.side_effect_failed", 1, sentry.WithAttributes(
			attribute.String("action", action),
			attribute.String("reason", "persist_details_failed"),
		))
		s.loggerFromContext(ctx).Error("failed to persist stripe connect details", "error", err, "shop_id", shopID)
	}

	if err := s.cacheProvider.Delete(ctx, cacheKey); err != nil {
		meter.Count("stripe.connect.onboarding.side_effect_failed", 1, sentry.WithAttributes(
			attribute.String("action", action),
			attribute.String("reason", "state_cleanup_failed"),
		))
		s.loggerFromContext(ctx).Warn("failed to clean stripe onboarding state", "error", err, "cache_key", cacheKey)
	}

	result = CompleteOnboardingResult{
		ShopID:           shopID,
		AccountID:        shop.StripeConnectAccountID,
		Connected:        account.DetailsSubmitted, // as long as the user submitted their details we can continue with setup
		DetailsSubmitted: account.DetailsSubmitted,
		ChargesEnabled:   account.ChargesEnabled,
		PayoutsEnabled:   account.PayoutsEnabled,
	}
	outcome := "pending"
	if result.Connected {
		outcome = "connected"
	}
	meter.Count("stripe.connect.onboarding.processed", 1, sentry.WithAttributes(
		attribute.String("action", action),
		attribute.String("outcome", outcome),
	))

	return result, nil
}

func (s *StripeConnectService) GetConnectionStatus(ctx context.Context, shopID uuid.UUID) (StripeConnectStatus, error) {
	status := StripeConnectStatus{
		Connected: false,
		Status:    "not_connected",
	}

	if s == nil || s.stripePlatform == nil {
		return status, ErrStripeConnectUnavailable
	}
	if s.shopStore == nil {
		return status, fmt.Errorf("stripe connect service dependencies are not configured")
	}
	if shopID == uuid.Nil {
		return status, fmt.Errorf("%w: empty shop id", ErrStripeConnectShopNotFound)
	}

	shop, err := s.shopStore.GetByID(ctx, shopID)
	if err != nil {
		return status, fmt.Errorf("%w: %w", ErrStripeConnectShopNotFound, err)
	}
	if shop.StripeConnectAccountID == "" {
		return status, nil
	}

	status.AccountID = shop.StripeConnectAccountID
	account, err := s.stripePlatform.GetAccount(ctx, shop.StripeConnectAccountID)
	if err != nil {
		status.Status = "error"
		status.Error = err.Error()
		return status, nil
	}

	status.DetailsSubmitted = account.DetailsSubmitted
	status.ChargesEnabled = account.ChargesEnabled
	status.PayoutsEnabled = account.PayoutsEnabled
	status.Connected = account.ChargesEnabled && account.PayoutsEnabled

	switch {
	case account.ChargesEnabled && account.PayoutsEnabled:
		status.Status = "connected"
	case account.DetailsSubmitted:
		status.Status = "pending_verification"
	default:
		status.Status = "incomplete"
	}

	return status, nil
}

func (s *StripeConnectService) ReconnectOnboarding(ctx context.Context, shopID uuid.UUID, baseURL string) (string, error) {
	span := sentry.StartSpan(
		ctx,
		"service.stripe_connect.reconnect_onboarding",
		sentry.WithOpName("service.stripe_connect"),
		sentry.WithDescription("ReconnectOnboarding"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := sentry.NewMeter(ctx).WithCtx(ctx)
	action := "reconnect"
	recordFailed := func(reason string) {
		meter.Count("stripe.connect.onboarding.failed", 1, sentry.WithAttributes(
			attribute.String("action", action),
			attribute.String("reason", reason),
		))
	}
	meter.Count("stripe.connect.onboarding.received", 1, sentry.WithAttributes(
		attribute.String("action", action),
	))

	if s == nil || s.stripePlatform == nil {
		recordFailed("service_unavailable")
		return "", ErrStripeConnectUnavailable
	}
	if s.shopStore == nil || s.cacheProvider == nil {
		recordFailed("dependencies_missing")
		return "", fmt.Errorf("stripe connect service dependencies are not configured")
	}
	if shopID == uuid.Nil {
		recordFailed("invalid_shop_id")
		return "", fmt.Errorf("%w: empty shop id", ErrStripeConnectShopNotFound)
	}

	shop, err := s.shopStore.GetByID(ctx, shopID)
	if err != nil {
		recordFailed("shop_lookup_failed")
		return "", fmt.Errorf("%w: %w", ErrStripeConnectShopNotFound, err)
	}
	if shop.StripeConnectAccountID == "" {
		recordFailed("missing_account")
		return "", ErrStripeConnectNoAccount
	}

	state, err := generateStripeOnboardingState()
	if err != nil {
		recordFailed("state_generation_failed")
		return "", fmt.Errorf("failed to generate onboarding state: %w", err)
	}

	cacheKey := stripeOnboardStateCacheKey(state)
	if err := s.cacheProvider.Set(ctx, cacheKey, shop.ID.String(), stripeOnboardingStateTTL); err != nil {
		recordFailed("state_store_failed")
		return "", fmt.Errorf("failed to store onboarding state: %w", err)
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	returnURL := fmt.Sprintf("%s/admin/stripe/onboard/callback?state=%s", baseURL, url.QueryEscape(state))
	refreshURL := fmt.Sprintf("%s/admin/setup?stripe=refresh", baseURL)

	link, err := s.stripePlatform.CreateAccountLink(ctx, shop.StripeConnectAccountID, returnURL, refreshURL)
	if err != nil {
		recordFailed("create_link_failed")
		return "", fmt.Errorf("%w: %w", ErrStripeConnectCreateLink, err)
	}
	meter.Count("stripe.connect.onboarding.processed", 1, sentry.WithAttributes(
		attribute.String("action", action),
		attribute.String("outcome", "link_created"),
	))

	return link.URL, nil
}

func (s *StripeConnectService) Disconnect(ctx context.Context, shopID uuid.UUID) error {
	span := sentry.StartSpan(
		ctx,
		"service.stripe_connect.disconnect",
		sentry.WithOpName("service.stripe_connect"),
		sentry.WithDescription("Disconnect"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := sentry.NewMeter(ctx).WithCtx(ctx)
	meter.Count("stripe.connect.disconnect.received", 1)
	recordFailed := func(reason string) {
		meter.Count("stripe.connect.disconnect.failed", 1, sentry.WithAttributes(
			attribute.String("reason", reason),
		))
	}

	if s == nil || s.shopStore == nil {
		recordFailed("dependencies_missing")
		return fmt.Errorf("stripe connect service dependencies are not configured")
	}
	if shopID == uuid.Nil {
		recordFailed("invalid_shop_id")
		return fmt.Errorf("%w: empty shop id", ErrStripeConnectShopNotFound)
	}

	shop, err := s.shopStore.GetByID(ctx, shopID)
	if err != nil {
		recordFailed("shop_lookup_failed")
		return fmt.Errorf("%w: %w", ErrStripeConnectShopNotFound, err)
	}
	if shop.StripeConnectAccountID == "" {
		meter.Count("stripe.connect.disconnect.processed", 1, sentry.WithAttributes(
			attribute.String("outcome", "no_account"),
		))
		return nil
	}

	if err := s.shopStore.UpdateStripeConnectAccount(ctx, shop.ID, ""); err != nil {
		recordFailed("disconnect_failed")
		return fmt.Errorf("failed to disconnect stripe account: %w", err)
	}
	meter.Count("stripe.connect.disconnect.processed", 1, sentry.WithAttributes(
		attribute.String("outcome", "disconnected"),
	))

	return nil
}

func stripeOnboardStateCacheKey(state string) string {
	return fmt.Sprintf("stripe_onboard:%s", state)
}

func generateStripeOnboardingState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
