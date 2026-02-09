// Package email provides email templates.
package email

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
	"time"
)

// OrderInfo contains all the information needed for order email templates
type OrderInfo struct {
	OrderNumber         string
	IssueURL            string
	CustomerName        string
	CustomerEmail       string
	ShopName            string
	ShopURL             string
	ProductName         string
	Quantity            int
	UnitPrice           string
	TotalPrice          string
	ShippingAddress     string
	ShippingAddressHTML string
	TrackingNumber      string
	TrackingURL         string
	TrackingCarrier     string
	OrderDate           string
	Items               []OrderItem
	Subtotal            string
	Shipping            string
	Tax                 string
	Total               string
}

// OrderItem represents a single item in an order
type OrderItem struct {
	Name       string
	SKU        string
	Quantity   int
	UnitPrice  string
	TotalPrice string
	Options    string
}

// EmailTemplate defines a named email template
type EmailTemplate struct {
	Name    string
	Subject string
	HTML    string
	Text    string
}

// Renderer provides methods to render email templates
type Renderer struct {
	templates *template.Template
}

// NewRenderer creates a new email template renderer with built-in templates
func NewRenderer() (*Renderer, error) {
	templates := map[string]EmailTemplate{
		"order_confirmation": {
			Name:    "Order Confirmation",
			Subject: "Order Confirmed - {{.OrderNumber}} - {{.ShopName}}",
			HTML:    orderConfirmationHTML,
			Text:    orderConfirmationText,
		},
		"order_shipped": {
			Name:    "Order Shipped",
			Subject: "Your Order Has Shipped - {{.OrderNumber}} - {{.ShopName}}",
			HTML:    orderShippedHTML,
			Text:    orderShippedText,
		},
		"order_delivered": {
			Name:    "Order Delivered",
			Subject: "Your Order Has Been Delivered - {{.OrderNumber}}",
			HTML:    orderDeliveredHTML,
			Text:    orderDeliveredText,
		},
	}

	funcMap := template.FuncMap{
		"formatDate": func(t time.Time) string {
			return t.Format("January 2, 2006")
		},
	}

	tmpl := template.New("email").Funcs(funcMap)

	for key, t := range templates {
		_, err := tmpl.New(key + "_html").Parse(t.HTML)
		if err != nil {
			return nil, fmt.Errorf("failed to parse HTML template %s: %w", key, err)
		}
		_, err = tmpl.New(key + "_text").Parse(t.Text)
		if err != nil {
			return nil, fmt.Errorf("failed to parse text template %s: %w", key, err)
		}
	}

	return &Renderer{
		templates: tmpl,
	}, nil
}

// Render renders an email template with the given data
func (r *Renderer) Render(ctx context.Context, templateName string, data *OrderInfo) (*Email, error) {
	var htmlBuf, textBuf bytes.Buffer

	// Render HTML version
	err := r.templates.ExecuteTemplate(&htmlBuf, templateName+"_html", data)
	if err != nil {
		return nil, fmt.Errorf("failed to render HTML template: %w", err)
	}

	// Render text version
	err = r.templates.ExecuteTemplate(&textBuf, templateName+"_text", data)
	if err != nil {
		return nil, fmt.Errorf("failed to render text template: %w", err)
	}

	// Get subject from template definition
	subject := ""
	switch templateName {
	case "order_confirmation":
		subject = fmt.Sprintf("Order Confirmed - %s - %s", data.OrderNumber, data.ShopName)
	case "order_shipped":
		subject = fmt.Sprintf("Your Order Has Shipped - %s - %s", data.OrderNumber, data.ShopName)
	case "order_delivered":
		subject = fmt.Sprintf("Your Order Has Been Delivered - %s", data.OrderNumber)
	}

	return &Email{
		To:      data.CustomerEmail,
		Subject: subject,
		Text:    textBuf.String(),
		HTML:    htmlBuf.String(),
	}, nil
}

// SendOrderConfirmation sends an order confirmation email
func SendOrderConfirmation(ctx context.Context, p Provider, orderInfo *OrderInfo) error {
	if p == nil {
		return nil
	}

	renderer, err := NewRenderer()
	if err != nil {
		return fmt.Errorf("failed to create renderer: %w", err)
	}

	email, err := renderer.Render(ctx, "order_confirmation", orderInfo)
	if err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	return p.SendEmail(ctx, email)
}

