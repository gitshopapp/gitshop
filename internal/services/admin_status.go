package services

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/gitshopapp/gitshop/internal/catalog"
	"github.com/gitshopapp/gitshop/internal/db"
	"github.com/gitshopapp/gitshop/internal/githubapp"
)

type SetupStatus struct {
	Labels   RepoLabelsStatus
	YAML     GitShopYAMLStatus
	Template OrderTemplateStatus
}

type RepoLabelsStatus struct {
	Ready        bool
	Missing      []string
	ErrorMessage string
}

type GitShopYAMLStatus struct {
	Exists           bool
	Valid            bool
	Method           string
	URL              string
	ErrorMessage     string
	LastUpdatedLabel string
}

type OrderTemplateStatus struct {
	Exists           bool
	Valid            bool
	Method           string
	URL              string
	ErrorMessage     string
	UnknownSKUs      []string
	PriceMismatches  []string
	OptionMismatches []string
	SyncAvailable    bool
	SyncMessage      string
	LastUpdatedLabel string
	Count            int
}

type ProductSummary struct {
	SKU        string
	Name       string
	PriceCents int
	Active     bool
}

type RepoStatus struct {
	YAMLExists               bool
	YAMLValid                bool
	YAMLURL                  string
	YAMLLastUpdatedLabel     string
	TemplateExists           bool
	TemplateValid            bool
	TemplateURL              string
	TemplateLastUpdatedLabel string
	TemplateCount            int
	TemplateFiles            []TemplateFile
	TemplateMissingSKUs      []string
	TemplateExtraSKUs        []string
	TemplatePriceMismatches  []string
	TemplateOptionMismatches []string
	TemplateSyncAvailable    bool
	TemplateSyncMessage      string
	Products                 []ProductSummary
}

type TemplateFile struct {
	Name  string
	URL   string
	Valid bool
}

func IsEmailConfigured(shop *db.Shop) bool {
	return shop != nil && shop.EmailVerified && shop.EmailProvider != "" && len(shop.EmailConfig) > 0
}

func RequiredRepoLabels() []githubapp.LabelDefinition {
	return []githubapp.LabelDefinition{
		{Name: "gitshop:order", Color: "0ea5e9", Description: "GitShop order issue"},
		{Name: "gitshop:status:pending-payment", Color: "f59e0b", Description: "Awaiting payment"},
		{Name: "gitshop:status:paid", Color: "10b981", Description: "Payment received"},
		{Name: "gitshop:status:shipped", Color: "3b82f6", Description: "Order shipped"},
		{Name: "gitshop:status:delivered", Color: "22c55e", Description: "Order delivered"},
		{Name: "gitshop:status:expired", Color: "6b7280", Description: "Order expired"},
	}
}

func (s *AdminService) BuildSetupStatus(ctx context.Context, shop *db.Shop) SetupStatus {
	status := SetupStatus{}
	if shop == nil || shop.GitHubRepoFullName == "" {
		status.Labels.ErrorMessage = "shop is required"
		status.YAML.ErrorMessage = "shop is required"
		status.Template.ErrorMessage = "shop is required"
		return status
	}

	client := s.githubClient.WithInstallation(shop.GitHubInstallationID)
	status.Labels = s.buildLabelsStatus(ctx, client, shop.GitHubRepoFullName)
	yamlStatus, config := s.buildYAMLStatus(ctx, client, shop.GitHubRepoFullName)
	status.YAML = yamlStatus
	status.Template = s.buildTemplateStatus(ctx, client, shop.GitHubRepoFullName, yamlStatus, config)
	return status
}

