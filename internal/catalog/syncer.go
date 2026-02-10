package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gitshopapp/gitshop/internal/githubapp"
)

type TemplateSyncer struct {
	githubClient *githubapp.Client
}

func NewTemplateSyncer(githubClient *githubapp.Client) *TemplateSyncer {
	return &TemplateSyncer{
		githubClient: githubClient,
	}
}

func (s *TemplateSyncer) SyncIssueTemplate(ctx context.Context, installationID int64, repoFullName string, config *GitShopConfig) error {
	client := s.githubClient.WithInstallation(installationID)

	templateContent, err := s.BuildTemplateContent(config)
	if err != nil {
		return err
	}
	templatePath := ".github/ISSUE_TEMPLATE/order.yaml"

	// Create or update the issue template
	return client.CreateOrUpdateFile(ctx, repoFullName, templatePath, templateContent, "Update order template from gitshop.yaml")
}

func (s *TemplateSyncer) BuildTemplateContent(config *GitShopConfig) (string, error) {
	products, err := selectTemplateProducts(config, nil)
	if err != nil {
		return "", err
	}
	if _, err := sharedOptionDefinitions(products); err != nil {
		return "", err
	}
	content, err := s.generateIssueTemplate(products)
	if err != nil {
		return "", err
	}
	return withOrderTemplateMarker(content), nil
}

func (s *TemplateSyncer) SyncTemplateContent(existingTemplate string, config *GitShopConfig) (string, error) {
	simple, reason, err := s.IsSimpleSync(existingTemplate, config)
	if err != nil {
		return "", err
	}
	if !simple {
		return "", errors.New(reason)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(existingTemplate), &doc); err != nil {
		return "", fmt.Errorf("invalid template YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0] == nil || doc.Content[0].Kind != yaml.MappingNode {
		return "", fmt.Errorf("invalid template structure")
	}

	root := doc.Content[0]
	bodyNode := findMappingValue(root, "body")
	if bodyNode == nil || bodyNode.Kind != yaml.SequenceNode {
		return "", fmt.Errorf("template is missing a valid body section")
	}

	productSKUs := extractProductSKUsFromTemplateBody(bodyNode)
	products, err := selectTemplateProducts(config, productSKUs)
	if err != nil {
		return "", err
	}
	sharedOptions, err := sharedOptionDefinitions(products)
	if err != nil {
		return "", err
	}

	productField := ensureFieldByID(bodyNode, "product", "dropdown")
	updateProductFieldOptions(productField, products)

	quantityValues := quantityOptionValues(products)
	quantityField := ensureFieldByID(bodyNode, "quantity", "dropdown")
	setFieldLabel(quantityField, "Quantity")
	setFieldOptions(quantityField, quantityValues)
	setFieldRequired(quantityField, true)

	s.syncOptionFields(bodyNode, sharedOptions)
	ensureLiteralStyleForMultilineScalars(&doc)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("failed to encode updated template: %w", err)
	}
	return withOrderTemplateMarker(string(out)), nil
}

func (s *TemplateSyncer) IsSimpleSync(existingTemplate string, config *GitShopConfig) (bool, string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(existingTemplate), &doc); err != nil {
		return false, "", fmt.Errorf("invalid template YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0] == nil || doc.Content[0].Kind != yaml.MappingNode {
		return false, "", fmt.Errorf("invalid template structure")
	}

	root := doc.Content[0]
	bodyNode := findMappingValue(root, "body")
	if bodyNode == nil || bodyNode.Kind != yaml.SequenceNode {
		return false, "", fmt.Errorf("template is missing a valid body section")
	}

	productSKUs := extractProductSKUsFromTemplateBody(bodyNode)
	if len(productSKUs) == 0 {
		return false, "Sync is only available when template SKUs match your current gitshop config. Update the template manually.", nil
	}

	products, err := selectTemplateProducts(config, productSKUs)
	if err != nil {
		return false, "Sync is only available for simple updates where SKUs stay the same. Update the template manually.", nil
	}
	if _, err := sharedOptionDefinitions(products); err != nil {
		return false, "Sync is unavailable because template products do not share the same option schema. Split products across templates.", nil
	}

	return true, "", nil
}

