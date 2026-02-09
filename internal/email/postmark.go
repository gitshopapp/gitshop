// Package email provides Postmark email provider.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PostmarkProvider implements the Provider interface for Postmark
type PostmarkProvider struct {
	apiKey string
	from   string
}

// PostmarkResponse represents the Postmark API response
type PostmarkResponse struct {
	ErrorCode   int    `json:"ErrorCode"`
	Message     string `json:"Message"`
	MessageID   string `json:"MessageID"`
	SubmittedAt string `json:"SubmittedAt"`
}

// NewPostmarkProvider creates a new Postmark provider
func NewPostmarkProvider(apiKey, from string) *PostmarkProvider {
	return &PostmarkProvider{
		apiKey: apiKey,
		from:   from,
	}
}

type postmarkEmail struct {
	From       string `json:"From"`
	To         string `json:"To"`
	Subject    string `json:"Subject"`
	TextBody   string `json:"TextBody,omitempty"`
	HtmlBody   string `json:"HtmlBody,omitempty"`
	TrackOpens bool   `json:"TrackOpens"`
	TrackLinks string `json:"TrackLinks"`
	InlineCSS  bool   `json:"InlineCSS"`
	Tag        string `json:"Tag,omitempty"`
	Metadata   string `json:"Metadata,omitempty"`
	ReplyTo    string `json:"ReplyTo,omitempty"`
	Headers    string `json:"Headers,omitempty"`
}

// SendEmail sends an email via the Postmark API
func (p *PostmarkProvider) SendEmail(ctx context.Context, email *Email) error {
	payload := postmarkEmail{
		From:       p.from,
		To:         email.To,
		Subject:    email.Subject,
		TextBody:   email.Text,
		HtmlBody:   email.HTML,
		TrackOpens: true,
		InlineCSS:  true,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal email: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.postmarkapp.com/email", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Postmark-Server-Token", p.apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("failed to read postmark response: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close postmark response body: %w", closeErr)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp PostmarkResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.ErrorCode != 0 {
			return fmt.Errorf("postmark error (%d): %s", errResp.ErrorCode, errResp.Message)
		}
		return fmt.Errorf("postmark API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result PostmarkResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.ErrorCode != 0 {
		return fmt.Errorf("postmark error (%d): %s", result.ErrorCode, result.Message)
	}

	return nil
}

// ValidateAPIKey checks if the API key is valid
func (p *PostmarkProvider) ValidateAPIKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.postmarkapp.com/server", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Postmark-Server-Token", p.apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to validate API key: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("failed to read postmark validation response: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close postmark validation response body: %w", closeErr)
	}

	if resp.StatusCode != http.StatusOK {
		if len(body) > 0 {
			return fmt.Errorf("invalid API key: received status %d: %s", resp.StatusCode, string(body))
		}
		return fmt.Errorf("invalid API key: received status %d", resp.StatusCode)
	}

	return nil
}