func (s *AdminService) BuildRepoStatus(ctx context.Context, shop *db.Shop) *RepoStatus {
	if shop == nil || shop.GitHubRepoFullName == "" {
		return nil
	}

	status := &RepoStatus{}
	client := s.githubClient.WithInstallation(shop.GitHubInstallationID)

	yamlFileStatus, yamlPath, err := s.getGitShopFileStatus(ctx, client, shop.GitHubRepoFullName)
	if err == nil && yamlFileStatus != nil {
		status.YAMLExists = yamlFileStatus.Exists
		status.YAMLURL = yamlFileStatus.HTMLURL
		if !yamlFileStatus.LastUpdated.IsZero() {
			status.YAMLLastUpdatedLabel = humanizeSince(yamlFileStatus.LastUpdated)
		}
	}

	var config *catalog.GitShopConfig
	if status.YAMLExists {
		content, contentErr := s.getGitShopFile(ctx, client, shop.GitHubRepoFullName, yamlPath)
		if contentErr == nil {
			parsed, parseErr := s.parser.Parse(content)
			if parseErr == nil {
				if validateErr := s.validator.Validate(parsed); validateErr == nil {
					status.YAMLValid = true
					config = parsed
				}
			}
		}
	}

	if config != nil {
		for _, product := range config.Products {
			if !product.Active {
				continue
			}
			status.Products = append(status.Products, ProductSummary{
				SKU:        product.SKU,
				Name:       product.Name,
				PriceCents: product.UnitPriceCents,
				Active:     product.Active,
			})
		}
	}

	templateFiles, err := client.ListDirectory(ctx, shop.GitHubRepoFullName, ".github/ISSUE_TEMPLATE")
	if err != nil {
		return status
	}

	templateCandidates := filterTemplateFiles(templateFiles)
	status.TemplateCount = len(templateCandidates)

	latestTemplateUpdate := time.Time{}
	templateExtraSKUs := make(map[string]struct{})
	anyValidTemplate := false

	for _, file := range templateCandidates {
		fileStatus, statusErr := client.GetFileStatus(ctx, shop.GitHubRepoFullName, file.Path)
		if statusErr == nil && fileStatus != nil && fileStatus.LastUpdated.After(latestTemplateUpdate) {
			latestTemplateUpdate = fileStatus.LastUpdated
		}

		content, readErr := client.GetFile(ctx, shop.GitHubRepoFullName, file.Path, "")
		if readErr != nil {
			continue
		}

		templateContent := string(content)
		hasMarker := hasOrderTemplateMarker(templateContent)
		if !hasMarker {
			continue
		}

		if status.TemplateURL == "" {
			status.TemplateURL = file.HTMLURL
		}
		status.TemplateExists = true

		hasLabel := templateHasLabel(templateContent, "gitshop:order")
		fileValid := hasMarker && hasLabel

		if status.YAMLValid && config != nil {
			templateSKUs := findTemplateSKUs(templateContent)
			yamlSKUs := make(map[string]struct{}, len(config.Products))
			for _, product := range config.Products {
				yamlSKUs[product.SKU] = struct{}{}
			}
			unknownForFile := false
			for sku := range templateSKUs {
				if _, ok := yamlSKUs[sku]; !ok {
					templateExtraSKUs[sku] = struct{}{}
					unknownForFile = true
				}
			}

			optionMismatches := findTemplateOptionMismatches(templateContent, config)
			status.TemplateOptionMismatches = append(status.TemplateOptionMismatches, optionMismatches...)

			priceMismatches := findTemplatePriceMismatches(templateContent, config)
			status.TemplatePriceMismatches = append(status.TemplatePriceMismatches, priceMismatches...)

			if len(templateSKUs) == 0 || unknownForFile || len(optionMismatches) > 0 || len(priceMismatches) > 0 {
				fileValid = false
			}
		}

		status.TemplateFiles = append(status.TemplateFiles, TemplateFile{
			Name:  file.Name,
			URL:   file.HTMLURL,
			Valid: fileValid,
		})

		if fileValid {
			anyValidTemplate = true
		}
	}

	if !latestTemplateUpdate.IsZero() {
		status.TemplateLastUpdatedLabel = humanizeSince(latestTemplateUpdate)
	}

	for sku := range templateExtraSKUs {
		status.TemplateExtraSKUs = append(status.TemplateExtraSKUs, sku)
	}
	sort.Strings(status.TemplateExtraSKUs)

	if anyValidTemplate && len(status.TemplateExtraSKUs) == 0 && len(status.TemplatePriceMismatches) == 0 && len(status.TemplateOptionMismatches) == 0 {
		status.TemplateValid = true
	}

	status.TemplateSyncAvailable, status.TemplateSyncMessage = computeTemplateSyncAvailability(status.TemplateExists, status.YAMLValid, status.TemplateExtraSKUs)
	return status
}

