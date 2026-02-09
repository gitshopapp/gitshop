# GitShop Agent Guide

This document helps AI agents work effectively in the GitShop codebase.

## Project Overview

GitShop is a GitHub-native commerce platform that turns any GitHub repository into a transparent storefront. Customers place orders via GitHub issues, and sellers manage orders through GitHub comments (ChatOps).

**Key Technologies:**
- Go 1.25+ with standard library first approach
- PostgreSQL 17 for data persistence
- Docker & Docker Compose for development
- GitHub App integration (webhooks, JWT auth)
- Stripe Connect integration (checkout, webhooks)
- sqlc for type-safe SQL
- templ for type-safe HTML templates
- HTMX for dynamic UI interactions

## Essential Commands

### Dev Workflow (Docker)
Development uses Docker/Docker Compose via the Makefile.
```bash
# Build the app (Docker build pipeline)
make dbuild

# Run the app (Docker Compose, background)
make dup

# Run tests in Docker
make dtest

# Run linter in Docker
make dlint
```

### Development
```bash
# Start development environment (builds and runs everything)
make docker.up

# Stop all services
make docker.down

# First-time setup (creates .env, runs migrations)
make docker.dev-setup

# Run tests
make docker.test
# or locally: make test

# Run linter
make docker.lint
# or locally: make lint

# Regenerate sqlc code from SQL queries
make docker.sqlc
# or locally: make sqlc

# Regenerate templ templates
make templ

# Build UI (Tailwind + templ) inside Docker
make docker.ui.build

# Full Docker build pipeline (image + templ + UI + go build)
make docker.build
```

### Docker Shortcuts
```bash
dup   = docker.up.d      # Start services in background
dt    = docker.test      # Run tests in Docker
dlint = docker.lint      # Run linter in Docker
dlogs = docker.logs      # View logs
ddown = docker.down      # Stop services
```

### Database Migrations
```bash
# Run all pending migrations
make docker.migrate.up

# Rollback last migration
make docker.migrate.down

# Force migration version (use when migration is dirty)
make docker.migrate.force V=20240129000001
```

### External Tools
```bash
# Start ngrok tunnel for webhook testing (port 8080)
make ngrok
# Then update GitHub App webhook URL to: https://<ngrok-url>/webhooks/github
# And Stripe webhook URL to: https://<ngrok-url>/webhooks/stripe
```

## Code Organization

```
cmd/server/main.go              # Entry point, HTTP server setup

internal/
├── config/config.go            # Environment configuration with validation
├── cache/
│   ├── provider.go             # Cache provider interface
│   ├── memory.go               # In-memory cache implementation
│   └── redis.go                # Redis cache implementation
├── db/
│   ├── db.go                   # Database connection (pgxpool)
│   ├── shops.go                # Shop store (wraps sqlc queries)
│   ├── orders.go              # Order store (wraps sqlc queries)
│   └── queries/                # sqlc generated code
│       ├── *.sql               # SQL queries
│       ├── *.sql.go            # Generated Go code
│       └── models.go           # Generated models
├── catalog/
│   ├── parser.go               # gitshop.yaml parsing
│   ├── validator.go            # Config validation
│   ├── pricer.go              # Price calculation
│   └── syncer.go              # Issue template syncer
├── githubapp/
│   ├── auth.go                 # JWT & installation token auth
│   ├── client.go               # GitHub API client wrapper
│   └── webhooks.go            # Webhook signature validation
├── stripe/
│   ├── connect.go              # Stripe Connect platform client
│   └── webhooks.go            # Stripe webhook validation
├── handlers/
│   ├── handlers.go             # Main handler struct
│   ├── github_webhook.go      # GitHub webhook processing
│   ├── stripe_webhook.go      # Stripe webhook processing
│   ├── admin.go               # Admin panel handlers
│   ├── auth.go               # GitHub OAuth authentication
│   ├── shop_selection.go      # Multi-shop selection handler
│   └── stripe_connect.go     # Stripe Connect onboarding
├── email/
│   ├── provider.go             # Email provider interface
│   ├── postmark.go             # Postmark implementation
│   └── mailgun.go             # Mailgun implementation
└── crypto/
    └── crypto.go              # AES-256-GCM encryption for API keys

ui/
├── views/                      # Current templ UI
│   ├── layout.templ            # Base layout + scripts
│   ├── setup.templ             # Explicit setup checklist
│   ├── dashboard.templ         # Orders + storefront status
│   └── settings.templ          # Stripe/email settings
├── components/                 # templUI components
└── assets/                     # CSS/JS assets

migrations/                     # Database migrations (golang-migrate format)
```

