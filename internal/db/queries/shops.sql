-- name: GetShopByID :one
SELECT id, github_installation_id, github_repo_id, github_repo_full_name, owner_email,
       email_provider, email_config, email_verified,
       stripe_connect_account_id, disconnected_at, created_at, updated_at, onboarded_at
FROM shops
WHERE id = $1;

-- name: GetShopByInstallationID :one
SELECT id, github_installation_id, github_repo_id, github_repo_full_name, owner_email,
       email_provider, email_config, email_verified,
       stripe_connect_account_id, disconnected_at, created_at, updated_at, onboarded_at
FROM shops
WHERE github_installation_id = $1;

-- name: GetShopByRepoID :one
SELECT id, github_installation_id, github_repo_id, github_repo_full_name, owner_email,
       email_provider, email_config, email_verified,
       stripe_connect_account_id, disconnected_at, created_at, updated_at, onboarded_at
FROM shops
WHERE github_repo_id = $1;

-- name: GetShopByInstallationAndRepoID :one
SELECT id, github_installation_id, github_repo_id, github_repo_full_name, owner_email,
       email_provider, email_config, email_verified,
       stripe_connect_account_id, disconnected_at, created_at, updated_at, onboarded_at
FROM shops
WHERE github_installation_id = $1 AND github_repo_id = $2;

-- name: GetShopsByInstallationID :many
SELECT id, github_installation_id, github_repo_id, github_repo_full_name, owner_email,
       email_provider, email_config, email_verified,
       stripe_connect_account_id, disconnected_at, created_at, updated_at, onboarded_at
FROM shops
WHERE github_installation_id = $1
ORDER BY github_repo_full_name;

-- name: GetConnectedShopsByInstallationID :many
SELECT id, github_installation_id, github_repo_id, github_repo_full_name, owner_email,
       email_provider, email_config, email_verified,
       stripe_connect_account_id, disconnected_at, created_at, updated_at, onboarded_at
FROM shops
WHERE github_installation_id = $1 AND disconnected_at IS NULL
ORDER BY github_repo_full_name;

-- name: CreateShop :one
INSERT INTO shops (github_installation_id, github_repo_id, github_repo_full_name, owner_email)
VALUES ($1, $2, $3, $4)
RETURNING id, github_installation_id, github_repo_id, github_repo_full_name, owner_email,
          email_provider, email_config, email_verified,
          stripe_connect_account_id, disconnected_at, created_at, updated_at, onboarded_at;

-- name: UpdateShopRepoFullName :exec
UPDATE shops
SET github_repo_full_name = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: DisconnectShop :exec
UPDATE shops
SET disconnected_at = NOW(),
    stripe_connect_account_id = NULL,
    email_config = '{}',
    email_verified = FALSE,
    updated_at = NOW()
WHERE github_installation_id = $1 AND github_repo_id = $2;

-- name: ReconnectShop :exec
UPDATE shops
SET disconnected_at = NULL,
    updated_at = NOW()
WHERE github_installation_id = $1 AND github_repo_id = $2;

-- name: UpdateShopEmailConfig :exec
UPDATE shops
SET email_provider = $2, email_config = $3, email_verified = $4, updated_at = NOW()
WHERE id = $1;

-- name: UpdateShopStripeConnectAccount :exec
UPDATE shops
SET stripe_connect_account_id = $2, updated_at = NOW()
WHERE id = $1;

-- name: MarkShopOnboarded :exec
UPDATE shops
SET onboarded_at = COALESCE(onboarded_at, NOW()),
    updated_at = NOW()
WHERE id = $1;

-- name: GetDistinctInstallationIDs :many
SELECT DISTINCT github_installation_id FROM shops WHERE github_installation_id IS NOT NULL;

-- name: CountShopsByInstallationID :one
SELECT COUNT(*) FROM shops WHERE github_installation_id = $1;

-- name: GetFirstConfiguredShop :one
SELECT id, github_installation_id, github_repo_id, github_repo_full_name, owner_email,
       email_provider, email_config, email_verified,
       stripe_connect_account_id, disconnected_at, created_at, updated_at, onboarded_at
FROM shops
WHERE github_installation_id = $1
  AND stripe_connect_account_id IS NOT NULL
  AND email_verified = true
LIMIT 1;
