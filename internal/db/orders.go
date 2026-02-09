package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gitshopapp/gitshop/internal/db/queries"
)

type OrderStore struct {
	pool    *pgxpool.Pool
	queries *queries.Queries
}

var ErrInvalidStatusTransition = errors.New("invalid order status transition")

func NewOrderStore(pool *pgxpool.Pool) *OrderStore {
	return &OrderStore{
		pool:    pool,
		queries: queries.New(pool),
	}
}

func (s *OrderStore) Create(ctx context.Context, order *Order) error {
	optionsJSON, err := json.Marshal(order.Options)
	if err != nil {
		return err
	}

	issueNumber, err := intToInt32(order.GitHubIssueNumber, "github issue number")
	if err != nil {
		return err
	}
	subtotalCents, err := intToInt32(order.SubtotalCents, "subtotal cents")
	if err != nil {
		return err
	}
	shippingCents, err := intToInt32(order.ShippingCents, "shipping cents")
	if err != nil {
		return err
	}
	totalCents, err := intToInt32(order.TotalCents, "total cents")
	if err != nil {
		return err
	}

	var shippingAddressJSON []byte
	if order.ShippingAddress != nil {
		shippingAddressJSON, err = json.Marshal(order.ShippingAddress)
		if err != nil {
			return err
		}
	}
	taxCents := pgtype.Int4{Valid: false}
	if order.TaxCents > 0 {
		taxInt32, convErr := intToInt32(order.TaxCents, "tax cents")
		if convErr != nil {
			return convErr
		}
		taxCents = pgtype.Int4{Int32: taxInt32, Valid: true}
	}

	row, err := s.queries.CreateOrder(ctx, queries.CreateOrderParams{
		ShopID:                  order.ShopID,
		GithubIssueNumber:       issueNumber,
		GithubIssueUrl:          pgtype.Text{String: order.GitHubIssueURL, Valid: order.GitHubIssueURL != ""},
		GithubUsername:          order.GitHubUsername,
		Sku:                     order.SKU,
		Options:                 optionsJSON,
		SubtotalCents:           subtotalCents,
		ShippingCents:           shippingCents,
		TaxCents:                taxCents,
		TotalCents:              totalCents,
		StripeCheckoutSessionID: pgtype.Text{String: "", Valid: false},
		CustomerEmail:           pgtype.Text{String: "", Valid: false},
		CustomerName:            pgtype.Text{String: "", Valid: false},
		ShippingAddress:         shippingAddressJSON,
		Status:                  string(order.Status),
	})
	if err != nil {
		return err
	}

	order.ID = row.ID
	order.OrderNumber = int(row.OrderNumber)
	order.CreatedAt = row.CreatedAt.Time
	return nil
}

func (s *OrderStore) GetByStripeSessionID(ctx context.Context, sessionID string) (*Order, error) {
	row, err := s.queries.GetOrderByStripeSessionID(ctx, pgtype.Text{String: sessionID, Valid: true})
	if err != nil {
		return nil, err
	}
	order, err := s.rowToOrder(orderRow{
		ID:                      row.ID,
		ShopID:                  row.ShopID,
		GithubIssueNumber:       row.GithubIssueNumber,
		OrderNumber:             row.OrderNumber,
		GithubIssueUrl:          row.GithubIssueUrl,
		GithubUsername:          row.GithubUsername,
		Sku:                     row.Sku,
		Options:                 row.Options,
		SubtotalCents:           row.SubtotalCents,
		ShippingCents:           row.ShippingCents,
		TaxCents:                row.TaxCents,
		TotalCents:              row.TotalCents,
		StripeCheckoutSessionID: row.StripeCheckoutSessionID,
		StripePaymentIntentID:   row.StripePaymentIntentID,
		CustomerEmail:           row.CustomerEmail,
		CustomerName:            row.CustomerName,
		ShippingAddress:         row.ShippingAddress,
		TrackingNumber:          row.TrackingNumber,
		TrackingUrl:             row.TrackingUrl,
		Carrier:                 row.Carrier,
		Status:                  row.Status,
		CreatedAt:               row.CreatedAt,
		PaidAt:                  row.PaidAt,
		ShippedAt:               row.ShippedAt,
		DeliveredAt:             row.DeliveredAt,
	})
	if err != nil {
		return nil, err
	}
	if err := s.populateFailureReason(ctx, order); err != nil {
		return nil, err
	}
	return order, nil
}