func (s *AdminService) buildLabelsStatus(ctx context.Context, client *githubapp.Client, repoFullName string) RepoLabelsStatus {
	status := RepoLabelsStatus{}
	labels, err := client.ListLabels(ctx, repoFullName)
	if err != nil {
		status.ErrorMessage = err.Error()
		return status
	}

	for _, label := range RequiredRepoLabels() {
		if _, ok := labels[label.Name]; !ok {
			status.Missing = append(status.Missing, label.Name)
		}
	}

	status.Ready = len(status.Missing) == 0
	return status
}

func (s *AdminService) buildYAMLStatus(ctx context.Context, client *githubapp.Client, repoFullName string) (GitShopYAMLStatus, *catalog.GitShopConfig) {
	status := GitShopYAMLStatus{}
	fileStatus, yamlPath, err := s.getGitShopFileStatus(ctx, client, repoFullName)
	if err != nil {
		status.ErrorMessage = err.Error()
		return status, nil
	}

	if fileStatus != nil {
		status.Exists = fileStatus.Exists
		status.URL = fileStatus.HTMLURL
		if !fileStatus.LastUpdated.IsZero() {
			status.LastUpdatedLabel = humanizeSince(fileStatus.LastUpdated)
		}
	}

	if !status.Exists {
		return status, nil
	}

	content, err := s.getGitShopFile(ctx, client, repoFullName, yamlPath)
	if err != nil {
		status.ErrorMessage = err.Error()
		return status, nil
	}

	config, err := s.parser.Parse(content)
	if err != nil {
		status.ErrorMessage = err.Error()
		return status, nil
	}

	if err := s.validator.Validate(config); err != nil {
		status.ErrorMessage = err.Error()
		return status, nil
	}

	status.Valid = true
	return status, config
}

func (s *AdminService) buildTemplateStatus(ctx context.Context, client *githubapp.Client, repoFullName string, yamlStatus GitShopYAMLStatus, config *catalog.GitShopConfig) OrderTemplateStatus {
	status := OrderTemplateStatus{}
	files, err := client.ListDirectory(ctx, repoFullName, ".github/ISSUE_TEMPLATE")
	if err != nil {
		status.ErrorMessage = err.Error()
		return status
	}

	templateCandidates := filterTemplateFiles(files)
	latestUpdate := time.Time{}
	anyValid := false
	firstURL := ""
	firstInvalidURL := ""

	for _, file := range templateCandidates {
		content, readErr := client.GetFile(ctx, repoFullName, file.Path, "")
		if readErr != nil {
			continue
		}

		templateContent := string(content)
		hasMarker := hasOrderTemplateMarker(templateContent)
		hasLabel := templateHasLabel(templateContent, "gitshop:order")
		if !hasMarker {
			continue
		}

		status.Count++
		if firstURL == "" {
			firstURL = file.HTMLURL
		}

		fileStatus, statusErr := client.GetFileStatus(ctx, repoFullName, file.Path)
		if statusErr == nil && fileStatus != nil && fileStatus.LastUpdated.After(latestUpdate) {
			latestUpdate = fileStatus.LastUpdated
		}

		fileValid := hasMarker && hasLabel
		if !yamlStatus.Valid || config == nil {
			fileValid = false
		} else {
			templateSKUs := findTemplateSKUs(templateContent)
			yamlSKUs := make(map[string]struct{}, len(config.Products))
			for _, product := range config.Products {
				yamlSKUs[product.SKU] = struct{}{}
			}
			unknown := false
			for sku := range templateSKUs {
				if _, ok := yamlSKUs[sku]; !ok {
					status.UnknownSKUs = append(status.UnknownSKUs, sku)
					unknown = true
				}
			}
			if len(templateSKUs) == 0 || unknown {
				fileValid = false
			}

			optionMismatches := findTemplateOptionMismatches(templateContent, config)
			if len(optionMismatches) > 0 {
				status.OptionMismatches = append(status.OptionMismatches, optionMismatches...)
				fileValid = false
			}

			priceMismatches := findTemplatePriceMismatches(templateContent, config)
			if len(priceMismatches) > 0 {
				status.PriceMismatches = append(status.PriceMismatches, priceMismatches...)
				fileValid = false
			}
		}

		if fileValid {
			anyValid = true
		} else if firstInvalidURL == "" {
			firstInvalidURL = file.HTMLURL
		}
	}

	status.Exists = status.Count > 0
	if firstInvalidURL != "" {
		status.URL = firstInvalidURL
	} else {
		status.URL = firstURL
	}
	if !latestUpdate.IsZero() {
		status.LastUpdatedLabel = humanizeSince(latestUpdate)
	}

	sort.Strings(status.UnknownSKUs)
	status.Valid = status.Exists && anyValid && len(status.UnknownSKUs) == 0 && len(status.PriceMismatches) == 0 && len(status.OptionMismatches) == 0
	status.SyncAvailable, status.SyncMessage = computeTemplateSyncAvailability(status.Exists, yamlStatus.Valid, status.UnknownSKUs)

	if !yamlStatus.Valid && status.Exists {
		status.ErrorMessage = "gitshop.yaml must be valid before templates can be verified."
	}

	return status
}

