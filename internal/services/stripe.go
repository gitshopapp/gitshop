package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"
	stripeapi "github.com/stripe/stripe-go/v84"

	"github.com/gitshopapp/gitshop/internal/catalog"
	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/githubapp"
	"github.com/gitshopapp/gitshop/internal/logging"
)

type StripeService struct {
	shopStore    *db.ShopStore
	orderStore   *db.OrderStore
	githubClient *githubapp.Client
	parser       configParser
	emailSender  OrderEmailSender
	logger       *slog.Logger
}

func NewStripeService(shopStore *db.ShopStore, orderStore *db.OrderStore, githubClient *githubapp.Client, parser configParser, emailSender OrderEmailSender, logger *slog.Logger) *StripeService {
	if emailSender == nil {
		emailSender = noopOrderEmailSender{}
	}

	return &StripeService{
		shopStore:    shopStore,
		orderStore:   orderStore,
		githubClient: githubClient,
		parser:       parser,
		emailSender:  emailSender,
		logger:       logger,
	}
}

func (s *StripeService) loggerFromContext(ctx context.Context) *slog.Logger {
	return logging.FromContext(ctx, s.logger)
}

type checkoutSessionPayload struct {
	stripeapi.CheckoutSession
	ShippingDetails *stripeapi.ShippingDetails `json:"shipping_details"`
}

func (s *StripeService) HandleCheckoutSessionCompleted(ctx context.Context, payload []byte) error {
	logger := s.loggerFromContext(ctx)

	var session checkoutSessionPayload
	if err := json.Unmarshal(payload, &session); err != nil {
		return fmt.Errorf("invalid event object: %w", err)
	}

	if session.ID == "" {
		return fmt.Errorf("missing session ID")
	}

	orderID, issueNumber, repoFullName, err := parseStripeMetadata(session.Metadata)
	if err != nil {
		return err
	}

	order, err := s.orderStore.GetByStripeSessionID(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("failed to get order: %w", err)
	}

	customerEmail, customerName := extractCustomerDetails(&session)
	shippingAddress := buildShippingAddress(session.ShippingDetails, session.CustomerDetails)

	paymentIntentID := ""
	if session.PaymentIntent != nil {
		paymentIntentID = session.PaymentIntent.ID
	}

	if markErr := s.orderStore.MarkPaid(ctx, orderID, paymentIntentID, customerEmail, customerName, shippingAddress); markErr != nil {
		if errors.Is(markErr, db.ErrInvalidStatusTransition) {
			logger.Info("ignoring checkout.session.completed due to state transition", "order_id", orderID, "session_id", session.ID, "error", markErr)
			return nil
		}
		return fmt.Errorf("failed to mark order as paid: %w", markErr)
	}

	shop, err := s.shopStore.GetByID(ctx, order.ShopID)
	if err != nil {
		logger.Error("failed to get shop", "error", err, "shop_id", order.ShopID)
		return fmt.Errorf("failed to get shop: %w", err)
	}

	githubClient := s.githubClient.WithInstallation(shop.GitHubInstallationID)

	comment := "✅ Payment received! We’re preparing your order now."
	if err := githubClient.CreateComment(ctx, repoFullName, issueNumber, comment); err != nil {
		logger.Error("failed to create payment received comment", "error", err, "repo", repoFullName, "issue", issueNumber)
	}

	if err := githubClient.RemoveLabel(ctx, repoFullName, issueNumber, "gitshop:status:pending-payment"); err != nil {
		logger.Warn("failed to remove pending-payment label", "error", err)
	}

	if err := githubClient.AddLabels(ctx, repoFullName, issueNumber, []string{"gitshop:status:paid"}); err != nil {
		logger.Error("failed to add paid label", "error", err, "repo", repoFullName, "issue", issueNumber)
	}

	s.deleteCheckoutLinkComments(ctx, githubClient, repoFullName, issueNumber)

	if err := s.sendOrderConfirmationEmail(ctx, shop, order, customerEmail, customerName, shippingAddress); err != nil {
		logger.Error("failed to send order confirmation email", "error", err, "order_id", orderID)
		internalIssueTitle := fmt.Sprintf("[GitShop Internal] Email failed for order #%d", order.OrderNumber)
		internalIssueBody := fmt.Sprintf("**Order #%d** on %s\n\n**Error:** Email delivery failed. Check server logs for details.\n\n**Order Issue:** https://github.com/%s/issues/%d", order.OrderNumber, shop.GitHubRepoFullName, repoFullName, issueNumber)
		assignees := s.shopManagerAssignees(ctx, githubClient, repoFullName)
		if createErr := githubClient.CreateIssue(ctx, repoFullName, internalIssueTitle, internalIssueBody, []string{"gitshop-internal", "email-failed"}, assignees); createErr != nil {
			if len(assignees) > 0 {
				logger.Warn("failed to create internal issue with assignee, retrying without assignee", "error", createErr, "repo", repoFullName, "order_id", orderID)
				if retryErr := githubClient.CreateIssue(ctx, repoFullName, internalIssueTitle, internalIssueBody, []string{"gitshop-internal", "email-failed"}, nil); retryErr != nil {
					logger.Error("failed to create internal issue for email failure", "error", retryErr, "repo", repoFullName, "order_id", orderID)
				}
			} else {
				logger.Error("failed to create internal issue for email failure", "error", createErr, "repo", repoFullName, "order_id", orderID)
			}
		}
	}

	return nil
}

