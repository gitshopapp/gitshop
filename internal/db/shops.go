package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gitshopapp/gitshop/internal/crypto"
	"github.com/gitshopapp/gitshop/internal/db/queries"
)

type ShopStore struct {
	pool    *pgxpool.Pool
	queries *queries.Queries
	crypto  crypto.Encryptor
}

func NewShopStore(pool *pgxpool.Pool, encryptor crypto.Encryptor) (*ShopStore, error) {
	if encryptor == nil {
		return nil, fmt.Errorf("encryptor is required")
	}

	return &ShopStore{
		pool:    pool,
		queries: queries.New(pool),
		crypto:  encryptor,
	}, nil
}

func (s *ShopStore) GetByID(ctx context.Context, id uuid.UUID) (*Shop, error) {
	shop, err := s.queries.GetShopByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.convertShop(shop), nil
}

func (s *ShopStore) GetByInstallationID(ctx context.Context, installationID int64) (*Shop, error) {
	shop, err := s.queries.GetShopByInstallationID(ctx, installationID)
	if err != nil {
		return nil, err
	}

	return s.convertShop(shop), nil
}

func (s *ShopStore) GetByRepoID(ctx context.Context, repoID int64) (*Shop, error) {
	shop, err := s.queries.GetShopByRepoID(ctx, repoID)
	if err != nil {
		return nil, err
	}
	return s.convertShop(shop), nil
}

func (s *ShopStore) GetByInstallationAndRepoID(ctx context.Context, installationID int64, repoID int64) (*Shop, error) {
	shop, err := s.queries.GetShopByInstallationAndRepoID(ctx, queries.GetShopByInstallationAndRepoIDParams{
		GithubInstallationID: installationID,
		GithubRepoID:         repoID,
	})
	if err != nil {
		return nil, err
	}
	return s.convertShop(shop), nil
}

func (s *ShopStore) GetShopsByInstallationID(ctx context.Context, installationID int64) ([]*Shop, error) {
	rows, err := s.queries.GetShopsByInstallationID(ctx, installationID)
	if err != nil {
		return nil, err
	}

	shops := make([]*Shop, 0, len(rows))
	for _, row := range rows {
		shops = append(shops, s.convertShop(row))
	}

	return shops, nil
}

func (s *ShopStore) convertShop(row queries.Shop) *Shop {
	shop := &Shop{
		ID:                   row.ID,
		GitHubInstallationID: row.GithubInstallationID,
		GitHubRepoID:         row.GithubRepoID,
		GitHubRepoFullName:   row.GithubRepoFullName,
		OwnerEmail:           row.OwnerEmail,
		EmailProvider:        row.EmailProvider.String,
		EmailVerified:        row.EmailVerified.Bool,
		CreatedAt:            row.CreatedAt.Time.Format("2006-01-02"),
		UpdatedAt:            row.UpdatedAt.Time.Format("2006-01-02"),
	}

	if row.StripeConnectAccountID.Valid {
		shop.StripeConnectAccountID = row.StripeConnectAccountID.String
	}
	if row.DisconnectedAt.Valid && row.DisconnectedAt.Time.String() != "" {
		shop.DisconnectedAt = row.DisconnectedAt.Time.Format("2006-01-02")
	}
	if row.EmailConfig != nil {
		config := s.decryptEmailConfig(row.EmailConfig)
		shop.EmailConfig = config
		if decoded, err := decodeEmailConfigMap(config); err == nil {
			shop.EmailFrom = decoded.FromEmail
			if shop.EmailFrom == "" {
				shop.EmailFrom = decoded.From
			}
		}
	}

	return shop
}

func (s *ShopStore) decryptEmailConfig(data []byte) map[string]any {
	decoded, err := decodeEmailConfig(data)
	if err != nil {
		return map[string]any{}
	}

	if decoded.APIKey != "" {
		if decrypted, err := s.crypto.Decrypt(decoded.APIKey); err == nil {
			decoded.APIKey = decrypted
		}
	}
	if decoded.FromEmail == "" {
		decoded.FromEmail = decoded.From
	}

	return decoded.toMap()
}

func (s *ShopStore) encryptEmailConfig(config map[string]any) (map[string]any, error) {
	decoded, err := decodeEmailConfigMap(config)
	if err != nil {
		return nil, err
	}

	if decoded.APIKey != "" {
		ciphertext, err := s.crypto.Encrypt(decoded.APIKey)
		if err != nil {
			return nil, err
		}
		decoded.APIKey = ciphertext
	}

	return decoded.toMap(), nil
}

func (s *ShopStore) Create(ctx context.Context, installationID int64, repoID int64, repoFullName, ownerEmail string) (*Shop, error) {
	shop, err := s.queries.CreateShop(ctx, queries.CreateShopParams{
		GithubInstallationID: installationID,
		GithubRepoID:         repoID,
		GithubRepoFullName:   repoFullName,
		OwnerEmail:           ownerEmail,
	})
	if err != nil {
		return nil, err
	}

	return s.convertShop(shop), nil
}

