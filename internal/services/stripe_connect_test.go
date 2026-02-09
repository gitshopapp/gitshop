package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/stripe"
)

func TestStripeConnectService_StartOnboarding_Unavailable(t *testing.T) {
	t.Parallel()

	service := NewStripeConnectService(nil, nil, nil, nil)

	_, err := service.StartOnboarding(context.Background(), uuid.New(), "https://example.com")
	if !errors.Is(err, ErrStripeConnectUnavailable) {
		t.Fatalf("expected ErrStripeConnectUnavailable, got %v", err)
	}
}

func TestStripeConnectService_CompleteOnboarding_InvalidState(t *testing.T) {
	t.Parallel()

	service := NewStripeConnectService(nil, &stripe.PlatformClient{}, nil, nil)

	_, err := service.CompleteOnboarding(context.Background(), "")
	if !errors.Is(err, ErrStripeConnectInvalidState) {
		t.Fatalf("expected ErrStripeConnectInvalidState, got %v", err)
	}
}

func TestStripeConnectService_GetConnectionStatus_Unavailable(t *testing.T) {
	t.Parallel()

	service := NewStripeConnectService(nil, nil, nil, nil)

	_, err := service.GetConnectionStatus(context.Background(), uuid.New())
	if !errors.Is(err, ErrStripeConnectUnavailable) {
		t.Fatalf("expected ErrStripeConnectUnavailable, got %v", err)
	}
}

func TestStripeConnectService_ReconnectOnboarding_Unavailable(t *testing.T) {
	t.Parallel()

	service := NewStripeConnectService(nil, nil, nil, nil)

	_, err := service.ReconnectOnboarding(context.Background(), uuid.New(), "https://example.com")
	if !errors.Is(err, ErrStripeConnectUnavailable) {
		t.Fatalf("expected ErrStripeConnectUnavailable, got %v", err)
	}
}