func (s *StripeService) HandleCheckoutSessionExpired(ctx context.Context, payload []byte) error {
	logger := s.loggerFromContext(ctx)

	var session checkoutSessionPayload
	if err := json.Unmarshal(payload, &session); err != nil {
		return fmt.Errorf("invalid event object: %w", err)
	}

	if session.ID == "" {
		return fmt.Errorf("missing session ID")
	}

	orderID, issueNumber, repoFullName, err := parseStripeMetadata(session.Metadata)
	if err != nil {
		return err
	}

	order, err := s.orderStore.GetByStripeSessionID(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("failed to get order: %w", err)
	}
	if markErr := s.orderStore.MarkExpired(ctx, order.ID); markErr != nil {
		if errors.Is(markErr, db.ErrInvalidStatusTransition) {
			logger.Info("ignoring checkout.session.expired due to state transition", "order_id", order.ID, "session_id", session.ID, "error", markErr)
			return nil
		}
		return fmt.Errorf("failed to mark order as expired: %w", markErr)
	}

	shop, err := s.shopStore.GetByID(ctx, order.ShopID)
	if err != nil {
		logger.Error("failed to get shop", "error", err, "shop_id", order.ShopID)
		return fmt.Errorf("failed to get shop: %w", err)
	}

	expireComment := "⏰ Your checkout link expired. Please place a new order when you're ready."
	githubClient := s.githubClient.WithInstallation(shop.GitHubInstallationID)
	if err := githubClient.CreateComment(ctx, repoFullName, issueNumber, expireComment); err != nil {
		logger.Error("failed to create expiration comment", "error", err, "repo", repoFullName, "issue", issueNumber)
	}
	if err := githubClient.RemoveLabel(ctx, repoFullName, issueNumber, "gitshop:status:pending-payment"); err != nil {
		logger.Warn("failed to remove pending-payment label", "error", err, "repo", repoFullName, "issue", issueNumber)
	}
	if err := githubClient.AddLabels(ctx, repoFullName, issueNumber, []string{"gitshop:status:expired"}); err != nil {
		logger.Warn("failed to add expired label", "error", err, "repo", repoFullName, "issue", issueNumber)
	}
	s.deleteCheckoutLinkComments(ctx, githubClient, repoFullName, issueNumber)

	logger.Info("checkout session expired handled", "order_id", orderID, "repo", repoFullName, "issue", issueNumber)
	return nil
}