func (s *ShopStore) UpdateRepoFullName(ctx context.Context, shopID uuid.UUID, repoFullName string) error {
	return s.queries.UpdateShopRepoFullName(ctx, queries.UpdateShopRepoFullNameParams{
		ID:                 shopID,
		GithubRepoFullName: repoFullName,
	})
}

func (s *ShopStore) UpdateEmailConfig(ctx context.Context, shopID uuid.UUID, provider string, config map[string]any, verified bool) error {
	encryptedConfig, err := s.encryptEmailConfig(config)
	if err != nil {
		return err
	}

	configJSON, err := json.Marshal(encryptedConfig)
	if err != nil {
		return err
	}

	return s.queries.UpdateShopEmailConfig(ctx, queries.UpdateShopEmailConfigParams{
		ID:            shopID,
		EmailProvider: pgtype.Text{String: provider, Valid: true},
		EmailConfig:   configJSON,
		EmailVerified: pgtype.Bool{Bool: verified, Valid: true},
	})
}

func (s *ShopStore) UpdateStripeConnectAccount(ctx context.Context, shopID uuid.UUID, connectAccountID string) error {
	valid := connectAccountID != ""
	return s.queries.UpdateShopStripeConnectAccount(ctx, queries.UpdateShopStripeConnectAccountParams{
		ID:                     shopID,
		StripeConnectAccountID: pgtype.Text{String: connectAccountID, Valid: valid},
	})
}

func (s *ShopStore) UpdateStripeConnectDetails(ctx context.Context, shopID uuid.UUID, accountID string, detailsSubmitted, chargesEnabled, payoutsEnabled bool) error {
	return s.queries.UpdateShopStripeConnectAccount(ctx, queries.UpdateShopStripeConnectAccountParams{
		ID:                     shopID,
		StripeConnectAccountID: pgtype.Text{String: accountID, Valid: true},
	})
}

func (s *ShopStore) ReconnectShop(ctx context.Context, installationID int64, repoID int64) error {
	return s.queries.ReconnectShop(ctx, queries.ReconnectShopParams{
		GithubInstallationID: installationID,
		GithubRepoID:         repoID,
	})
}

func (s *ShopStore) DisconnectShop(ctx context.Context, installationID int64, repoID int64) error {
	return s.queries.DisconnectShop(ctx, queries.DisconnectShopParams{
		GithubInstallationID: installationID,
		GithubRepoID:         repoID,
	})
}

func (s *ShopStore) SuspendShop(ctx context.Context, installationID int64, repoID int64) error {
	return s.queries.DisconnectShop(ctx, queries.DisconnectShopParams{
		GithubInstallationID: installationID,
		GithubRepoID:         repoID,
	})
}

func (s *ShopStore) UnsuspendShop(ctx context.Context, installationID int64, repoID int64) error {
	return s.queries.ReconnectShop(ctx, queries.ReconnectShopParams{
		GithubInstallationID: installationID,
		GithubRepoID:         repoID,
	})
}

func (s *ShopStore) GetConnectedShopsByInstallationID(ctx context.Context, installationID int64) ([]*Shop, error) {
	rows, err := s.queries.GetConnectedShopsByInstallationID(ctx, installationID)
	if err != nil {
		return nil, err
	}

	shops := make([]*Shop, 0, len(rows))
	for _, row := range rows {
		shops = append(shops, s.convertShop(row))
	}

	return shops, nil
}

func (s *ShopStore) GetDistinctInstallationIDs(ctx context.Context) ([]int64, error) {
	return s.queries.GetDistinctInstallationIDs(ctx)
}

func (s *ShopStore) CountShopsByInstallationID(ctx context.Context, installationID int64) (int, error) {
	count, err := s.queries.CountShopsByInstallationID(ctx, installationID)
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

func (s *ShopStore) GetFirstConfiguredShop(ctx context.Context, installationID int64) (*Shop, error) {
	shop, err := s.queries.GetFirstConfiguredShop(ctx, installationID)
	if err != nil {
		return nil, err
	}

	return s.convertShop(shop), nil
}

type emailConfigData struct {
	APIKey    string `json:"api_key"`
	FromEmail string `json:"from_email"`
	From      string `json:"from"`
	Domain    string `json:"domain"`
	BaseURL   string `json:"base_url"`
}

func decodeEmailConfig(data []byte) (emailConfigData, error) {
	var cfg emailConfigData
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func decodeEmailConfigMap(data map[string]any) (emailConfigData, error) {
	var cfg emailConfigData
	if data == nil {
		return cfg, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c emailConfigData) toMap() map[string]any {
	out := map[string]any{}
	if c.APIKey != "" {
		out["api_key"] = c.APIKey
	}
	if c.FromEmail != "" {
		out["from_email"] = c.FromEmail
	}
	if c.From != "" {
		out["from"] = c.From
	}
	if c.Domain != "" {
		out["domain"] = c.Domain
	}
	if c.BaseURL != "" {
		out["base_url"] = c.BaseURL
	}
	return out
}