func (s *OrderStore) GetByShopAndIssue(ctx context.Context, shopID uuid.UUID, issueNumber int) (*Order, error) {
	issueNumberInt32, err := intToInt32(issueNumber, "github issue number")
	if err != nil {
		return nil, err
	}

	row, err := s.queries.GetOrderByIssueNumber(ctx, queries.GetOrderByIssueNumberParams{
		ShopID:            shopID,
		GithubIssueNumber: issueNumberInt32,
	})
	if err != nil {
		return nil, err
	}
	order, err := s.rowToOrder(orderRow{
		ID:                      row.ID,
		ShopID:                  row.ShopID,
		GithubIssueNumber:       row.GithubIssueNumber,
		OrderNumber:             row.OrderNumber,
		GithubIssueUrl:          row.GithubIssueUrl,
		GithubUsername:          row.GithubUsername,
		Sku:                     row.Sku,
		Options:                 row.Options,
		SubtotalCents:           row.SubtotalCents,
		ShippingCents:           row.ShippingCents,
		TaxCents:                row.TaxCents,
		TotalCents:              row.TotalCents,
		StripeCheckoutSessionID: row.StripeCheckoutSessionID,
		StripePaymentIntentID:   row.StripePaymentIntentID,
		CustomerEmail:           row.CustomerEmail,
		CustomerName:            row.CustomerName,
		ShippingAddress:         row.ShippingAddress,
		TrackingNumber:          row.TrackingNumber,
		TrackingUrl:             row.TrackingUrl,
		Carrier:                 row.Carrier,
		Status:                  row.Status,
		CreatedAt:               row.CreatedAt,
		PaidAt:                  row.PaidAt,
		ShippedAt:               row.ShippedAt,
		DeliveredAt:             row.DeliveredAt,
	})
	if err != nil {
		return nil, err
	}
	if err := s.populateFailureReason(ctx, order); err != nil {
		return nil, err
	}
	return order, nil
}

func (s *OrderStore) GetByID(ctx context.Context, orderID uuid.UUID) (*Order, error) {
	order, err := s.queries.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	converted, err := s.rowToOrder(orderRow{
		ID:                      order.ID,
		ShopID:                  order.ShopID,
		GithubIssueNumber:       order.GithubIssueNumber,
		OrderNumber:             order.OrderNumber,
		GithubIssueUrl:          order.GithubIssueUrl,
		GithubUsername:          order.GithubUsername,
		Sku:                     order.Sku,
		Options:                 order.Options,
		SubtotalCents:           order.SubtotalCents,
		ShippingCents:           order.ShippingCents,
		TaxCents:                order.TaxCents,
		TotalCents:              order.TotalCents,
		StripeCheckoutSessionID: order.StripeCheckoutSessionID,
		StripePaymentIntentID:   order.StripePaymentIntentID,
		CustomerEmail:           order.CustomerEmail,
		CustomerName:            order.CustomerName,
		ShippingAddress:         order.ShippingAddress,
		TrackingNumber:          order.TrackingNumber,
		TrackingUrl:             order.TrackingUrl,
		Carrier:                 order.Carrier,
		Status:                  order.Status,
		CreatedAt:               order.CreatedAt,
		PaidAt:                  order.PaidAt,
		ShippedAt:               order.ShippedAt,
		DeliveredAt:             order.DeliveredAt,
	})
	if err != nil {
		return nil, err
	}
	if err := s.populateFailureReason(ctx, converted); err != nil {
		return nil, err
	}
	return converted, nil
}

func (s *OrderStore) GetOrdersByShop(ctx context.Context, shopID uuid.UUID, limit int) ([]*Order, error) {
	limitInt32, err := intToInt32(limit, "limit")
	if err != nil {
		return nil, err
	}

	rows, err := s.queries.GetOrdersByShop(ctx, queries.GetOrdersByShopParams{
		ShopID: shopID,
		Limit:  limitInt32,
	})
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, len(rows))
	for i, row := range rows {
		order, err := s.rowToOrder(orderRow{
			ID:                      row.ID,
			ShopID:                  row.ShopID,
			GithubIssueNumber:       row.GithubIssueNumber,
			OrderNumber:             row.OrderNumber,
			GithubIssueUrl:          row.GithubIssueUrl,
			GithubUsername:          row.GithubUsername,
			Sku:                     row.Sku,
			Options:                 row.Options,
			SubtotalCents:           row.SubtotalCents,
			ShippingCents:           row.ShippingCents,
			TaxCents:                row.TaxCents,
			TotalCents:              row.TotalCents,
			StripeCheckoutSessionID: row.StripeCheckoutSessionID,
			StripePaymentIntentID:   row.StripePaymentIntentID,
			CustomerEmail:           row.CustomerEmail,
			CustomerName:            row.CustomerName,
			ShippingAddress:         row.ShippingAddress,
			TrackingNumber:          row.TrackingNumber,
			TrackingUrl:             row.TrackingUrl,
			Carrier:                 row.Carrier,
			Status:                  row.Status,
			CreatedAt:               row.CreatedAt,
			PaidAt:                  row.PaidAt,
			ShippedAt:               row.ShippedAt,
			DeliveredAt:             row.DeliveredAt,
		})
		if err != nil {
			return nil, err
		}
		if err := s.populateFailureReason(ctx, order); err != nil {
			return nil, err
		}
		orders[i] = order
	}

	return orders, nil
}

