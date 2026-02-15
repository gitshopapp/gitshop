package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/google/uuid"

	"github.com/gitshopapp/gitshop/internal/catalog"
	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/email"
	"github.com/gitshopapp/gitshop/internal/githubapp"
	"github.com/gitshopapp/gitshop/internal/logging"
	"github.com/gitshopapp/gitshop/internal/observability"
	"github.com/gitshopapp/gitshop/internal/stripe"
)

type UserError struct {
	Message string
}

func (e UserError) Error() string {
	return e.Message
}

var (
	ErrAdminInvalidShipmentInput = errors.New("invalid shipment input")
	ErrAdminOrderNotFound        = errors.New("order not found")
	ErrAdminOrderStatusConflict  = errors.New("order status conflict")
	ErrAdminShopNotFound         = errors.New("shop not found")
	ErrAdminServiceUnavailable   = errors.New("admin service unavailable")
)

type ShipOrderInput struct {
	ShopID           uuid.UUID
	OrderID          uuid.UUID
	TrackingNumber   string
	ShippingProvider string
	Carrier          string
	OtherCarrier     string
}

type AdminService struct {
	shopStore      *db.ShopStore
	orderStore     *db.OrderStore
	githubClient   *githubapp.Client
	stripePlatform *stripe.PlatformClient
	orderEmailer   OrderEmailSender
	parser         configParser
	validator      configValidator
	newSyncer      func(client *githubapp.Client) *catalog.TemplateSyncer
	newProvider    func(config email.Config) (email.Provider, error)
	logger         *slog.Logger
}

func NewAdminService(
	shopStore *db.ShopStore,
	orderStore *db.OrderStore,
	githubClient *githubapp.Client,
	stripePlatform *stripe.PlatformClient,
	parser configParser,
	validator configValidator,
	orderEmailer OrderEmailSender,
	newSyncer func(client *githubapp.Client) *catalog.TemplateSyncer,
	newProvider func(config email.Config) (email.Provider, error),
	logger *slog.Logger,
) *AdminService {
	if newProvider == nil {
		newProvider = email.NewProvider
	}
	if orderEmailer == nil {
		orderEmailer = noopOrderEmailSender{}
	}

	return &AdminService{
		shopStore:      shopStore,
		orderStore:     orderStore,
		githubClient:   githubClient,
		stripePlatform: stripePlatform,
		orderEmailer:   orderEmailer,
		parser:         parser,
		validator:      validator,
		newSyncer:      newSyncer,
		newProvider:    newProvider,
		logger:         logger,
	}
}

func (s *AdminService) loggerFromContext(ctx context.Context) *slog.Logger {
	return logging.FromContext(ctx, s.logger)
}

func (s *AdminService) UpdateEmailSettings(ctx context.Context, shopID uuid.UUID, provider, apiKey, from, domain string) error {
	if provider != "postmark" && provider != "mailgun" && provider != "resend" {
		return UserError{Message: "Provider must be postmark, mailgun, or resend"}
	}

	if apiKey == "" || from == "" {
		return UserError{Message: "API key and from email are required"}
	}

	if provider == "mailgun" && domain == "" {
		return UserError{Message: "Domain is required for mailgun"}
	}

	emailConfig := map[string]any{
		"api_key":    apiKey,
		"from_email": from,
	}
	if provider == "mailgun" {
		emailConfig["domain"] = domain
	}

	_, err := s.newProvider(email.Config{
		Provider: provider,
		APIKey:   apiKey,
		From:     from,
		Domain:   domain,
	})
	if err != nil {
		return UserError{Message: fmt.Sprintf("Invalid email configuration: %s", err.Error())}
	}

	if err := s.shopStore.UpdateEmailConfig(ctx, shopID, provider, emailConfig, true); err != nil {
		return fmt.Errorf("failed to update email config: %w", err)
	}

	return nil
}