func (s *StripeService) HandlePaymentIntentFailed(ctx context.Context, payload []byte) error {
	logger := s.loggerFromContext(ctx)

	var intent stripeapi.PaymentIntent
	if err := json.Unmarshal(payload, &intent); err != nil {
		return fmt.Errorf("invalid event object: %w", err)
	}

	if intent.ID == "" {
		return fmt.Errorf("missing payment intent ID")
	}

	if len(intent.Metadata) == 0 {
		logger.Info("payment intent missing metadata; skipping", "intent_id", intent.ID)
		return nil
	}

	orderID, issueNumber, repoFullName, err := parseStripeMetadata(intent.Metadata)
	if err != nil {
		return err
	}

	order, err := s.orderStore.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("failed to get order: %w", err)
	}
	if markErr := s.orderStore.MarkFailed(ctx, orderID, "payment_intent_failed"); markErr != nil {
		if errors.Is(markErr, db.ErrInvalidStatusTransition) {
			logger.Info("ignoring payment_intent.payment_failed due to state transition", "order_id", orderID, "intent_id", intent.ID, "error", markErr)
			return nil
		}
		return fmt.Errorf("failed to mark order as payment_failed: %w", markErr)
	}

	shop, err := s.shopStore.GetByID(ctx, order.ShopID)
	if err != nil {
		logger.Error("failed to get shop", "error", err, "shop_id", order.ShopID)
		return fmt.Errorf("failed to get shop: %w", err)
	}

	failComment := "❌ Payment failed. The checkout link is no longer active. Ask the seller for help or add a new comment `.gitshop retry`."
	githubClient := s.githubClient.WithInstallation(shop.GitHubInstallationID)
	if err := githubClient.CreateComment(ctx, repoFullName, issueNumber, failComment); err != nil {
		logger.Error("failed to create payment failure comment", "error", err, "repo", repoFullName, "issue", issueNumber)
	}
	if err := githubClient.RemoveLabel(ctx, repoFullName, issueNumber, "gitshop:status:pending-payment"); err != nil {
		logger.Warn("failed to remove pending-payment label", "error", err, "repo", repoFullName, "issue", issueNumber)
	}
	if err := githubClient.AddLabels(ctx, repoFullName, issueNumber, []string{"gitshop:status:expired"}); err != nil {
		logger.Warn("failed to add expired label", "error", err, "repo", repoFullName, "issue", issueNumber)
	}
	s.deleteCheckoutLinkComments(ctx, githubClient, repoFullName, issueNumber)

	logger.Info("payment failure handled", "order_id", orderID, "repo", repoFullName, "issue", issueNumber)
	return nil
}

func extractCustomerDetails(session *checkoutSessionPayload) (string, string) {
	if session == nil {
		return "", ""
	}

	customerEmail := ""
	customerName := ""

	if session.CustomerDetails != nil {
		customerEmail = session.CustomerDetails.Email
		customerName = session.CustomerDetails.Name
		if customerName == "" {
			customerName = session.CustomerDetails.IndividualName
		}
	}

	if customerEmail == "" {
		customerEmail = session.CustomerEmail
	}

	if customerName == "" && session.ShippingDetails != nil {
		customerName = session.ShippingDetails.Name
	}

	return customerEmail, customerName
}

func buildShippingAddress(details *stripeapi.ShippingDetails, customerDetails *stripeapi.CheckoutSessionCustomerDetails) map[string]any {
	var address *stripeapi.Address
	if details != nil && details.Address != nil {
		address = details.Address
	} else if customerDetails != nil && customerDetails.Address != nil {
		address = customerDetails.Address
	}
	if address == nil {
		return nil
	}

	return map[string]any{
		"line1":       address.Line1,
		"line2":       address.Line2,
		"city":        address.City,
		"state":       address.State,
		"postal_code": address.PostalCode,
		"country":     address.Country,
	}
}

func parseStripeMetadata(metadata map[string]string) (uuid.UUID, int, string, error) {
	if metadata == nil {
		return uuid.Nil, 0, "", fmt.Errorf("missing metadata")
	}

	orderIDStr, ok := metadata["order_id"]
	if !ok || orderIDStr == "" {
		return uuid.Nil, 0, "", fmt.Errorf("missing order_id in metadata")
	}

	orderID, err := uuid.Parse(orderIDStr)
	if err != nil {
		return uuid.Nil, 0, "", fmt.Errorf("invalid order_id: %w", err)
	}

	issueNumberStr, ok := metadata["github_issue_number"]
	if !ok || issueNumberStr == "" {
		return uuid.Nil, 0, "", fmt.Errorf("missing github_issue_number in metadata")
	}

	issueNumber, err := strconv.Atoi(issueNumberStr)
	if err != nil {
		return uuid.Nil, 0, "", fmt.Errorf("invalid github_issue_number: %w", err)
	}

	repoFullName, ok := metadata["github_repo_full_name"]
	if !ok || repoFullName == "" {
		return uuid.Nil, 0, "", fmt.Errorf("missing github_repo_full_name in metadata")
	}

	return orderID, issueNumber, repoFullName, nil
}

