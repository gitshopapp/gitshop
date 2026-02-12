package models

import (
	"time"

	"github.com/google/uuid"
)

type Shop struct {
	ID                     uuid.UUID      `json:"id"`
	GitHubInstallationID   int64          `json:"github_installation_id"`
	GitHubRepoID           int64          `json:"github_repo_id"`
	GitHubRepoFullName     string         `json:"github_repo_full_name"`
	OwnerEmail             string         `json:"owner_email"`
	EmailProvider          string         `json:"email_provider"`
	EmailFrom              string         `json:"email_from"`
	EmailConfig            map[string]any `json:"email_config"`
	EmailVerified          bool           `json:"email_verified"`
	StripeConnectAccountID string         `json:"stripe_connect_account_id"`
	DisconnectedAt         time.Time      `json:"disconnected_at"`
	OnboardedAt            time.Time      `json:"onboarded_at"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

func (s *Shop) IsConnected() bool {
	return s != nil && s.DisconnectedAt.IsZero()
}

func (s *Shop) IsOnboarded() bool {
	return s != nil && !s.OnboardedAt.IsZero()
}
