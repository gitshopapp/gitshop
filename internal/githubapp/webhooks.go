package githubapp

// Package githubapp provides webhook validation functions.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func ValidateWebhookSignature(payload []byte, signature, secret string) error {
	if !strings.HasPrefix(signature, "sha256=") {
		return fmt.Errorf("invalid signature format")
	}

	expectedSignature := signature[7:] // Remove "sha256=" prefix

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actualSignature := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSignature), []byte(actualSignature)) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

func ReadWebhookPayload(r *http.Request, secret string) ([]byte, error) {
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		return nil, fmt.Errorf("missing signature header")
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	if err := ValidateWebhookSignature(payload, signature, secret); err != nil {
		return nil, fmt.Errorf("webhook signature validation failed: %w", err)
	}

	return payload, nil
}
