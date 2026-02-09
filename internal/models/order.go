package models

import (
	"time"

	"github.com/google/uuid"
)

type OrderStatus string

const (
	StatusPendingPayment OrderStatus = "pending_payment"
	StatusPaid           OrderStatus = "paid"
	StatusPaymentFailed  OrderStatus = "payment_failed"
	StatusExpired        OrderStatus = "expired"
	StatusShipped        OrderStatus = "shipped"
	StatusDelivered      OrderStatus = "delivered"
	StatusRefunded       OrderStatus = "refunded"
)

type Order struct {
	ID                      uuid.UUID      `json:"id"`
	ShopID                  uuid.UUID      `json:"shop_id"`
	GitHubIssueNumber       int            `json:"github_issue_number"`
	OrderNumber             int            `json:"order_number"`
	GitHubIssueURL          string         `json:"github_issue_url"`
	GitHubUsername          string         `json:"github_username"`
	SKU                     string         `json:"sku"`
	Options                 map[string]any `json:"options"`
	SubtotalCents           int            `json:"subtotal_cents"`
	ShippingCents           int            `json:"shipping_cents"`
	TaxCents                int            `json:"tax_cents"`
	TotalCents              int            `json:"total_cents"`
	StripeCheckoutSessionID string         `json:"stripe_checkout_session_id"`
	StripePaymentIntentID   string         `json:"stripe_payment_intent_id"`
	CustomerEmail           string         `json:"customer_email"`
	CustomerName            string         `json:"customer_name"`
	ShippingAddress         map[string]any `json:"shipping_address"`
	TrackingNumber          string         `json:"tracking_number"`
	TrackingURL             string         `json:"tracking_url"`
	Carrier                 string         `json:"carrier"`
	FailureReason           string         `json:"failure_reason"`
	Status                  OrderStatus    `json:"status"`
	CreatedAt               time.Time      `json:"created_at"`
	PaidAt                  time.Time      `json:"paid_at"`
	ShippedAt               time.Time      `json:"shipped_at"`
	DeliveredAt             time.Time      `json:"delivered_at"`
}