func (s *StripeService) deleteCheckoutLinkComments(ctx context.Context, client *githubapp.Client, repoFullName string, issueNumber int) {
	logger := s.loggerFromContext(ctx)
	comments, err := client.ListComments(ctx, repoFullName, issueNumber)
	if err != nil {
		logger.Error("failed to list comments for checkout cleanup", "error", err, "repo", repoFullName, "issue", issueNumber)
		return
	}

	for _, comment := range comments {
		if comment == nil || comment.Body == nil || comment.ID == nil {
			continue
		}
		if strings.Contains(*comment.Body, "gitshop:checkout-link") {
			if err := client.DeleteComment(ctx, repoFullName, *comment.ID); err != nil {
				logger.Error("failed to delete checkout link comment", "error", err, "repo", repoFullName, "issue", issueNumber)
			}
		}
	}
}

func (s *StripeService) sendOrderConfirmationEmail(ctx context.Context, shop *db.Shop, order *db.Order, customerEmail, customerName string, shippingAddress map[string]any) error {
	decodedAddress, err := decodeShippingAddress(shippingAddress)
	if err != nil {
		return err
	}

	addressLines := []string{
		customerName,
	}
	if decodedAddress.Line1 != "" {
		addressLines = append(addressLines, decodedAddress.Line1)
	}
	if decodedAddress.Line2 != "" {
		addressLines = append(addressLines, decodedAddress.Line2)
	}
	city := decodedAddress.City
	state := decodedAddress.State
	postalCode := decodedAddress.PostalCode
	country := decodedAddress.Country
	if city != "" || state != "" || postalCode != "" {
		addressLines = append(addressLines, fmt.Sprintf("%s, %s %s", city, state, postalCode))
	}
	if country != "" {
		addressLines = append(addressLines, country)
	}

	return s.emailSender.SendOrderConfirmation(ctx, shop, order, OrderConfirmationEmailInput{
		CustomerName:    customerName,
		CustomerEmail:   customerEmail,
		ShippingAddress: strings.Join(addressLines, "\n"),
	})
}

type shippingAddressPayload struct {
	Line1      string `json:"line1"`
	Line2      string `json:"line2"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
}

func decodeShippingAddress(shippingAddress map[string]any) (shippingAddressPayload, error) {
	var payload shippingAddressPayload
	if len(shippingAddress) == 0 {
		return payload, nil
	}
	raw, err := json.Marshal(shippingAddress)
	if err != nil {
		return payload, fmt.Errorf("failed to encode shipping address: %w", err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, fmt.Errorf("failed to decode shipping address: %w", err)
	}
	return payload, nil
}

func (s *StripeService) shopManagerAssignees(ctx context.Context, client *githubapp.Client, repoFullName string) []string {
	if client == nil {
		return nil
	}
	configContent, err := s.getGitShopConfigFile(ctx, client, repoFullName)
	if err != nil {
		return nil
	}
	config, err := s.parser.Parse(configContent)
	if err != nil || config == nil {
		return nil
	}
	manager := strings.TrimSpace(config.Shop.Manager)
	if manager == "" {
		return nil
	}
	if !catalog.IsValidGitHubUsername(manager) {
		return nil
	}
	return []string{manager}
}

func (s *StripeService) getGitShopConfigFile(ctx context.Context, client *githubapp.Client, repoFullName string) ([]byte, error) {
	content, err := client.GetFile(ctx, repoFullName, "gitshop.yaml", "")
	if err == nil {
		return content, nil
	}
	content, err = client.GetFile(ctx, repoFullName, "gitshop.yml", "")
	if err == nil {
		return content, nil
	}
	return nil, err
}
