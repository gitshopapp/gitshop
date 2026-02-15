package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/google/go-github/v66/github"
	"github.com/jackc/pgx/v5"

	"github.com/gitshopapp/gitshop/internal/catalog"
	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/githubapp"
	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/observability"
	"github.com/gitshopapp/gitshop/internal/stripe"
)

type OrderService struct {
	shopStore      *db.ShopStore
	orderStore     *db.OrderStore
	githubClient   *githubapp.Client
	stripePlatform *stripe.PlatformClient
	parser         configParser
	validator      configValidator
	pricer         orderPricer
	emailSender    OrderEmailSender
	logger         *slog.Logger
}

type configParser interface {
	Parse(content []byte) (*catalog.GitShopConfig, error)
}

type configValidator interface {
	Validate(config *catalog.GitShopConfig) error
}

type orderPricer interface {
	ComputeSubtotal(config *catalog.GitShopConfig, sku string, options map[string]any) (int, error)
	GetShippingCents(config *catalog.GitShopConfig) int
}

func NewOrderService(shopStore *db.ShopStore, orderStore *db.OrderStore, githubClient *githubapp.Client, stripePlatform *stripe.PlatformClient, parser configParser, validator configValidator, pricer orderPricer, emailSender OrderEmailSender, logger *slog.Logger) *OrderService {
	if emailSender == nil {
		emailSender = noopOrderEmailSender{}
	}

	return &OrderService{
		shopStore:      shopStore,
		orderStore:     orderStore,
		githubClient:   githubClient,
		stripePlatform: stripePlatform,
		parser:         parser,
		validator:      validator,
		pricer:         pricer,
		emailSender:    emailSender,
		logger:         logger,
	}
}

func (s *OrderService) loggerFromContext(ctx context.Context) *slog.Logger {
	return logging.FromContext(ctx, s.logger)
}

type IssueOpenedInput struct {
	InstallationID int64
	RepoID         int64
	RepoFullName   string
	IssueNumber    int
	IssueURL       string
	IssueTitle     string
	IssueUsername  string
	IssueBody      string
}

type IssueCommentCreatedInput struct {
	InstallationID int64
	RepoID         int64
	RepoFullName   string
	IssueNumber    int
	CommentBody    string
	CommenterLogin string
}

