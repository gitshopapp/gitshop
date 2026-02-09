package emailconfig

import "strings"

func NormalizeProvider(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	switch normalized {
	case "postmark", "mailgun", "resend":
		return normalized
	default:
		return "postmark"
	}
}