// SendOrderShipped sends an order shipped email
func SendOrderShipped(ctx context.Context, p Provider, orderInfo *OrderInfo) error {
	if p == nil {
		return nil
	}

	renderer, err := NewRenderer()
	if err != nil {
		return fmt.Errorf("failed to create renderer: %w", err)
	}

	email, err := renderer.Render(ctx, "order_shipped", orderInfo)
	if err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	return p.SendEmail(ctx, email)
}

// SendOrderDelivered sends an order delivered email
func SendOrderDelivered(ctx context.Context, p Provider, orderInfo *OrderInfo) error {
	if p == nil {
		return nil
	}

	renderer, err := NewRenderer()
	if err != nil {
		return fmt.Errorf("failed to create renderer: %w", err)
	}

	email, err := renderer.Render(ctx, "order_delivered", orderInfo)
	if err != nil {
		return fmt.Errorf("failed to render template: %w", err)
	}

	return p.SendEmail(ctx, email)
}

// Template text content - Order Confirmation
const orderConfirmationText = `Thank you for your order!

Order Number: {{.OrderNumber}}
Order Date: {{.OrderDate}}

Items:
{{range .Items}}
- {{.Name}}{{if .Options}} ({{.Options}}){{end}} x{{.Quantity}} - {{.TotalPrice}}
{{end}}

Subtotal: {{.Subtotal}}
Shipping: {{.Shipping}}
Tax: {{.Tax}}
Total: {{.Total}}

{{if .IssueURL}}Order Issue: {{.IssueURL}}{{end}}

We'll send you another email when your order ships.

Thank you for shopping with {{.ShopName}}!
{{.ShopURL}}
`