func (s *OrderService) HandleIssueOpened(ctx context.Context, input IssueOpenedInput) error {
	span := sentry.StartSpan(
		ctx,
		"service.order.handle_issue_opened",
		sentry.WithOpName("service.order"),
		sentry.WithDescription("HandleIssueOpened"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	logger := s.loggerFromContext(ctx)
	meter := observability.MeterFromContext(ctx)
	meter.SetAttributes(attribute.String("source", "issue_opened"))
	recordFailure := func(reason string) {
		meter.Count("order.intake.failed", 1, sentry.WithAttributes(
			attribute.String("reason", reason),
		))
	}
	meter.Count("order.intake.received", 1)

	githubClient := s.githubClient.WithInstallation(input.InstallationID)

	shop, err := s.shopStore.GetByInstallationAndRepoID(ctx, input.InstallationID, input.RepoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			shop, err = s.shopStore.Create(ctx, input.InstallationID, input.RepoID, input.RepoFullName, "")
		}
		if err == nil {
			logger.Info("created shop from issue webhook", "installation_id", input.InstallationID, "repo_id", input.RepoID, "shop_id", shop.ID)
		}
	}
	if err != nil {
		recordFailure("shop_lookup_failed")
		return fmt.Errorf("failed to get shop: %w", err)
	}

	if !shop.IsConnected() {
		recordFailure("shop_disconnected")
		return fmt.Errorf("shop is disconnected, cannot process orders: %s", input.RepoFullName)
	}
	if shop.StripeConnectAccountID == "" {
		recordFailure("stripe_not_connected")
		comment := s.appendManagerMention(ctx, githubClient, input.RepoFullName, "⚠️ Payments are not ready yet for this storefront. Ask the shop owner to complete Stripe setup in the GitShop dashboard.")
		if commentErr := githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber, comment); commentErr != nil {
			logger.Warn("failed to create stripe-not-connected comment", "error", commentErr, "repo", input.RepoFullName, "issue", input.IssueNumber)
		}
		return fmt.Errorf("stripe not connected for shop: %s", shop.ID.String())
	}
	if s.stripePlatform == nil {
		recordFailure("stripe_unavailable")
		comment := s.appendManagerMention(ctx, githubClient, input.RepoFullName, "⚠️ Payments are temporarily unavailable for this GitShop instance.")
		if commentErr := githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber, comment); commentErr != nil {
			logger.Warn("failed to create stripe-unavailable comment", "error", commentErr, "repo", input.RepoFullName, "issue", input.IssueNumber)
		}
		return fmt.Errorf("stripe platform not configured")
	}

	if shop.GitHubRepoFullName != input.RepoFullName {
		logger.Info("repository name changed, updating shop",
			"shop_id", shop.ID,
			"old_name", shop.GitHubRepoFullName,
			"new_name", input.RepoFullName)
		if updateErr := s.shopStore.UpdateRepoFullName(ctx, shop.ID, input.RepoFullName); updateErr != nil {
			logger.Error("failed to update repo full name", "error", updateErr, "shop_id", shop.ID)
		}
	}

	orderData, err := parseOrderFromIssue(input.IssueBody)
	if err != nil {
		recordFailure("order_parse_failed")
		comment := fmt.Sprintf(`❌ **Order Error**

%s

**How to fix:**
1. Use the order template by clicking "New Issue" → "Place an Order"
2. Fill in all required fields
3. Make sure to select a product from the dropdown

Need help? Check our [documentation](https://github.com/%s/blob/main/README.md) or open a support issue.`, err.Error(), input.RepoFullName)

		if createErr := githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber, comment); createErr != nil {
			logger.Error("failed to create error comment", "error", createErr)
		}
		return fmt.Errorf("failed to parse order: %w", err)
	}

	configContent, err := s.getGitShopConfigFile(ctx, githubClient, input.RepoFullName)
	if err != nil {
		recordFailure("config_missing")
		comment := s.appendManagerMention(ctx, githubClient, input.RepoFullName, "❌ Could not find `gitshop.yaml` in the repo. Create it in the repo root to enable ordering.")
		if commentErr := githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber, comment); commentErr != nil {
			logger.Warn("failed to create missing-config comment", "error", commentErr, "repo", input.RepoFullName, "issue", input.IssueNumber)
		}
		return fmt.Errorf("failed to fetch gitshop.yaml: %w", err)
	}

	config, err := s.parser.Parse(configContent)
	if err != nil {
		recordFailure("config_parse_failed")
		return fmt.Errorf("failed to parse gitshop.yaml: %w", err)
	}

	validateErr := s.validator.Validate(config)
	if validateErr != nil {
		recordFailure("config_invalid")
		comment := s.appendManagerMention(ctx, githubClient, input.RepoFullName, fmt.Sprintf("❌ `gitshop.yaml` is invalid: %s\n\nFix the file and try again.", validateErr.Error()))
		if commentErr := githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber, comment); commentErr != nil {
			logger.Warn("failed to create invalid-config comment", "error", commentErr, "repo", input.RepoFullName, "issue", input.IssueNumber)
		}
		return fmt.Errorf("invalid gitshop.yaml: %w", validateErr)
	}
	s.assignShopManager(ctx, githubClient, input.RepoFullName, input.IssueNumber, config)

	subtotalCents, err := s.pricer.ComputeSubtotal(config, orderData.SKU, orderData.Options)
	if err != nil {
		recordFailure("pricing_failed")
		comment := s.appendManagerMention(ctx, githubClient, input.RepoFullName, fmt.Sprintf("❌ We couldn't price this order yet: %s", err.Error()))
		if commentErr := githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber, comment); commentErr != nil {
			logger.Warn("failed to create pricing-error comment", "error", commentErr, "repo", input.RepoFullName, "issue", input.IssueNumber)
		}
		return fmt.Errorf("failed to compute subtotal: %w", err)
	}

	shippingCents := s.pricer.GetShippingCents(config)

	product := findProduct(config, orderData.SKU)
	if product == nil {
		recordFailure("sku_missing")
		comment := s.appendManagerMention(ctx, githubClient, input.RepoFullName, fmt.Sprintf("❌ SKU `%s` not found in `gitshop.yaml`. Update the file and try again.", orderData.SKU))
		if commentErr := githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber, comment); commentErr != nil {
			logger.Warn("failed to create missing-sku comment", "error", commentErr, "repo", input.RepoFullName, "issue", input.IssueNumber)
		}
		return fmt.Errorf("sku not found: %s", orderData.SKU)
	}

	order := &db.Order{
		ShopID:            shop.ID,
		GitHubIssueNumber: input.IssueNumber,
		OrderNumber:       input.IssueNumber,
		GitHubIssueURL:    input.IssueURL,
		GitHubUsername:    input.IssueUsername,
		SKU:               orderData.SKU,
		Options:           orderData.Options,
		SubtotalCents:     subtotalCents,
		ShippingCents:     shippingCents,
		TotalCents:        subtotalCents + shippingCents,
		Status:            db.StatusPendingPayment,
	}

	createErr := s.orderStore.Create(ctx, order)
	if createErr != nil {
		recordFailure("order_create_failed")
		return fmt.Errorf("failed to create order: %w", createErr)
	}
	meter.Count("order.created", 1)

	quantity := int64(orderQuantity(orderData.Options))
	checkoutParams := stripe.CheckoutSessionParams{
		OrderID:         order.ID,
		ShopID:          shop.ID,
		IssueNumber:     input.IssueNumber,
		RepoFullName:    input.RepoFullName,
		ProductName:     product.Name,
		UnitPriceCents:  int64(product.UnitPriceCents),
		Quantity:        quantity,
		ShippingCents:   int64(shippingCents),
		ShippingCarrier: config.Shop.Shipping.Carrier,
		CustomerEmail:   "",
		SuccessURL:      fmt.Sprintf("https://github.com/%s/issues/%d", input.RepoFullName, input.IssueNumber),
		CancelURL:       fmt.Sprintf("https://github.com/%s/issues/%d", input.RepoFullName, input.IssueNumber),
		StripeAccountID: shop.StripeConnectAccountID,
	}

	session, err := s.stripePlatform.CreateCheckoutSession(ctx, checkoutParams)
	if err != nil {
		recordFailure("checkout_create_failed")
		meter.Count("checkout.session.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "create_failed"),
		))
		if markErr := s.orderStore.MarkFailed(ctx, order.ID, "stripe_checkout_failed"); markErr != nil {
			logger.Warn("failed to mark order failed after checkout error", "error", markErr, "order_id", order.ID)
		}
		failComment := s.appendManagerMention(ctx, githubClient, input.RepoFullName, "⚠️ Thanks for your order. We couldn't create a checkout link right now.\n\nAsk the shop owner for help or add a new comment `.gitshop retry` to try again.")
		if commentErr := githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber, failComment); commentErr != nil {
			logger.Warn("failed to create checkout-failed comment", "error", commentErr, "repo", input.RepoFullName, "issue", input.IssueNumber)
		}
		return fmt.Errorf("failed to create checkout session: %w", err)
	}

	if err := s.orderStore.UpdateStripeSession(ctx, order.ID, session.ID); err != nil {
		recordFailure("order_update_stripe_session_failed")
		return fmt.Errorf("failed to update order with session ID: %w", err)
	}

	comment := fmt.Sprintf("🛍️ Thanks for your order! Complete payment here: %s\n\nThis checkout link expires in 30 minutes.\n\n<!-- gitshop:checkout-link -->", session.URL)
	if err := githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber, comment); err != nil {
		recordFailure("checkout_comment_failed")
		return fmt.Errorf("failed to create comment: %w", err)
	}

	s.ensureIssueNumberInTitle(ctx, githubClient, input.RepoFullName, input.IssueNumber, input.IssueTitle)

	if err := githubClient.AddLabels(ctx, input.RepoFullName, input.IssueNumber, []string{"gitshop:status:pending-payment"}); err != nil {
		recordFailure("label_add_failed")
		return fmt.Errorf("failed to add label: %w", err)
	}
	meter.Count("checkout.session.created", 1)

	return nil
}