## Architecture Patterns

### Handler Pattern
All HTTP handlers are methods on a central `Handlers` struct:
```go
type Handlers struct {
    config            *config.Config
    db               *pgxpool.Pool
    shopStore        *db.ShopStore
    orderStore       *db.OrderStore
    cacheProvider    cache.Provider
    githubAuth       *githubapp.Auth
    githubClient     *githubapp.Client
    stripePlatform   *stripe.PlatformClient
    emailProvider    email.Provider
    sessionManager   *session.Manager
}
```

### Store Pattern
Data access uses Store structs that wrap sqlc-generated queries:
```go
// Store in shops.go, orders.go
type ShopStore struct {
    pool    *pgxpool.Pool
    queries *queries.Queries
}
func (s *ShopStore) GetByInstallationID(ctx context.Context, id int64) (*Shop, error)
```

### Cache Pattern
Caching uses a provider interface for webhook idempotency and caching:
```go
type Provider interface {
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key string, value string, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Close() error
}
```

Available implementations:
- `memory`: HashiCorp-style LRU-backed in-memory cache with TTL checks (default)
- `redis`: Redis-backed cache (uses go-redis)

## Engineering Standards (Required)
- Use dependency injection/inversion-of-control: initialize shared dependencies in `app/app.go` and pass them into services/handlers. Do not call `catalog.New...` or similar constructors deep inside request flows.
- Prefer `any` over `interface{}` in application code.
- Avoid transport coupling in services: handlers/routers map HTTP/GitHub/Stripe payloads into service input structs before invoking services.
- Use `errors.Is` / `errors.As` for error type checks.
- Do not use `panic` outside `cmd/server/main.go`; constructors should return errors.
- Avoid long-lived background goroutines unless they have explicit lifecycle management.
- Prefer typed structs plus marshal/unmarshal over ad-hoc map assertions (e.g. avoid repeated `val.(string)` paths).
- Keep comments only where context is non-obvious; remove comments that restate the code.
- For UI work, check existing templUI components first and prefer them over custom HTML controls. If a component requires JavaScript, ensure its `@component.Script()` is included in `ui/views/layout.templ`.

## Domain Models
- Core domain models live in `internal/models` (e.g. `Shop`, `Order`).
- `internal/db` is responsible for converting database rows into domain models.
- Keep DB and HTTP concerns at their boundaries; domain models should remain storage/transport agnostic.

## Go File Naming
- In package-oriented folders (for example `internal/services`), prefer concise file names like `order.go`, `stripe.go`, `installation.go` instead of repeating package words (`order_service.go`, etc.).

### Webhook Processing Flow
1. Validate signature (HMAC-SHA256 for GitHub, Stripe-Signature for Stripe)
2. Check idempotency (cache with 24-hour TTL)
3. Process event
4. Mark as processed in cache (only on success)

### Order State Machine
```
pending_payment → paid → shipped → delivered
     ↓
payment_failed / expired
```

### Order Workflow (Happy Path)
1. Customer opens issue using GitShop order template (marker required).
2. Webhook validates template + gitshop.yaml.
3. Order created in DB, Stripe Checkout Session created.
4. GitShop comments checkout link and adds `gitshop:status:pending-payment`.
5. Stripe webhook marks order paid, updates labels, deletes checkout link comment.
6. Seller uses ChatOps to mark shipped/delivered.