func (s *AdminService) getGitShopFileStatus(ctx context.Context, client *githubapp.Client, repoFullName string) (*githubapp.FileStatus, string, error) {
	for _, path := range []string{"gitshop.yaml", "gitshop.yml"} {
		status, err := client.GetFileStatus(ctx, repoFullName, path)
		if err != nil {
			return nil, "", err
		}
		if status != nil && status.Exists {
			return status, path, nil
		}
	}
	return &githubapp.FileStatus{Exists: false}, "gitshop.yaml", nil
}

func (s *AdminService) getGitShopFile(ctx context.Context, client *githubapp.Client, repoFullName, preferredPath string) ([]byte, error) {
	if preferredPath != "" {
		content, err := client.GetFile(ctx, repoFullName, preferredPath, "")
		if err == nil {
			return content, nil
		}
	}
	for _, path := range []string{"gitshop.yaml", "gitshop.yml"} {
		content, err := client.GetFile(ctx, repoFullName, path, "")
		if err == nil {
			return content, nil
		}
	}
	return nil, fmt.Errorf("gitshop.yaml not found")
}

func findTemplateSKUs(template string) map[string]struct{} {
	skus := make(map[string]struct{})
	skuRegex := regexp.MustCompile(`(?i)SKU:([A-Z0-9_]+)`)
	for _, line := range strings.Split(template, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		if !strings.Contains(trimmed, "SKU:") {
			continue
		}
		if match := skuRegex.FindStringSubmatch(trimmed); len(match) >= 2 {
			skus[match[1]] = struct{}{}
		}
	}
	return skus
}

func findTemplatePriceMismatches(template string, config *catalog.GitShopConfig) []string {
	mismatches := []string{}
	if config == nil {
		return mismatches
	}

	productPrices := make(map[string]int)
	for _, product := range config.Products {
		productPrices[product.SKU] = product.UnitPriceCents
	}

	skuRegex := regexp.MustCompile(`(?i)SKU:([A-Z0-9_]+)`)
	priceRegex := regexp.MustCompile(`\$([0-9]+(?:\.[0-9]{2})?)`)
	for _, line := range strings.Split(template, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "-") || !strings.Contains(trimmed, "SKU:") || !strings.Contains(trimmed, "$") {
			continue
		}
		skuMatch := skuRegex.FindStringSubmatch(trimmed)
		priceMatch := priceRegex.FindStringSubmatch(trimmed)
		if len(skuMatch) < 2 || len(priceMatch) < 2 {
			continue
		}
		sku := skuMatch[1]
		templateCents, err := parsePriceToCents(priceMatch[1])
		if err != nil {
			continue
		}
		if yamlCents, ok := productPrices[sku]; ok && yamlCents != templateCents {
			mismatches = append(mismatches, fmt.Sprintf("%s ($%.2f vs $%.2f)", sku, float64(templateCents)/100, float64(yamlCents)/100))
		}
	}

	return mismatches
}