func (s *TemplateSyncer) syncOptionFields(bodyNode *yaml.Node, options []normalizedOption) {
	if bodyNode == nil || bodyNode.Kind != yaml.SequenceNode {
		return
	}

	expectedOptionIDs := make(map[string]struct{}, len(options))
	for _, opt := range options {
		if opt.Name == "" {
			continue
		}
		expectedOptionIDs[opt.Name] = struct{}{}
	}

	quantityIndex := -1
	for idx, item := range bodyNode.Content {
		if item == nil || item.Kind != yaml.MappingNode {
			continue
		}
		if getFieldID(item) == "quantity" {
			quantityIndex = idx
			break
		}
	}

	if quantityIndex == -1 {
		return
	}

	start := quantityIndex + 1
	end := start
	for end < len(bodyNode.Content) {
		item := bodyNode.Content[end]
		if item == nil || item.Kind != yaml.MappingNode {
			break
		}
		fieldID := getFieldID(item)
		if _, expected := expectedOptionIDs[fieldID]; !expected {
			break
		}
		end++
	}

	existingByID := map[string]*yaml.Node{}
	for _, field := range bodyNode.Content {
		if field == nil || field.Kind != yaml.MappingNode {
			continue
		}
		fieldID := getFieldID(field)
		if fieldID == "" {
			continue
		}
		if _, expected := expectedOptionIDs[fieldID]; !expected {
			continue
		}
		if _, exists := existingByID[fieldID]; exists {
			continue
		}
		existingByID[fieldID] = field
	}

	newBlock := make([]*yaml.Node, 0, len(options))
	for _, opt := range options {
		field := existingByID[opt.Name]
		if field == nil {
			fieldType := opt.Type
			if fieldType == "" {
				fieldType = "dropdown"
			}
			field = &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					scalarNode("type"), scalarNode(fieldType),
					scalarNode("id"), scalarNode(opt.Name),
					scalarNode("attributes"), {Kind: yaml.MappingNode},
				},
			}
		}

		fieldType := opt.Type
		if fieldType == "" {
			fieldType = "dropdown"
		}
		setMappingScalar(field, "type", fieldType)
		setFieldLabel(field, opt.Label)
		if fieldType == "dropdown" {
			setFieldOptions(field, opt.Values)
		}
		setFieldRequired(field, opt.Required)
		newBlock = append(newBlock, field)
	}

	updated := make([]*yaml.Node, 0, len(bodyNode.Content)+len(newBlock))
	inserted := false
	for idx, item := range bodyNode.Content {
		if idx >= start && idx < end {
			continue
		}

		if item != nil && item.Kind == yaml.MappingNode {
			fieldID := getFieldID(item)
			if _, expected := expectedOptionIDs[fieldID]; expected {
				// Remove duplicate/misplaced managed option fields and reinsert normalized copies.
				continue
			}
			updated = append(updated, item)
			if !inserted && fieldID == "quantity" {
				updated = append(updated, newBlock...)
				inserted = true
			}
			continue
		}

		updated = append(updated, item)
	}
	if !inserted {
		updated = append(updated, newBlock...)
	}
	bodyNode.Content = updated
}

func (s *TemplateSyncer) generateIssueTemplate(products []ProductConfig) (string, error) {
	template := issueTemplate{
		Name:        "🛒 Place an Order",
		Description: "Order products from our store",
		Title:       "[ORDER] ",
		Labels:      []string{"gitshop:order", "gitshop:status:pending-payment"},
		Body: []templateField{
			{
				Type: "markdown",
				Attributes: templateFieldAttributes{
					Value: "## Welcome to our store!\nFill out the form below to place your order. You'll receive a payment link after submitting.\n",
				},
			},
			{
				Type: "dropdown",
				ID:   "product",
				Attributes: templateFieldAttributes{
					Label:       "Product",
					Description: "Select the product you want to order",
					Options:     productOptions(products),
				},
				Validations: &templateFieldValidations{Required: true},
			},
		},
	}

	template.Body = append(template.Body, templateField{
		Type: "dropdown",
		ID:   "quantity",
		Attributes: templateFieldAttributes{
			Label:   "Quantity",
			Options: quantityOptionValues(products),
		},
		Validations: &templateFieldValidations{Required: true},
	})

	sharedOptions, err := sharedOptionDefinitions(products)
	if err != nil {
		return "", err
	}
	for _, opt := range sharedOptions {
		fieldType := opt.Type
		if fieldType == "" {
			fieldType = "dropdown"
		}
		field := templateField{
			Type: fieldType,
			ID:   opt.Name,
			Attributes: templateFieldAttributes{
				Label: opt.Label,
			},
		}
		if fieldType == "dropdown" {
			field.Attributes.Options = append([]string{}, opt.Values...)
		}
		if opt.Required {
			field.Validations = &templateFieldValidations{Required: true}
		}
		template.Body = append(template.Body, field)
	}

	template.Body = append(template.Body, templateField{
		Type: "markdown",
		Attributes: templateFieldAttributes{
			Value: "---\n**Note:** The SKU in parentheses (e.g., `SKU:PRODUCT_NAME`) must match exactly with your `gitshop.yaml` file.\n",
		},
	})

	content, err := yaml.Marshal(template)
	if err != nil {
		return "", fmt.Errorf("failed to encode issue template: %w", err)
	}
	return string(content), nil
}