## Naming Conventions

### Go Code
- **Packages**: lowercase, no underscores (e.g., `githubapp`, `catalog`)
- **Files**: snake_case for multi-word (e.g., `github_webhook.go`)
- **Types**: PascalCase (e.g., `ShopRepository`, `OrderStatus`)
- **Constants**: PascalCase for exported, camelCase for internal
- **Interfaces**: `-er` suffix (e.g., `Provider`, `Querier`)
- **Error variables**: `Err` prefix (e.g., `ErrNotFound`)

### Database
- **Tables**: plural, snake_case (e.g., `shops`, `orders`)
- **Columns**: snake_case (e.g., `github_installation_id`)
- **Indexes**: `idx_<table>_<columns>`
- **Migrations**: `YYYYMMDDHHMMSS_description.up.sql` / `.down.sql`

### SQL Queries (sqlc)
- Use named queries with `-- name: QueryName :one/:many/:exec`
- `:one` for single row returns
- `:many` for multiple rows
- `:exec` for no return value

## Testing Patterns

### Unit Tests
```go
func TestParser_Parse(t *testing.T) {
    tests := []struct {
        name    string
        yaml    string
        wantErr bool
    }{
        { name: "valid config", yaml: "...", wantErr: false },
        { name: "invalid yaml", yaml: "...", wantErr: true },
    }
    
    parser := NewParser()
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            config, err := parser.ParseFromString(tt.yaml)
            if tt.wantErr && err == nil { t.Error("expected error") }
            // ... assertions
        })
    }
}
```

### Running Tests
- Tests run inside Docker container via `make docker.test`
- Database is available at `postgres:5432` inside container

## Configuration

### Required Environment Variables
All configuration is 12-factor style via environment variables:

```bash
# Database
DATABASE_URL=postgres://user:pass@host:5432/db

# Cache
CACHE_PROVIDER=memory|redis
REDIS_ADDR=localhost:6379

# GitHub App
GITHUB_APP_ID=your_app_id
GITHUB_WEBHOOK_SECRET=your_secret
GITHUB_PRIVATE_KEY_BASE64=base64_encoded_pem
GITHUB_CLIENT_ID=oauth_client_id
GITHUB_CLIENT_SECRET=oauth_client_secret

# Stripe
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
STRIPE_CONNECT_CLIENT_ID=ca_...
ENCRYPTION_KEY=32_byte_key_for_api_keys

# Email
EMAIL_PROVIDER=postmark|mailgun
EMAIL_FROM=orders@yourstore.com
# Postmark:
POSTMARK_API_KEY=...
# Mailgun:
MAILGUN_API_KEY=...
MAILGUN_DOMAIN=mg.yourstore.com

# App
BASE_URL=https://your-domain.com
PORT=8080
LOG_LEVEL=info|debug
```

## Important Gotchas

### GitHub App Authentication
- **JWT** is used for app-level API calls (10 min expiry)
- **Installation tokens** are used for repo-level operations (1 hour expiry)
- Private key must be base64 encoded in env var

### Webhook Security
- GitHub webhooks use `X-Hub-Signature-256` header (HMAC-SHA256)
- Stripe webhooks use `Stripe-Signature` header
- Always verify signatures before processing
- Idempotency check uses cache (24-hour TTL) to prevent duplicate processing
- Stripe webhooks must be configured for **Connected accounts** (Connect events) since checkout sessions are created on connected accounts.

### Database
- sqlc generates code from SQL files - run `sqlc generate` after modifying queries
- Migrations use golang-migrate format (timestamp + description)
- JSONB columns for flexible config storage
- Use `pgtype.Text` for nullable strings in sqlc models

### Multi-tenancy
- Shops identified by `github_installation_id`
- Each shop has separate Stripe/email config
- Orders scoped to shop_id