func (s *OrderStore) UpdateStripeSession(ctx context.Context, orderID uuid.UUID, sessionID string) error {
	// This needs a custom query - adding it to orders.sql would be better
	// For now, using direct pool access
	query := `UPDATE orders SET stripe_checkout_session_id = $1 WHERE id = $2`
	_, err := s.pool.Exec(ctx, query, sessionID, orderID)
	return err
}

func (s *OrderStore) MarkPaid(ctx context.Context, orderID uuid.UUID, paymentIntentID, customerEmail, customerName string, shippingAddress map[string]any) error {
	addressJSON, err := json.Marshal(shippingAddress)
	if err != nil {
		return err
	}

	// Update order with paid status
	query := `
		UPDATE orders
		SET status = $1, stripe_payment_intent_id = $2, customer_email = $3,
		    customer_name = $4, shipping_address = $5, paid_at = NOW(), failure_reason = NULL
		WHERE id = $6 AND status IN ('pending_payment', 'payment_failed', 'paid')
	`
	cmdTag, err := s.pool.Exec(ctx, query, StatusPaid, paymentIntentID, customerEmail, customerName, addressJSON, orderID)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: expected pending_payment/payment_failed/paid", ErrInvalidStatusTransition)
	}
	return nil
}

func (s *OrderStore) MarkShipped(ctx context.Context, orderID uuid.UUID, trackingNumber, carrier string) error {
	query := `
		UPDATE orders
		SET status = $1, tracking_number = $2, carrier = $3, shipped_at = NOW()
		WHERE id = $4 AND status = 'paid'
	`
	cmdTag, err := s.pool.Exec(ctx, query, StatusShipped, trackingNumber, carrier, orderID)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: expected paid", ErrInvalidStatusTransition)
	}
	return nil
}

func (s *OrderStore) UpdateShipmentDetails(ctx context.Context, orderID uuid.UUID, trackingNumber, carrier string) error {
	query := `
		UPDATE orders
		SET tracking_number = $1, carrier = $2
		WHERE id = $3 AND status = 'shipped'
	`
	cmdTag, err := s.pool.Exec(ctx, query, trackingNumber, carrier, orderID)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: expected shipped", ErrInvalidStatusTransition)
	}
	return nil
}

func (s *OrderStore) MarkShippedWithoutTracking(ctx context.Context, orderID uuid.UUID) error {
	query := `
		UPDATE orders
		SET status = $1, shipped_at = NOW()
		WHERE id = $2 AND status = 'paid'
	`
	cmdTag, err := s.pool.Exec(ctx, query, StatusShipped, orderID)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: expected paid", ErrInvalidStatusTransition)
	}
	return nil
}

func (s *OrderStore) MarkDelivered(ctx context.Context, orderID uuid.UUID) error {
	query := `
		UPDATE orders
		SET status = $1, delivered_at = NOW()
		WHERE id = $2 AND status = 'shipped'
	`
	cmdTag, err := s.pool.Exec(ctx, query, StatusDelivered, orderID)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: expected shipped", ErrInvalidStatusTransition)
	}
	return nil
}

func (s *OrderStore) MarkFailed(ctx context.Context, orderID uuid.UUID, reason string) error {
	query := `
		UPDATE orders
		SET status = $1, failure_reason = $3
		WHERE id = $2 AND status IN ('pending_payment', 'payment_failed')
	`
	cmdTag, err := s.pool.Exec(ctx, query, StatusPaymentFailed, orderID, reason)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: expected pending_payment/payment_failed", ErrInvalidStatusTransition)
	}
	return nil
}