// Template HTML content - Order Confirmation
const orderConfirmationHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Order Confirmation</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px; }
    .header { background: #2563eb; color: white; padding: 20px; text-align: center; border-radius: 8px 8px 0 0; }
    .content { background: #f9fafb; padding: 20px; border: 1px solid #e5e7eb; }
    .order-info { background: white; padding: 15px; border-radius: 6px; margin: 15px 0; }
    .items-table { width: 100%; border-collapse: collapse; margin: 15px 0; }
    .items-table th { text-align: left; padding: 10px; background: #f3f4f6; border-bottom: 2px solid #e5e7eb; }
    .items-table td { padding: 10px; border-bottom: 1px solid #e5e7eb; }
    .total { font-size: 18px; font-weight: bold; text-align: right; padding: 15px 0; }
    .footer { text-align: center; padding: 20px; color: #6b7280; font-size: 14px; }
    .button { display: inline-block; background: #2563eb; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; margin-top: 15px; }
  </style>
</head>
<body>
  <div class="header">
    <h1>Order Confirmed!</h1>
    <p>Thank you for your order, {{.CustomerName}}</p>
  </div>
  <div class="content">
    <div class="order-info">
      <strong>Order Number:</strong> {{.OrderNumber}}<br>
      <strong>Order Date:</strong> {{.OrderDate}}
    </div>

    <h3>Order Summary</h3>
    <table class="items-table">
      <thead>
        <tr>
          <th>Item</th>
          <th>Qty</th>
          <th>Price</th>
        </tr>
      </thead>
      <tbody>
        {{range .Items}}
        <tr>
          <td>{{.Name}}{{if .Options}} <br><small>{{.Options}}</small>{{end}}</td>
          <td>{{.Quantity}}</td>
          <td>{{.TotalPrice}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>

    <div class="total">
      <p>Subtotal: {{.Subtotal}}</p>
      <p>Shipping: {{.Shipping}}</p>
      <p>Tax: {{.Tax}}</p>
      <p>Total: {{.Total}}</p>
    </div>

    <p>We'll send you another email when your order ships.</p>
    {{if .IssueURL}}<p><a href="{{.IssueURL}}" class="button">View your GitHub order issue</a></p>{{end}}
  </div>
  <div class="footer">
    <p>Thank you for shopping with <a href="{{.ShopURL}}">{{.ShopName}}</a></p>
  </div>
</body>
</html>
`

// Template text content - Order Shipped
const orderShippedText = `Great news! Your order has shipped!

Order Number: {{.OrderNumber}}
Shipped Date: {{.OrderDate}}

{{if .TrackingNumber}}
Tracking Number: {{.TrackingNumber}}
Carrier: {{.TrackingCarrier}}
{{if .TrackingURL}}Track your package: {{.TrackingURL}}{{end}}
{{end}}

Shipping Address:
{{.ShippingAddress}}

{{if .IssueURL}}Order Issue: {{.IssueURL}}{{end}}

We'll let you know when your package is delivered!

Thank you for shopping with {{.ShopName}}!
{{.ShopURL}}
`

// Template HTML content - Order Shipped
const orderShippedHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Order Shipped</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px; }
    .header { background: #059669; color: white; padding: 20px; text-align: center; border-radius: 8px 8px 0 0; }
    .content { background: #f9fafb; padding: 20px; border: 1px solid #e5e7eb; }
    .tracking { background: white; padding: 20px; border-radius: 6px; margin: 15px 0; border-left: 4px solid #059669; }
    .tracking-number { font-size: 24px; font-weight: bold; color: #059669; }
    .button { display: inline-block; background: #059669; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; margin-top: 15px; }
    .footer { text-align: center; padding: 20px; color: #6b7280; font-size: 14px; }
  </style>
</head>
<body>
  <div class="header">
    <h1>Your Order Has Shipped! 📦</h1>
    <p>Great news, {{.CustomerName}}! Your order is on its way.</p>
  </div>
  <div class="content">
    <p><strong>Order Number:</strong> {{.OrderNumber}}</p>
    <p><strong>Shipped Date:</strong> {{.OrderDate}}</p>

    {{if .TrackingNumber}}
    <div class="tracking">
      <p><strong>Carrier:</strong> {{.TrackingCarrier}}</p>
      <p class="tracking-number">{{.TrackingNumber}}</p>
      {{if .TrackingURL}}
      <a href="{{.TrackingURL}}" class="button">Track Your Package</a>
      {{end}}
    </div>
    {{end}}

    <h3>Shipping Address</h3>
    <p>{{if .ShippingAddressHTML}}{{.ShippingAddressHTML}}{{else}}{{.ShippingAddress}}{{end}}</p>

    {{if .IssueURL}}<p><a href="{{.IssueURL}}" class="button">View your GitHub order issue</a></p>{{end}}
    <p>We'll let you know when your package is delivered!</p>
  </div>
  <div class="footer">
    <p>Thank you for shopping with <a href="{{.ShopURL}}">{{.ShopName}}</a></p>
  </div>
</body>
</html>
`

// Template text content - Order Delivered
const orderDeliveredText = `Your order has been delivered!

Order Number: {{.OrderNumber}}
Delivered Date: {{.OrderDate}}

Your package should have arrived at:
{{.ShippingAddress}}

We hope you enjoy your purchase! If you have any questions or concerns, please don't hesitate to reach out.

Thank you for shopping with {{.ShopName}}!
{{.ShopURL}}
`

// Template HTML content - Order Delivered
const orderDeliveredHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Order Delivered</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px; }
    .header { background: #7c3aed; color: white; padding: 20px; text-align: center; border-radius: 8px 8px 0 0; }
    .content { background: #f9fafb; padding: 20px; border: 1px solid #e5e7eb; }
    .delivered-badge { background: #7c3aed; color: white; padding: 20px; text-align: center; border-radius: 8px; margin: 15px 0; font-size: 48px; }
    .footer { text-align: center; padding: 20px; color: #6b7280; font-size: 14px; }
  </style>
</head>
<body>
  <div class="header">
    <h1>Your Order Has Been Delivered! 🎉</h1>
    <p>Your package has arrived, {{.CustomerName}}!</p>
  </div>
  <div class="content">
    <div class="delivered-badge">✓</div>
    <p><strong>Order Number:</strong> {{.OrderNumber}}</p>
    <p><strong>Delivered Date:</strong> {{.OrderDate}}</p>

    <h3>Delivered To</h3>
    <p>{{.ShippingAddress}}</p>

    <p>We hope you enjoy your purchase! If you have any questions or concerns about your order, please don't hesitate to reach out.</p>
  </div>
  <div class="footer">
    <p>Thank you for shopping with <a href="{{.ShopURL}}">{{.ShopName}}</a></p>
  </div>
</body>
</html>
`
