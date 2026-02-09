package githubapp

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"testing"
)

func TestValidateWebhookSignature(t *testing.T) {
	secret := "test_secret"
	payload := []byte("test payload")

	// Generate correct signature for test
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	correctSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name      string
		signature string
		wantErr   bool
	}{
		{
			name:      "valid signature",
			signature: correctSignature,
			wantErr:   false,
		},
		{
			name:      "invalid signature",
			signature: "sha256=invalid_signature",
			wantErr:   true,
		},
		{
			name:      "missing sha256 prefix",
			signature: "invalid_signature",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWebhookSignature(payload, tt.signature, secret)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestReadWebhookPayload_MissingSignature(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewBufferString("payload"))
	_, err := ReadWebhookPayload(req, "secret")
	if err == nil {
		t.Fatal("expected error for missing signature")
	}
}

func TestReadWebhookPayload_Valid(t *testing.T) {
	t.Parallel()

	secret := "test_secret"
	payload := []byte(`{"action":"opened"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", signature)

	got, err := ReadWebhookPayload(req, secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("unexpected payload: got=%q want=%q", string(got), string(payload))
	}
}
