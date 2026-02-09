package stripe

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v84/webhook"
)

func TestReadWebhookEvent_MissingSignature(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/webhooks/stripe", bytes.NewBufferString(`{}`))
	_, err := ReadWebhookEvent(req, "whsec_test")
	if err == nil {
		t.Fatal("expected error for missing signature")
	}
}

func TestReadWebhookEvent_Valid(t *testing.T) {
	t.Parallel()

	secret := "whsec_test_secret"
	payload := []byte(`{"id":"evt_test","object":"event","api_version":"2026-01-28.clover","type":"checkout.session.completed","data":{"object":{"id":"cs_test","object":"checkout.session"}}}`)

	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now(),
		Scheme:    "v1",
	})

	req := httptest.NewRequest("POST", "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", signed.Header)

	event, err := ReadWebhookEvent(req, secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil || event.ID != "evt_test" {
		t.Fatalf("unexpected event: %+v", event)
	}
}