type templateForm struct {
	Labels []string        `yaml:"labels"`
	Body   []templateField `yaml:"body"`
}

type templateField struct {
	Type       string             `yaml:"type"`
	ID         string             `yaml:"id"`
	Attributes templateAttributes `yaml:"attributes"`
}

type templateAttributes struct {
	Label   string `yaml:"label"`
	Options []any  `yaml:"options"`
}

func findTemplateOptionMismatches(template string, config *catalog.GitShopConfig) []string {
	mismatches := []string{}
	if config == nil {
		return mismatches
	}

	form := templateForm{}
	if err := yaml.Unmarshal([]byte(template), &form); err != nil {
		return mismatches
	}

	templateOptions := make(map[string]templateAttributes)
	duplicateIDs := make(map[string]struct{})
	for _, field := range form.Body {
		if field.ID == "" {
			continue
		}
		if _, exists := templateOptions[field.ID]; exists {
			if _, reported := duplicateIDs[field.ID]; !reported {
				mismatches = append(mismatches, fmt.Sprintf("duplicate option field id: %s", field.ID))
				duplicateIDs[field.ID] = struct{}{}
			}
			continue
		}
		templateOptions[field.ID] = field.Attributes
	}

	templateSKUs := findTemplateSKUs(template)
	selectedProducts := activeTemplateProducts(config, templateSKUs)
	if len(selectedProducts) == 0 {
		return mismatches
	}

	baseProduct := selectedProducts[0]
	baseOptions := normalizedOptionsForTemplate(baseProduct)
	for _, product := range selectedProducts[1:] {
		if !templateOptionDefsEqual(baseOptions, normalizedOptionsForTemplate(product)) {
			mismatches = append(mismatches, "template includes products with different option sets; split these products into separate order templates")
			return mismatches
		}
	}

	quantityValues := quantityValuesForProduct(baseProduct)
	templateQuantity, hasQuantity := templateOptions["quantity"]
	if !hasQuantity {
		mismatches = append(mismatches, "missing option: quantity")
	} else {
		templateValues := filterTemplateOptionValues(optionValuesToStrings(templateQuantity.Options))
		if len(templateValues) > 0 && !stringSlicesEqual(quantityValues, templateValues) {
			mismatches = append(mismatches, fmt.Sprintf("values mismatch for quantity (template: %s, yaml: %s)", strings.Join(templateValues, ", "), strings.Join(quantityValues, ", ")))
		}
	}

	for _, option := range baseProduct.Options {
		if option.Name == "quantity" {
			continue
		}
		templateAttr, ok := templateOptions[option.Name]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("missing option: %s", option.Name))
			continue
		}

		if option.Label != "" && templateAttr.Label != "" && option.Label != templateAttr.Label {
			mismatches = append(mismatches, fmt.Sprintf("label mismatch for %s (%s vs %s)", option.Name, templateAttr.Label, option.Label))
		}

		if option.Type == "dropdown" {
			yamlValues := optionValuesToStrings(option.Values)
			templateValues := filterTemplateOptionValues(optionValuesToStrings(templateAttr.Options))
			if !stringSlicesEqual(yamlValues, templateValues) {
				mismatches = append(mismatches, fmt.Sprintf("values mismatch for %s (template: %s, yaml: %s)", option.Name, strings.Join(templateValues, ", "), strings.Join(yamlValues, ", ")))
			}
		}
	}

	return mismatches
}

