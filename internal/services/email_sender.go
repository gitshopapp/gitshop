package services

import (
	"context"
	"fmt"

	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/email"
)

type OrderEmailSender interface {
	SendOrderConfirmation(ctx context.Context, shop *db.Shop, order *db.Order, input OrderConfirmationEmailInput) error
	SendOrderShipped(ctx context.Context, shop *db.Shop, order *db.Order, input OrderShipmentEmailInput) error
	SendOrderDelivered(ctx context.Context, shop *db.Shop, order *db.Order) error
}

type OrderConfirmationEmailInput struct {
	CustomerName    string
	CustomerEmail   string
	ShippingAddress string
}

type OrderShipmentEmailInput struct {
	TrackingNumber  string
	TrackingURL     string
	TrackingCarrier string
}

type ShopEmailProviderFactory func(shop *db.Shop) (email.Provider, error)

type ShopOrderEmailSender struct {
	providerFromShop ShopEmailProviderFactory
}

func NewShopOrderEmailSender(providerFromShop ShopEmailProviderFactory) *ShopOrderEmailSender {
	if providerFromShop == nil {
		providerFromShop = email.NewProviderFromShop
	}
	return &ShopOrderEmailSender{
		providerFromShop: providerFromShop,
	}
}

func (s *ShopOrderEmailSender) SendOrderConfirmation(ctx context.Context, shop *db.Shop, order *db.Order, input OrderConfirmationEmailInput) error {
	provider, err := s.provider(shop)
	if err != nil {
		return err
	}

	orderInfo := BuildOrderInfo(shop, order, OrderInfoOverrides{
		CustomerName:    input.CustomerName,
		CustomerEmail:   input.CustomerEmail,
		ShippingAddress: input.ShippingAddress,
	})

	return email.SendOrderConfirmation(ctx, provider, orderInfo)
}

func (s *ShopOrderEmailSender) SendOrderShipped(ctx context.Context, shop *db.Shop, order *db.Order, input OrderShipmentEmailInput) error {
	provider, err := s.provider(shop)
	if err != nil {
		return err
	}

	orderInfo := BuildOrderInfo(shop, order, OrderInfoOverrides{
		TrackingNumber:  input.TrackingNumber,
		TrackingURL:     input.TrackingURL,
		TrackingCarrier: input.TrackingCarrier,
	})

	return email.SendOrderShipped(ctx, provider, orderInfo)
}

func (s *ShopOrderEmailSender) SendOrderDelivered(ctx context.Context, shop *db.Shop, order *db.Order) error {
	provider, err := s.provider(shop)
	if err != nil {
		return err
	}

	orderInfo := BuildOrderInfo(shop, order, OrderInfoOverrides{})

	return email.SendOrderDelivered(ctx, provider, orderInfo)
}

func (s *ShopOrderEmailSender) provider(shop *db.Shop) (email.Provider, error) {
	if shop == nil {
		return nil, fmt.Errorf("shop is required")
	}
	if s == nil || s.providerFromShop == nil {
		return nil, fmt.Errorf("email provider factory is not configured")
	}

	provider, err := s.providerFromShop(shop)
	if err != nil {
		return nil, fmt.Errorf("failed to get email provider: %w", err)
	}

	return provider, nil
}

type noopOrderEmailSender struct{}

func (noopOrderEmailSender) SendOrderConfirmation(context.Context, *db.Shop, *db.Order, OrderConfirmationEmailInput) error {
	return nil
}

func (noopOrderEmailSender) SendOrderShipped(context.Context, *db.Shop, *db.Order, OrderShipmentEmailInput) error {
	return nil
}

func (noopOrderEmailSender) SendOrderDelivered(context.Context, *db.Shop, *db.Order) error {
	return nil
}
