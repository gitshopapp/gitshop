// Package stripe provides Stripe Connect functionality.
package stripe

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v84"
)

// PlatformClient handles Stripe Connect platform operations
type PlatformClient struct {
	client   *stripe.Client
	clientID string
	baseURL  string
}

// NewPlatformClient creates a new Stripe Connect client
func NewPlatformClient(secretKey, clientID, baseURL string) *PlatformClient {
	return &PlatformClient{
		client:   stripe.NewClient(secretKey),
		clientID: clientID,
		baseURL:  baseURL,
	}
}

// CreateAccount creates a Standard connected account for a seller
func (c *PlatformClient) CreateAccount(ctx context.Context, country string) (*stripe.Account, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	params := &stripe.AccountCreateParams{
		Type:    stripe.String(string(stripe.AccountTypeStandard)),
		Country: stripe.String(country),
		Capabilities: &stripe.AccountCreateCapabilitiesParams{
			CardPayments: &stripe.AccountCreateCapabilitiesCardPaymentsParams{
				Requested: stripe.Bool(true),
			},
			Transfers: &stripe.AccountCreateCapabilitiesTransfersParams{
				Requested: stripe.Bool(true),
			},
		},
	}

	account, err := c.client.V1Accounts.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create connected account: %w", err)
	}

	return account, nil
}

// CreateAccountLink creates an onboarding link for a connected account
func (c *PlatformClient) CreateAccountLink(ctx context.Context, accountID, returnURL, refreshURL string) (*stripe.AccountLink, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	params := &stripe.AccountLinkCreateParams{
		Account:    stripe.String(accountID),
		RefreshURL: stripe.String(refreshURL),
		ReturnURL:  stripe.String(returnURL),
		Type:       stripe.String(string(stripe.AccountLinkTypeAccountOnboarding)),
	}

	link, err := c.client.V1AccountLinks.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create account link: %w", err)
	}

	return link, nil
}

// GetAccount retrieves a connected account's details
func (c *PlatformClient) GetAccount(ctx context.Context, accountID string) (*stripe.Account, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	account, err := c.client.V1Accounts.GetByID(ctx, accountID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get account: %w", err)
	}
	return account, nil
}

// CreateLoginLink creates a login link for a connected account to access their Stripe dashboard
func (c *PlatformClient) CreateLoginLink(ctx context.Context, accountID string) (*stripe.LoginLink, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	params := &stripe.LoginLinkCreateParams{
		Account: stripe.String(accountID),
	}

	link, err := c.client.V1LoginLinks.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create login link: %w", err)
	}

	return link, nil
}

// CheckoutSessionParams holds parameters for creating a checkout session
type CheckoutSessionParams struct {
	OrderID         uuid.UUID
	ShopID          uuid.UUID
	IssueNumber     int
	RepoFullName    string
	ProductName     string
	UnitPriceCents  int64
	Quantity        int64
	ShippingCents   int64
	ShippingCarrier string
	CustomerEmail   string
	SuccessURL      string
	CancelURL       string
	StripeAccountID string // For Stripe Connect
}

// CreateCheckoutSession creates a checkout session for an order
func (c *PlatformClient) CreateCheckoutSession(ctx context.Context, params CheckoutSessionParams) (*stripe.CheckoutSession, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	if params.Quantity <= 0 {
		params.Quantity = 1
	}

	sessionParams := &stripe.CheckoutSessionCreateParams{
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		Mode:               stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:         stripe.String(params.SuccessURL),
		CancelURL:          stripe.String(params.CancelURL),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
					Currency: stripe.String("usd"),
					ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
						Name: stripe.String(params.ProductName),
					},
					UnitAmount: stripe.Int64(params.UnitPriceCents),
				},
				Quantity: stripe.Int64(params.Quantity),
			},
		},
		ShippingOptions: []*stripe.CheckoutSessionCreateShippingOptionParams{
			{
				ShippingRateData: &stripe.CheckoutSessionCreateShippingOptionShippingRateDataParams{
					DisplayName: stripe.String(fmt.Sprintf("Shipping (%s)", params.ShippingCarrier)),
					Type:        stripe.String(string(stripe.ShippingRateTypeFixedAmount)),
					FixedAmount: &stripe.CheckoutSessionCreateShippingOptionShippingRateDataFixedAmountParams{
						Amount:   stripe.Int64(params.ShippingCents),
						Currency: stripe.String("usd"),
					},
				},
			},
		},
		AutomaticTax: &stripe.CheckoutSessionCreateAutomaticTaxParams{
			Enabled: stripe.Bool(true),
		},
		ShippingAddressCollection: &stripe.CheckoutSessionCreateShippingAddressCollectionParams{
			AllowedCountries: stripe.StringSlice([]string{"US"}),
		},
		// Customer email is optional. Only send if present to avoid Stripe validation errors.
		CustomerEmail: stripe.String(params.CustomerEmail),
		Metadata: map[string]string{
			"order_id":              params.OrderID.String(),
			"shop_id":               params.ShopID.String(),
			"github_issue_number":   fmt.Sprintf("%d", params.IssueNumber),
			"github_repo_full_name": params.RepoFullName,
		},
	}

	if params.CustomerEmail == "" {
		sessionParams.CustomerEmail = nil
	}

	// Use Stripe Connect if shop has connected account
	if params.StripeAccountID != "" {
		sessionParams.SetStripeAccount(params.StripeAccountID)
	}

	sess, err := c.client.V1CheckoutSessions.Create(ctx, sessionParams)
	if err != nil {
		return nil, fmt.Errorf("failed to create checkout session: %w", err)
	}

	return sess, nil
}