func activeTemplateProducts(config *catalog.GitShopConfig, templateSKUs map[string]struct{}) []catalog.ProductConfig {
	if config == nil || len(templateSKUs) == 0 {
		return nil
	}
	selected := []catalog.ProductConfig{}
	for _, product := range config.Products {
		if !product.Active {
			continue
		}
		if _, ok := templateSKUs[product.SKU]; ok {
			selected = append(selected, product)
		}
	}
	return selected
}

func normalizedOptionsForTemplate(product catalog.ProductConfig) []catalog.ProductOption {
	options := []catalog.ProductOption{}
	for _, option := range product.Options {
		if option.Name == "quantity" {
			continue
		}
		options = append(options, option)
	}
	return options
}

func templateOptionDefsEqual(a, b []catalog.ProductOption) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx].Name != b[idx].Name || a[idx].Label != b[idx].Label || a[idx].Type != b[idx].Type || a[idx].Required != b[idx].Required {
			return false
		}
		if !stringSlicesEqual(optionValuesToStrings(a[idx].Values), optionValuesToStrings(b[idx].Values)) {
			return false
		}
	}
	return true
}

func quantityValuesForProduct(product catalog.ProductConfig) []string {
	for _, option := range product.Options {
		if option.Name != "quantity" {
			continue
		}
		values := optionValuesToStrings(option.Values)
		if len(values) > 0 {
			return values
		}
	}
	return []string{"1", "2", "3", "4", "5"}
}

func optionValuesToStrings(values any) []string {
	converted := []string{}
	switch v := values.(type) {
	case []any:
		for _, value := range v {
			converted = append(converted, fmt.Sprintf("%v", value))
		}
	case []string:
		converted = append(converted, v...)
	}
	return converted
}

func filterTemplateOptionValues(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if strings.EqualFold(cleaned, "N/A") || strings.EqualFold(cleaned, "None") || strings.EqualFold(cleaned, "Select one") {
			continue
		}
		filtered = append(filtered, cleaned)
	}
	return filtered
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int)
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		if counts[value] == 0 {
			return false
		}
		counts[value]--
	}
	return true
}

func parsePriceToCents(price string) (int, error) {
	parts := strings.Split(price, ".")
	if len(parts) == 1 {
		dollars, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, err
		}
		return dollars * 100, nil
	}
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid price: %s", price)
	}
	dollars, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	centsStr := parts[1]
	if len(centsStr) == 1 {
		centsStr += "0"
	}
	if len(centsStr) > 2 {
		centsStr = centsStr[:2]
	}
	cents, err := strconv.Atoi(centsStr)
	if err != nil {
		return 0, err
	}
	return dollars*100 + cents, nil
}

func templateHasLabel(template, label string) bool {
	form := templateForm{}
	if err := yaml.Unmarshal([]byte(template), &form); err == nil {
		for _, value := range form.Labels {
			trimmed := strings.TrimSpace(value)
			if trimmed == label {
				return true
			}
		}
		return false
	}

	re := regexp.MustCompile(`labels:\s*\[([^\]]+)\]`)
	match := re.FindStringSubmatch(template)
	if len(match) < 2 {
		return false
	}
	values := strings.Split(match[1], ",")
	for _, value := range values {
		trimmed := strings.TrimSpace(strings.Trim(value, "\"'"))
		if trimmed == label {
			return true
		}
	}
	return false
}

func computeTemplateSyncAvailability(templateExists, yamlValid bool, unknownSKUs []string) (bool, string) {
	if !templateExists {
		return false, ""
	}
	if !yamlValid {
		return false, "Sync is unavailable until your gitshop config is valid."
	}
	if len(unknownSKUs) > 0 {
		return false, "Sync is only available for simple updates where SKUs stay the same. Update the template manually."
	}
	return true, ""
}

func humanizeSince(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	duration := time.Since(t)
	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		mins := int(duration.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		return fmt.Sprintf("%dh ago", hours)
	default:
		days := int(duration.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
}