func (s *TemplateSyncer) CreateDefaultGitShopYaml(ctx context.Context, installationID int64, repoFullName string) error {
	client := s.githubClient.WithInstallation(installationID)

	defaultConfigDoc := GitShopConfig{
		Shop: ShopConfig{
			Name:     "My Shop",
			Currency: "usd",
			Shipping: ShippingConfig{
				FlatRateCents: 500,
				Carrier:       "USPS",
			},
		},
		Products: []ProductConfig{
			{
				SKU:            "EXAMPLE_TSHIRT",
				Name:           "Example T-Shirt",
				Description:    "Sample product - replace this with your own catalog.",
				UnitPriceCents: 2500,
				Active:         true,
				Options: []ProductOption{
					{
						Name:     "size",
						Label:    "Size",
						Type:     "dropdown",
						Required: true,
						Values:   []string{"S", "M", "L", "XL"},
					},
				},
			},
		},
	}

	configBytes, err := yaml.Marshal(defaultConfigDoc)
	if err != nil {
		return fmt.Errorf("failed to encode default gitshop.yaml: %w", err)
	}

	return client.CreateOrUpdateFile(ctx, repoFullName, "gitshop.yaml", string(configBytes), "Initialize GitShop store configuration")
}

type normalizedOption struct {
	Name     string
	Label    string
	Type     string
	Required bool
	Values   []string
}

var skuPattern = regexp.MustCompile(`(?i)SKU:([A-Z0-9_]+)`)

func selectTemplateProducts(config *GitShopConfig, preferredSKUs []string) ([]ProductConfig, error) {
	if config == nil {
		return nil, fmt.Errorf("gitshop config is required")
	}

	activeBySKU := map[string]ProductConfig{}
	active := make([]ProductConfig, 0, len(config.Products))
	for _, product := range config.Products {
		if !product.Active {
			continue
		}
		active = append(active, product)
		activeBySKU[product.SKU] = product
	}
	if len(active) == 0 {
		return nil, fmt.Errorf("no active products found in gitshop.yaml")
	}

	if len(preferredSKUs) == 0 {
		return active, nil
	}

	selected := make([]ProductConfig, 0, len(preferredSKUs))
	seen := map[string]struct{}{}
	for _, sku := range preferredSKUs {
		if _, already := seen[sku]; already {
			continue
		}
		seen[sku] = struct{}{}
		product, ok := activeBySKU[sku]
		if !ok {
			return nil, fmt.Errorf("template references unknown or inactive SKU: %s", sku)
		}
		selected = append(selected, product)
	}
	return selected, nil
}

func sharedOptionDefinitions(products []ProductConfig) ([]normalizedOption, error) {
	if len(products) == 0 {
		return nil, fmt.Errorf("template must include at least one active product")
	}

	base := normalizeProductOptions(products[0])
	for _, product := range products[1:] {
		next := normalizeProductOptions(product)
		if !normalizedOptionSlicesEqual(base, next) {
			return nil, fmt.Errorf("order template can only include products with the same options; split products into separate templates")
		}
	}
	return base, nil
}

func normalizeProductOptions(product ProductConfig) []normalizedOption {
	normalized := []normalizedOption{}
	for _, option := range product.Options {
		if option.Name == "quantity" {
			continue
		}
		item := normalizedOption{
			Name:     option.Name,
			Label:    option.Label,
			Type:     option.Type,
			Required: option.Required,
			Values:   optionValuesToStrings(option.Values),
		}
		normalized = append(normalized, item)
	}
	return normalized
}

func normalizedOptionSlicesEqual(a, b []normalizedOption) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Label != b[i].Label || a[i].Type != b[i].Type || a[i].Required != b[i].Required {
			return false
		}
		if !slices.Equal(a[i].Values, b[i].Values) {
			return false
		}
	}
	return true
}

func optionValuesToStrings(values []string) []string {
	return append([]string{}, values...)
}