func (s *OrderService) HandleIssueCommentCreated(ctx context.Context, input IssueCommentCreatedInput) error {
	span := sentry.StartSpan(
		ctx,
		"service.order.handle_issue_comment_created",
		sentry.WithOpName("service.order"),
		sentry.WithDescription("HandleIssueCommentCreated"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := observability.MeterFromContext(ctx)
	commentBody := strings.TrimSpace(input.CommentBody)
	if commentBody != ".gitshop retry" {
		return nil
	}
	meter.Count("order.retry.received", 1, sentry.WithAttributes(
		attribute.String("source", "issue_comment"),
	))

	githubClient := s.githubClient.WithInstallation(input.InstallationID)

	hasPermission := false
	permission, err := githubClient.CheckPermission(ctx, input.RepoFullName, input.CommenterLogin)
	if err != nil {
		s.loggerFromContext(ctx).Warn("failed to check permission for retry", "error", err, "repo", input.RepoFullName, "commenter", input.CommenterLogin)
	} else {
		hasPermission = permission
	}
	shop, err := s.shopStore.GetByInstallationAndRepoID(ctx, input.InstallationID, input.RepoID)
	if err != nil {
		meter.Count("order.retry.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "shop_lookup_failed"),
		))
		return fmt.Errorf("failed to get shop: %w", err)
	}

	if !shop.IsConnected() {
		meter.Count("order.retry.rejected", 1, sentry.WithAttributes(
			attribute.String("reason", "shop_disconnected"),
		))
		return githubClient.CreateComment(ctx, input.RepoFullName, input.IssueNumber,
			"❌ This shop is currently disconnected. Please reconnect the GitHub App to use GitShop commands.")
	}

	order, err := s.orderStore.GetByShopAndIssue(ctx, shop.ID, input.IssueNumber)
	if err != nil {
		meter.Count("order.retry.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "order_lookup_failed"),
		))
		return fmt.Errorf("failed to get order: %w", err)
	}

	return s.executeCommand(ctx, githubClient, input.RepoFullName, input.IssueNumber, order, commentBody, input.CommenterLogin, hasPermission, shop)
}