func (s *AdminService) EnsureRepoLabels(ctx context.Context, shop *db.Shop) error {
	if s == nil || s.githubClient == nil {
		return fmt.Errorf("%w: github client unavailable", ErrAdminServiceUnavailable)
	}
	if shop == nil {
		return fmt.Errorf("shop is required")
	}

	client := s.githubClient.WithInstallation(shop.GitHubInstallationID)
	if err := client.EnsureLabels(ctx, shop.GitHubRepoFullName, RequiredRepoLabels()); err != nil {
		return err
	}

	return nil
}

func (s *AdminService) EnsureGitShopYAML(ctx context.Context, shop *db.Shop) (*githubapp.YAMLCreationResult, error) {
	if s == nil || s.githubClient == nil {
		return nil, fmt.Errorf("%w: github client unavailable", ErrAdminServiceUnavailable)
	}
	if shop == nil {
		return nil, fmt.Errorf("shop is required")
	}

	owner, repo, err := splitRepoFullName(shop.GitHubRepoFullName)
	if err != nil {
		return nil, err
	}

	client := s.githubClient.WithInstallation(shop.GitHubInstallationID)
	result, err := client.EnsureGitShopYAMLForRepo(ctx, owner, repo, shop.GitHubRepoFullName)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *AdminService) GetRecentOrders(ctx context.Context, shopID uuid.UUID, limit int) ([]*db.Order, error) {
	if s == nil || s.orderStore == nil {
		return nil, fmt.Errorf("%w: order store unavailable", ErrAdminServiceUnavailable)
	}
	if shopID == uuid.Nil {
		return nil, fmt.Errorf("%w: empty shop id", ErrAdminShopNotFound)
	}
	if limit <= 0 {
		limit = 20
	}

	orders, err := s.orderStore.GetOrdersByShop(ctx, shopID, limit)
	if err != nil {
		return nil, err
	}

	return orders, nil
}

func (s *AdminService) EnsureOrderTemplate(ctx context.Context, shop *db.Shop) (*githubapp.FileCreationResult, error) {
	if shop == nil {
		return nil, fmt.Errorf("shop is required")
	}

	client := s.githubClient.WithInstallation(shop.GitHubInstallationID)
	config, err := s.fetchValidatedConfig(ctx, client, shop.GitHubRepoFullName)
	if err != nil {
		return nil, err
	}

	syncer := s.newSyncer(s.githubClient)
	templateContent, err := syncer.BuildTemplateContent(config)
	if err != nil {
		return nil, err
	}

	owner, repo, err := splitRepoFullName(shop.GitHubRepoFullName)
	if err != nil {
		return nil, err
	}

	result, err := client.EnsureOrderTemplate(ctx, owner, repo, templateContent)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *AdminService) SyncOrderTemplates(ctx context.Context, shop *db.Shop) (string, error) {
	if shop == nil {
		return "", fmt.Errorf("shop is required")
	}

	client := s.githubClient.WithInstallation(shop.GitHubInstallationID)
	config, err := s.fetchValidatedConfig(ctx, client, shop.GitHubRepoFullName)
	if err != nil {
		return "", err
	}

	syncer := s.newSyncer(s.githubClient)

	owner, repo, err := splitRepoFullName(shop.GitHubRepoFullName)
	if err != nil {
		return "", err
	}

	templates, listErr := client.ListDirectory(ctx, shop.GitHubRepoFullName, ".github/ISSUE_TEMPLATE")
	if listErr != nil {
		return "", listErr
	}

	markerFiles := []githubapp.RepoFile{}
	for _, file := range filterTemplateFiles(templates) {
		content, readErr := client.GetFile(ctx, shop.GitHubRepoFullName, file.Path, "")
		if readErr != nil {
			continue
		}
		if hasOrderTemplateMarker(string(content)) {
			markerFiles = append(markerFiles, file)
		}
	}

	if len(markerFiles) == 0 {
		markerFiles = append(markerFiles, githubapp.RepoFile{
			Name: "order.yaml",
			Path: ".github/ISSUE_TEMPLATE/order.yaml",
		})
	}

	var prURL string
	for _, file := range markerFiles {
		var syncedContent string
		currentContent, err := client.GetFile(ctx, shop.GitHubRepoFullName, file.Path, "")
		if err != nil {
			syncedContent, err = syncer.BuildTemplateContent(config)
			if err != nil {
				return "", err
			}
		} else {
			simple, reason, simpleErr := syncer.IsSimpleSync(string(currentContent), config)
			if simpleErr != nil {
				return "", simpleErr
			}
			if !simple {
				return "", UserError{Message: reason}
			}

			syncedContent, err = syncer.SyncTemplateContent(string(currentContent), config)
			if err != nil {
				return "", err
			}
		}

		branchSuffix := strings.ReplaceAll(strings.TrimSuffix(file.Name, filepath.Ext(file.Name)), "/", "-")
		result, err := client.CreateOrUpdateFileWithPR(ctx, owner, repo, file.Path, syncedContent, "Sync GitShop order template", "Sync GitShop order template", "This PR synchronizes the GitShop order issue template with your current `gitshop.yaml`.", "gitshop/sync-order-template-"+branchSuffix)
		if err != nil {
			return "", err
		}
		if result != nil && result.Method == "pr" && result.URL != "" && prURL == "" {
			prURL = result.URL
		}
	}

	return prURL, nil
}

