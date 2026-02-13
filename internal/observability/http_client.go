package observability

import (
	"net/http"
	"time"

	sentryhttpclient "github.com/getsentry/sentry-go/httpclient"
)

var tracePropagationTargets = []string{
	"api.github.com",
	"api.stripe.com",
}

func WrapRoundTripper(base http.RoundTripper) http.RoundTripper {
	return sentryhttpclient.NewSentryRoundTripper(
		base,
		sentryhttpclient.WithTracePropagationTargets(tracePropagationTargets),
	)
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	client := &http.Client{
		Transport: WrapRoundTripper(http.DefaultTransport),
	}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return client
}