func (s *OrderService) executeCommand(ctx context.Context, client *githubapp.Client, repoFullName string, issueNumber int, order *db.Order, commentBody, commenterLogin string, hasPermission bool, shop *db.Shop) error {
	if commentBody == ".gitshop retry" {
		return s.handleRetryCommand(ctx, client, repoFullName, issueNumber, order, commenterLogin, hasPermission, shop)
	}

	return nil
}

func (s *OrderService) handleRetryCommand(ctx context.Context, client *githubapp.Client, repoFullName string, issueNumber int, order *db.Order, commenterLogin string, hasPermission bool, shop *db.Shop) error {
	span := sentry.StartSpan(
		ctx,
		"service.order.handle_retry_command",
		sentry.WithOpName("service.order"),
		sentry.WithDescription("handleRetryCommand"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	meter := observability.MeterFromContext(ctx)
	if order == nil || shop == nil {
		meter.Count("order.retry.rejected", 1, sentry.WithAttributes(
			attribute.String("reason", "order_not_found"),
		))
		return client.CreateComment(ctx, repoFullName, issueNumber, "❌ Order not found.")
	}

	if !hasPermission && commenterLogin != order.GitHubUsername {
		meter.Count("order.retry.rejected", 1, sentry.WithAttributes(
			attribute.String("reason", "permission_denied"),
		))
		return client.CreateComment(ctx, repoFullName, issueNumber, "❌ Only the issue author or a repo admin can retry order creation.")
	}

	if order.Status != db.StatusPaymentFailed {
		meter.Count("order.retry.rejected", 1, sentry.WithAttributes(
			attribute.String("reason", "invalid_order_status"),
		))
		return client.CreateComment(ctx, repoFullName, issueNumber, "⚠️ This order doesn't need a retry right now.")
	}

	if s.stripePlatform == nil || shop.StripeConnectAccountID == "" {
		meter.Count("order.retry.rejected", 1, sentry.WithAttributes(
			attribute.String("reason", "stripe_unavailable"),
		))
		return client.CreateComment(ctx, repoFullName, issueNumber, s.appendManagerMention(ctx, client, repoFullName, "❌ Stripe is not connected for this shop yet."))
	}

	configContent, err := s.getGitShopConfigFile(ctx, client, repoFullName)
	if err != nil {
		meter.Count("order.retry.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "config_missing"),
		))
		return client.CreateComment(ctx, repoFullName, issueNumber, s.appendManagerMention(ctx, client, repoFullName, "❌ `gitshop.yaml` is missing. Fix it before retrying."))
	}

	config, err := s.parser.Parse(configContent)
	if err != nil {
		meter.Count("order.retry.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "config_invalid"),
		))
		return client.CreateComment(ctx, repoFullName, issueNumber, s.appendManagerMention(ctx, client, repoFullName, "❌ `gitshop.yaml` is invalid. Fix it before retrying."))
	}

	if validateErr := s.validator.Validate(config); validateErr != nil {
		meter.Count("order.retry.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "config_invalid"),
		))
		return client.CreateComment(ctx, repoFullName, issueNumber, s.appendManagerMention(ctx, client, repoFullName, "❌ `gitshop.yaml` is invalid. Fix it before retrying."))
	}

	product := findProduct(config, order.SKU)
	if product == nil {
		meter.Count("order.retry.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "sku_missing"),
		))
		return client.CreateComment(ctx, repoFullName, issueNumber, s.appendManagerMention(ctx, client, repoFullName, "❌ SKU not found in `gitshop.yaml`. Update the file and retry."))
	}

	quantity := int64(orderQuantity(order.Options))
	checkoutParams := stripe.CheckoutSessionParams{
		OrderID:         order.ID,
		ShopID:          shop.ID,
		IssueNumber:     issueNumber,
		RepoFullName:    repoFullName,
		ProductName:     product.Name,
		UnitPriceCents:  int64(product.UnitPriceCents),
		Quantity:        quantity,
		ShippingCents:   int64(order.ShippingCents),
		ShippingCarrier: config.Shop.Shipping.Carrier,
		CustomerEmail:   "",
		SuccessURL:      fmt.Sprintf("https://github.com/%s/issues/%d", repoFullName, issueNumber),
		CancelURL:       fmt.Sprintf("https://github.com/%s/issues/%d", repoFullName, issueNumber),
		StripeAccountID: shop.StripeConnectAccountID,
	}

	session, err := s.stripePlatform.CreateCheckoutSession(ctx, checkoutParams)
	if err != nil {
		meter.Count("order.retry.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "checkout_create_failed"),
		))
		meter.Count("checkout.session.failed", 1, sentry.WithAttributes(
			attribute.String("source", "retry"),
			attribute.String("reason", "create_failed"),
		))
		return client.CreateComment(ctx, repoFullName, issueNumber, s.appendManagerMention(ctx, client, repoFullName, "❌ Retry failed to create a checkout link. Please try again later."))
	}

	if err := s.orderStore.MarkPendingPayment(ctx, order.ID, session.ID); err != nil {
		meter.Count("order.retry.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "mark_pending_failed"),
		))
		return fmt.Errorf("failed to update order after retry: %w", err)
	}

	comment := fmt.Sprintf("🛍️ Thanks for your order! Complete payment here: %s\n\nThis checkout link expires in 30 minutes.\n\n<!-- gitshop:checkout-link -->", session.URL)
	if err := client.CreateComment(ctx, repoFullName, issueNumber, comment); err != nil {
		meter.Count("order.retry.failed", 1, sentry.WithAttributes(
			attribute.String("reason", "checkout_comment_failed"),
		))
		return fmt.Errorf("failed to comment checkout link: %w", err)
	}
	meter.Count("order.retry.succeeded", 1, sentry.WithAttributes(
		attribute.String("source", "issue_comment"),
	))
	meter.Count("checkout.session.created", 1, sentry.WithAttributes(
		attribute.String("source", "retry"),
	))

	return nil
}