func (s *AdminService) ShipOrder(ctx context.Context, input ShipOrderInput) error {
	span := sentry.StartSpan(
		ctx,
		"service.admin.ship_order",
		sentry.WithOpName("service.admin"),
		sentry.WithDescription("ShipOrder"),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	)
	defer span.Finish()
	ctx = span.Context()

	logger := s.loggerFromContext(ctx)
	meter := observability.MeterFromContext(ctx)
	meter.Count("fulfillment.shipment.received", 1)
	recordFailed := func(reason string) {
		meter.Count("fulfillment.shipment.failed", 1, sentry.WithAttributes(
			attribute.String("reason", reason),
		))
	}

	if input.ShopID == uuid.Nil || input.OrderID == uuid.Nil {
		recordFailed("invalid_input")
		return fmt.Errorf("%w: shop and order IDs are required", ErrAdminInvalidShipmentInput)
	}

	trackingNumber := strings.TrimSpace(input.TrackingNumber)
	carrier := ResolveShippingCarrier(input.ShippingProvider, input.Carrier, input.OtherCarrier)
	carrier = strings.TrimSpace(carrier)
	if trackingNumber == "" || carrier == "" {
		recordFailed("missing_tracking_details")
		return fmt.Errorf("%w: tracking number and carrier are required", ErrAdminInvalidShipmentInput)
	}

	order, err := s.orderStore.GetByID(ctx, input.OrderID)
	if err != nil {
		recordFailed("order_lookup_failed")
		return fmt.Errorf("%w: %w", ErrAdminOrderNotFound, err)
	}
	if order.ShopID != input.ShopID {
		recordFailed("order_shop_mismatch")
		return fmt.Errorf("%w: order does not belong to shop", ErrAdminOrderNotFound)
	}

	if order.Status != db.StatusPaid && order.Status != db.StatusShipped {
		recordFailed("invalid_order_status")
		return fmt.Errorf("%w: only paid or shipped orders can be updated", ErrAdminOrderStatusConflict)
	}

	action := "update_shipment_details"
	if order.Status == db.StatusPaid {
		action = "mark_shipped"
		if err := s.orderStore.MarkShipped(ctx, input.OrderID, trackingNumber, carrier); err != nil {
			if errors.Is(err, db.ErrInvalidStatusTransition) {
				recordFailed("invalid_status_transition")
				return fmt.Errorf("%w: %w", ErrAdminOrderStatusConflict, err)
			}
			recordFailed("mark_shipped_failed")
			return fmt.Errorf("failed to mark order as shipped: %w", err)
		}
	} else {
		if err := s.orderStore.UpdateShipmentDetails(ctx, input.OrderID, trackingNumber, carrier); err != nil {
			if errors.Is(err, db.ErrInvalidStatusTransition) {
				recordFailed("invalid_status_transition")
				return fmt.Errorf("%w: %w", ErrAdminOrderStatusConflict, err)
			}
			recordFailed("update_shipment_failed")
			return fmt.Errorf("failed to update shipment details: %w", err)
		}
	}

	shop, err := s.shopStore.GetByID(ctx, input.ShopID)
	if err != nil {
		recordFailed("shop_lookup_failed")
		return fmt.Errorf("%w: %w", ErrAdminShopNotFound, err)
	}

	trackingURL := BuildTrackingURL(carrier, trackingNumber)
	if err := s.orderEmailer.SendOrderShipped(ctx, shop, order, OrderShipmentEmailInput{
		TrackingNumber:  trackingNumber,
		TrackingURL:     trackingURL,
		TrackingCarrier: carrier,
	}); err != nil {
		meter.Count("fulfillment.shipment.side_effect_failed", 1, sentry.WithAttributes(
			attribute.String("reason", "shipping_email_failed"),
		))
		logger.Error("failed to send shipping email", "error", err, "order_id", input.OrderID)
	}

	client := s.githubClient.WithInstallation(shop.GitHubInstallationID)
	commentBody := "🚚 Your order has shipped! Tracking details were sent by email."
	if order.Status == db.StatusShipped {
		commentBody = "🔄 Shipment details were updated. Check the latest tracking details in your email."
	}

	if err := client.CreateComment(ctx, shop.GitHubRepoFullName, order.GitHubIssueNumber, commentBody); err != nil {
		meter.Count("fulfillment.shipment.side_effect_failed", 1, sentry.WithAttributes(
			attribute.String("reason", "github_comment_failed"),
		))
		logger.Error("failed to create GitHub comment", "error", err, "issue", order.GitHubIssueNumber, "shop_id", shop.ID)
	}
	if err := client.RemoveLabel(ctx, shop.GitHubRepoFullName, order.GitHubIssueNumber, "gitshop:status:paid"); err != nil {
		meter.Count("fulfillment.shipment.side_effect_failed", 1, sentry.WithAttributes(
			attribute.String("reason", "github_remove_label_failed"),
		))
		logger.Warn("failed to remove paid label", "error", err, "issue", order.GitHubIssueNumber, "shop_id", shop.ID)
	}
	if err := client.AddLabels(ctx, shop.GitHubRepoFullName, order.GitHubIssueNumber, []string{"gitshop:status:shipped"}); err != nil {
		meter.Count("fulfillment.shipment.side_effect_failed", 1, sentry.WithAttributes(
			attribute.String("reason", "github_add_label_failed"),
		))
		logger.Warn("failed to add shipped label", "error", err, "issue", order.GitHubIssueNumber, "shop_id", shop.ID)
	}
	meter.Count("fulfillment.shipment.processed", 1, sentry.WithAttributes(
		attribute.String("action", action),
	))

	return nil
}