func (s *OrderStore) MarkPendingPayment(ctx context.Context, orderID uuid.UUID, sessionID string) error {
	query := `
		UPDATE orders
		SET status = $1, stripe_checkout_session_id = $2, failure_reason = NULL
		WHERE id = $3 AND status IN ('payment_failed', 'pending_payment')
	`
	cmdTag, err := s.pool.Exec(ctx, query, StatusPendingPayment, sessionID, orderID)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: expected payment_failed/pending_payment", ErrInvalidStatusTransition)
	}
	return nil
}

func (s *OrderStore) MarkExpired(ctx context.Context, orderID uuid.UUID) error {
	query := `
		UPDATE orders
		SET status = $1
		WHERE id = $2 AND status = 'pending_payment'
	`
	cmdTag, err := s.pool.Exec(ctx, query, StatusExpired, orderID)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("%w: expected pending_payment", ErrInvalidStatusTransition)
	}
	return nil
}

type orderRow struct {
	ID                      uuid.UUID
	ShopID                  uuid.UUID
	GithubIssueNumber       int32
	OrderNumber             int32
	GithubIssueUrl          pgtype.Text
	GithubUsername          string
	Sku                     string
	Options                 []byte
	SubtotalCents           int32
	ShippingCents           int32
	TaxCents                pgtype.Int4
	TotalCents              int32
	StripeCheckoutSessionID pgtype.Text
	StripePaymentIntentID   pgtype.Text
	CustomerEmail           pgtype.Text
	CustomerName            pgtype.Text
	ShippingAddress         []byte
	TrackingNumber          pgtype.Text
	TrackingUrl             pgtype.Text
	Carrier                 pgtype.Text
	Status                  string
	CreatedAt               pgtype.Timestamptz
	PaidAt                  pgtype.Timestamptz
	ShippedAt               pgtype.Timestamptz
	DeliveredAt             pgtype.Timestamptz
}

func (s *OrderStore) rowToOrder(row orderRow) (*Order, error) {
	order := &Order{
		ID:                row.ID,
		ShopID:            row.ShopID,
		GitHubIssueNumber: int(row.GithubIssueNumber),
		OrderNumber:       int(row.OrderNumber),
		GitHubUsername:    row.GithubUsername,
		SKU:               row.Sku,
		SubtotalCents:     int(row.SubtotalCents),
		ShippingCents:     int(row.ShippingCents),
		TotalCents:        int(row.TotalCents),
		Status:            OrderStatus(row.Status),
		CreatedAt:         row.CreatedAt.Time,
	}

	if row.GithubIssueUrl.Valid {
		order.GitHubIssueURL = row.GithubIssueUrl.String
	}
	if row.TaxCents.Valid {
		order.TaxCents = int(row.TaxCents.Int32)
	}
	if row.StripeCheckoutSessionID.Valid {
		order.StripeCheckoutSessionID = row.StripeCheckoutSessionID.String
	}
	if row.StripePaymentIntentID.Valid {
		order.StripePaymentIntentID = row.StripePaymentIntentID.String
	}
	if row.CustomerEmail.Valid {
		order.CustomerEmail = row.CustomerEmail.String
	}
	if row.CustomerName.Valid {
		order.CustomerName = row.CustomerName.String
	}
	if row.TrackingNumber.Valid {
		order.TrackingNumber = row.TrackingNumber.String
	}
	if row.TrackingUrl.Valid {
		order.TrackingURL = row.TrackingUrl.String
	}
	if row.Carrier.Valid {
		order.Carrier = row.Carrier.String
	}
	if row.PaidAt.Valid {
		order.PaidAt = row.PaidAt.Time
	}
	if row.ShippedAt.Valid {
		order.ShippedAt = row.ShippedAt.Time
	}
	if row.DeliveredAt.Valid {
		order.DeliveredAt = row.DeliveredAt.Time
	}

	if row.Options != nil {
		if err := json.Unmarshal(row.Options, &order.Options); err != nil {
			return nil, err
		}
	}

	if row.ShippingAddress != nil {
		if err := json.Unmarshal(row.ShippingAddress, &order.ShippingAddress); err != nil {
			return nil, err
		}
	}

	return order, nil
}

func (s *OrderStore) populateFailureReason(ctx context.Context, order *Order) error {
	if order == nil {
		return nil
	}
	var failureReason pgtype.Text
	if err := s.pool.QueryRow(ctx, "SELECT failure_reason FROM orders WHERE id = $1", order.ID).Scan(&failureReason); err != nil {
		return err
	}
	if failureReason.Valid {
		order.FailureReason = failureReason.String
	}
	return nil
}

func intToInt32(value int, name string) (int32, error) {
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, fmt.Errorf("%s out of int32 range: %d", name, value)
	}
	return int32(value), nil
}