func (s *OrderService) appendManagerMention(ctx context.Context, client *githubapp.Client, repoFullName, message string) string {
	mention := s.shopManagerMention(ctx, client, repoFullName)
	if mention == "" {
		return message
	}
	return message + "\n\nShop manager: " + mention
}

func (s *OrderService) shopManagerMention(ctx context.Context, client *githubapp.Client, repoFullName string) string {
	if client == nil || repoFullName == "" {
		return ""
	}
	content, err := s.getGitShopConfigFile(ctx, client, repoFullName)
	if err != nil {
		return ""
	}
	config, err := s.parser.Parse(content)
	if err != nil || config == nil {
		return ""
	}
	manager := strings.TrimSpace(config.Shop.Manager)
	if manager == "" || !catalog.IsValidGitHubUsername(manager) {
		return ""
	}
	return "@" + manager
}

func IsOrderIssue(issue *github.Issue) bool {
	if issue == nil {
		return false
	}

	for _, label := range issue.Labels {
		if label == nil {
			continue
		}
		if label.GetName() == "gitshop:order" {
			return true
		}
	}

	body := issue.GetBody()
	return strings.Contains(body, "gitshop:order-template")
}

type OrderData struct {
	SKU     string         `json:"sku"`
	Options map[string]any `json:"options"`
}