func (s *AdminService) fetchValidatedConfig(ctx context.Context, client *githubapp.Client, repoFullName string) (*catalog.GitShopConfig, error) {
	content, err := client.GetFile(ctx, repoFullName, "gitshop.yaml", "")
	if err != nil {
		content, err = client.GetFile(ctx, repoFullName, "gitshop.yml", "")
	}
	if err != nil {
		return nil, fmt.Errorf("gitshop.yaml not found")
	}

	config, err := s.parser.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("invalid gitshop.yaml: %w", err)
	}

	if err := s.validator.Validate(config); err != nil {
		return nil, fmt.Errorf("invalid gitshop.yaml: %w", err)
	}

	return config, nil
}

func splitRepoFullName(repoFullName string) (string, string, error) {
	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repository")
	}
	return parts[0], parts[1], nil
}

func filterTemplateFiles(files []githubapp.RepoFile) []githubapp.RepoFile {
	candidates := make([]githubapp.RepoFile, 0, len(files))
	for _, file := range files {
		name := strings.ToLower(file.Name)
		if strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") {
			candidates = append(candidates, file)
		}
	}
	return candidates
}

func hasOrderTemplateMarker(template string) bool {
	for _, line := range strings.Split(template, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return trimmed == "# gitshop:order-template"
	}
	return false
}