func quantityOptionValues(products []ProductConfig) []string {
	if len(products) == 0 {
		return []string{"1", "2", "3", "4", "5"}
	}
	for _, option := range products[0].Options {
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

type issueTemplate struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Title       string          `yaml:"title"`
	Labels      []string        `yaml:"labels"`
	Body        []templateField `yaml:"body"`
}

type templateField struct {
	Type        string                    `yaml:"type"`
	ID          string                    `yaml:"id,omitempty"`
	Attributes  templateFieldAttributes   `yaml:"attributes"`
	Validations *templateFieldValidations `yaml:"validations,omitempty"`
}

type templateFieldAttributes struct {
	Label       string   `yaml:"label,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Options     []string `yaml:"options,omitempty"`
	Value       string   `yaml:"value,omitempty"`
}

type templateFieldValidations struct {
	Required bool `yaml:"required,omitempty"`
}

func productOptions(products []ProductConfig) []string {
	options := make([]string, 0, len(products))
	for _, product := range products {
		options = append(options, fmt.Sprintf("%s — $%.2f (SKU:%s)", product.Name, float64(product.UnitPriceCents)/100, product.SKU))
	}
	return options
}

func extractProductSKUsFromTemplateBody(bodyNode *yaml.Node) []string {
	productField := findFieldByID(bodyNode, "product")
	if productField == nil {
		return nil
	}
	options := getFieldOptions(productField)
	if len(options) == 0 {
		return nil
	}

	skus := make([]string, 0, len(options))
	for _, item := range options {
		match := skuPattern.FindStringSubmatch(item)
		if len(match) >= 2 {
			skus = append(skus, match[1])
		}
	}
	return skus
}

func updateProductFieldOptions(field *yaml.Node, products []ProductConfig) {
	options := make([]string, 0, len(products))
	for _, product := range products {
		options = append(options, fmt.Sprintf("%s — $%.2f (SKU:%s)", product.Name, float64(product.UnitPriceCents)/100, product.SKU))
	}
	setFieldOptions(field, options)
}

func findFieldByID(bodyNode *yaml.Node, id string) *yaml.Node {
	if bodyNode == nil || bodyNode.Kind != yaml.SequenceNode {
		return nil
	}
	for _, item := range bodyNode.Content {
		if item == nil || item.Kind != yaml.MappingNode {
			continue
		}
		idNode := findMappingValue(item, "id")
		if idNode != nil && idNode.Value == id {
			return item
		}
	}
	return nil
}

func getFieldID(field *yaml.Node) string {
	if field == nil || field.Kind != yaml.MappingNode {
		return ""
	}
	idNode := findMappingValue(field, "id")
	if idNode == nil {
		return ""
	}
	return strings.TrimSpace(idNode.Value)
}

func ensureFieldByID(bodyNode *yaml.Node, id, fieldType string) *yaml.Node {
	field := findFieldByID(bodyNode, id)
	if field != nil {
		setMappingScalar(field, "type", fieldType)
		return field
	}

	field = &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			scalarNode("type"), scalarNode(fieldType),
			scalarNode("id"), scalarNode(id),
			scalarNode("attributes"), {Kind: yaml.MappingNode},
		},
	}
	bodyNode.Content = append(bodyNode.Content, field)
	return field
}

func setFieldLabel(field *yaml.Node, label string) {
	attrs := ensureMappingValue(field, "attributes")
	setMappingScalar(attrs, "label", label)
}

func setFieldOptions(field *yaml.Node, options []string) {
	attrs := ensureMappingValue(field, "attributes")
	setMappingSequence(attrs, "options", options)
}

func setFieldRequired(field *yaml.Node, required bool) {
	validations := ensureMappingValue(field, "validations")
	setMappingBool(validations, "required", required)
}

func getFieldOptions(field *yaml.Node) []string {
	attrs := findMappingValue(field, "attributes")
	if attrs == nil || attrs.Kind != yaml.MappingNode {
		return nil
	}
	options := findMappingValue(attrs, "options")
	if options == nil || options.Kind != yaml.SequenceNode {
		return nil
	}
	values := make([]string, 0, len(options.Content))
	for _, option := range options.Content {
		if option != nil {
			values = append(values, option.Value)
		}
	}
	return values
}

func ensureMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if existing := findMappingValue(mapping, key); existing != nil && existing.Kind == yaml.MappingNode {
		return existing
	}
	value := &yaml.Node{Kind: yaml.MappingNode}
	setMappingNode(mapping, key, value)
	return value
}

func setMappingSequence(mapping *yaml.Node, key string, values []string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, value := range values {
		seq.Content = append(seq.Content, scalarNode(value))
	}
	setMappingNode(mapping, key, seq)
}

func setMappingBool(mapping *yaml.Node, key string, value bool) {
	boolValue := "false"
	if value {
		boolValue = "true"
	}
	setMappingNode(mapping, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: boolValue})
}

func setMappingScalar(mapping *yaml.Node, key, value string) {
	setMappingNode(mapping, key, scalarNode(value))
}

func setMappingNode(mapping *yaml.Node, key string, value *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func findMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: value,
	}
}

func withOrderTemplateMarker(content string) string {
	trimmed := strings.TrimLeft(content, "\n")
	return "# gitshop:order-template\n" + trimmed
}

func ensureLiteralStyleForMultilineScalars(node *yaml.Node) {
	if node == nil {
		return
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && strings.Contains(node.Value, "\n") {
		node.Style = yaml.LiteralStyle
	}
	for _, child := range node.Content {
		ensureLiteralStyleForMultilineScalars(child)
	}
}