func parseOrderFromIssue(body string) (*OrderData, error) {
	sku := ""
	options := make(map[string]any)

	lines := strings.Split(body, "\n")
	currentHeader := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			currentHeader = strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "<!--") {
			continue
		}

		if currentHeader != "" {
			key := normalizeHeader(currentHeader)
			switch key {
			case "product", "product_sku", "sku":
				sku = extractSKU(trimmed)
			case "quantity":
				if qty := parseQuantity(trimmed); qty > 0 {
					options["quantity"] = qty
				}
			default:
				options[key] = trimmed
			}
			currentHeader = ""
			continue
		}
	}

	if sku == "" {
		skuRegex := regexp.MustCompile(`(?i)(?:sku|product sku)[:\s]*([A-Z0-9_]+)`)
		matches := skuRegex.FindStringSubmatch(body)
		if len(matches) >= 2 {
			sku = strings.TrimSpace(matches[1])
		}
	}

	if sku == "" {
		return nil, fmt.Errorf("no SKU found in issue body. Please include 'SKU: <product-sku>' in your order")
	}

	return &OrderData{
		SKU:     sku,
		Options: options,
	}, nil
}

func extractSKU(value string) string {
	skuRegex := regexp.MustCompile(`(?i)SKU[:\s]*([A-Z0-9_]+)`)
	if matches := skuRegex.FindStringSubmatch(value); len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return strings.TrimSpace(value)
}

func normalizeHeader(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, " ", "_")
	normalized = strings.ReplaceAll(normalized, "-", "_")
	var b strings.Builder
	for _, r := range normalized {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseQuantity(value string) int {
	value = strings.TrimSpace(value)
	if qty, err := strconv.Atoi(value); err == nil && qty > 0 {
		if qty > 10 {
			return 10
		}
		return qty
	}

	numberRegex := regexp.MustCompile(`\d+`)
	if match := numberRegex.FindString(value); match != "" {
		if qty, err := strconv.Atoi(match); err == nil && qty > 0 {
			if qty > 10 {
				return 10
			}
			return qty
		}
	}

	return 0
}

func findProduct(config *catalog.GitShopConfig, sku string) *catalog.ProductConfig {
	if config == nil {
		return nil
	}
	for _, product := range config.Products {
		if product.SKU == sku {
			return &product
		}
	}
	return nil
}

func (s *OrderService) ensureIssueNumberInTitle(ctx context.Context, client *githubapp.Client, repoFullName string, issueNumber int, title string) {
	if strings.TrimSpace(title) == "" {
		return
	}

	suffix := fmt.Sprintf("#%d", issueNumber)
	if strings.Contains(title, suffix) {
		return
	}

	updatedTitle := strings.TrimSpace(fmt.Sprintf("%s %s", title, suffix))
	if err := client.UpdateIssueTitle(ctx, repoFullName, issueNumber, updatedTitle); err != nil {
		s.loggerFromContext(ctx).Error("failed to update issue title with order number", "error", err, "repo", repoFullName, "issue", issueNumber)
	}
}

func (s *OrderService) getGitShopConfigFile(ctx context.Context, client *githubapp.Client, repoFullName string) ([]byte, error) {
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

func (s *OrderService) assignShopManager(ctx context.Context, client *githubapp.Client, repoFullName string, issueNumber int, config *catalog.GitShopConfig) {
	if client == nil || config == nil {
		return
	}
	manager := strings.TrimSpace(config.Shop.Manager)
	if manager == "" || !catalog.IsValidGitHubUsername(manager) {
		return
	}
	if err := client.AssignIssue(ctx, repoFullName, issueNumber, []string{manager}); err != nil {
		s.loggerFromContext(ctx).Warn("failed to assign manager to order issue", "error", err, "repo", repoFullName, "issue", issueNumber, "manager", manager)
	}
}
