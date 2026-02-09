// Package email provides Mailgun email provider.
package email

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MailgunProvider implements the Provider interface for Mailgun
type MailgunProvider struct {
	apiKey  string
	from    string
	domain  string
	baseURL string
}

// MailgunResponse represents the Mailgun API response
type MailgunResponse struct {
	Message string `json:"message"`
	ID      string `json:"id"`
}

// NewMailgunProvider creates a new Mailgun provider with default base URL
func NewMailgunProvider(apiKey, domain, from string) *MailgunProvider {
	return &MailgunProvider{
		apiKey:  apiKey,
		domain:  domain,
		from:    from,
		baseURL: "https://api.mailgun.net/v3",
	}
}

// NewMailgunProviderWithBaseURL creates a new Mailgun provider with custom base URL
func NewMailgunProviderWithBaseURL(apiKey, domain, from, baseURL string) *MailgunProvider {
	return &MailgunProvider{
		apiKey:  apiKey,
		domain:  domain,
		from:    from,
		baseURL: baseURL,
	}
}

// SendEmail sends an email via the Mailgun API
func (m *MailgunProvider) SendEmail(ctx context.Context, email *Email) error {
	data := url.Values{}
	data.Set("from", m.from)
	data.Set("to", email.To)
	data.Set("subject", email.Subject)

	if email.Text != "" {
		data.Set("text", email.Text)
	}
	if email.HTML != "" {
		data.Set("html", email.HTML)
	}

	apiURL := fmt.Sprintf("%s/%s/messages", m.baseURL, m.domain)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("api", m.apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("failed to read mailgun response: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close mailgun response body: %w", closeErr)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp MailgunResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
			return fmt.Errorf("mailgun error: %s", errResp.Message)
		}
		return fmt.Errorf("mailgun API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ValidateAPIKey checks if the API key is valid by making a test request
func (m *MailgunProvider) ValidateAPIKey(ctx context.Context) error {
	apiURL := fmt.Sprintf("%s/%s/domains", m.baseURL, m.domain)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth("api", m.apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to validate API key: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("failed to read mailgun validation response: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close mailgun validation response body: %w", closeErr)
	}

	if resp.StatusCode != http.StatusOK {
		if len(body) > 0 {
			return fmt.Errorf("invalid API key: received status %d: %s", resp.StatusCode, string(body))
		}
		return fmt.Errorf("invalid API key: received status %d", resp.StatusCode)
	}

	return nil
}