### Setup Is Explicit (No Magic Writes)
GitShop no longer auto-creates repo resources. Setup requires user-triggered actions in the UI:
- Create GitHub labels
- Create `gitshop.yaml` (commit or PR if branch is protected)
- Create order template (commit or PR)
- Stripe + Email setup
Dashboard access is blocked until all are complete.

### GitHub Labels (Required)
All labels are **gitshop-prefixed**:
- `gitshop:order`
- `gitshop:status:pending-payment`
- `gitshop:status:paid`
- `gitshop:status:shipped`
- `gitshop:status:delivered`
- `gitshop:status:expired`

### Order Templates
- Stored in `.github/ISSUE_TEMPLATE/*.yml|*.yaml`
- Must include marker comment **`# gitshop:order-template`** (or `# GITSHOP_ORDER_TEMPLATE`)
- Must include label `gitshop:order`
- SKUs in template must match `gitshop.yaml`
- Prices and option values are validated in admin UI
- Multiple templates allowed if each has the marker comment

### Syncing Templates
- “Sync Template” button regenerates template content from `gitshop.yaml`
- If branch protected, opens PR
- Sync targets **all marker templates** (not filename-based)

### Checkout Link Hygiene
- Checkout comment includes `<!-- gitshop:checkout-link -->`
- Checkout link comment is deleted once payment succeeds
- Retry command: `.gitshop retry` (issue author or repo admin only)

## SQLC Usage

### Adding New Queries
1. Add SQL to `internal/db/queries/<table>.sql`:
```sql
-- name: GetShopByInstallationID :one
SELECT * FROM shops WHERE github_installation_id = $1;
```

2. Regenerate code:
```bash
make docker.sqlc
```

3. Use generated code:
```go
queries := queries.New(db)
shop, err := queries.GetShopByInstallationID(ctx, installationID)
```

## Docker Development

### Services
- **postgres**: PostgreSQL 17 (port 5432)
- **redis**: Redis 7 (port 6379)
- **gitshop-dev**: Go dev server with hot reload (Air) on port 8080

### Hot Reload
Air is configured via `.air.toml`:
- Watches `.go`, `.yaml`, `.yml` files
- Excludes `tmp/`, `vendor/`, `testdata/`
- Rebuilds on change

## GitShop YAML Format

Storefronts use `gitshop.yaml` in repo root:
```yaml
shop:
  name: "Your Shop"
  currency: "usd"
  manager: "octocat"
  shipping:
    flat_rate_cents: 900
    carrier: "USPS Priority"

products:
  - sku: "PRODUCT_V1"
    name: "Your Product"
    description: "Description"
    unit_price_cents: 2000
    active: true
    options:
      - name: "quantity"
        label: "Quantity"
        type: "dropdown"
        required: true
        values: [1, 2, 3, 4, 5]
```

## Order Issue Template Format

Marker + label required:
```yaml
# gitshop:order-template
name: 🛒 Place an Order
labels: ["gitshop:order", "gitshop:status:pending-payment"]
```

## ChatOps Commands

Sellers use GitHub issue comments:
- `.gitshop retry` - Retry checkout link creation (issue author or repo admin)

Commands only work for users with write access to the repo.
Tracking details are entered in the admin dashboard and emailed to customers (not posted to GitHub).

## What's NOT In Scope

- **Refunds** - Handled in seller's Stripe dashboard
- **Inventory management** - Managed in gitshop.yaml by seller
- **Analytics dashboard** - Use Stripe/GitHub dashboards
- **Multi-currency** - USD only
- **International shipping** - US addresses only

## Useful Resources

- `docs/ARCHITECTURE.md` - System architecture diagrams
- `docs/LOCAL_TESTING.md` - Detailed local setup guide
- `ROADMAP.md` - Feature roadmap and status
- `IMPLEMENTATION_SUMMARY.md` - What's implemented
