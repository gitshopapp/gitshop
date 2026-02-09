// Package email provides Resend email provider.
package email

import (
	"context"
	"fmt"

	resend "github.com/resend/resend-go/v3"
)

// ResendProvider implements the Provider interface for Resend.
type ResendProvider struct {
	apiKey string
	from   string
	client *resend.Client
}

// NewResendProvider creates a new Resend provider.
func NewResendProvider(apiKey, from string) *ResendProvider {
	return &ResendProvider{
		apiKey: apiKey,
		from:   from,
		client: resend.NewClient(apiKey),
	}
}

// SendEmail sends an email via the Resend API.
func (r *ResendProvider) SendEmail(ctx context.Context, email *Email) error {
	if email == nil {
		return fmt.Errorf("email is required")
	}
	if r.client == nil {
		return fmt.Errorf("resend client not configured")
	}

	params := &resend.SendEmailRequest{
		From:    r.from,
		To:      []string{email.To},
		Subject: email.Subject,
	}
	if email.HTML != "" {
		params.Html = email.HTML
	}
	if email.Text != "" {
		params.Text = email.Text
	}
	if params.Html == "" && params.Text == "" {
		return fmt.Errorf("email body is empty")
	}

	if _, err := r.client.Emails.SendWithContext(ctx, params); err != nil {
		return fmt.Errorf("failed to send email via resend: %w", err)
	}
	return nil
}

// ValidateAPIKey checks if the API key is valid.
func (r *ResendProvider) ValidateAPIKey(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("resend client not configured")
	}
	if _, err := r.client.ApiKeys.ListWithContext(ctx); err != nil {
		return fmt.Errorf("invalid API key: %w", err)
	}
	return nil
}
