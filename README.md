# GitShop

Sell from the place you already work: GitHub.

GitShop turns any GitHub repository into a simple, transparent storefront. Customers place orders through GitHub Issues, pay through Stripe Checkout, and you manage setup and fulfillment from one GitShop admin dashboard.

## What Is GitShop?
GitShop is for creators and teams who want a lightweight, GitHub-native way to sell products without building a separate ecommerce site.

With GitShop, your repository becomes your storefront system:
- Product catalog in `gitshop.yaml`
- Order forms in GitHub issue templates
- Order history and status visible in GitHub
- Payments handled by Stripe
- Seller operations handled in GitShop admin

## Why Sellers Like It
- No separate storefront app to maintain
- Clear audit trail of orders in GitHub
- Fast setup checklist in one place
- Stripe-powered checkout and payouts
- Built-in order status + shipping notifications

## Install GitShop
Getting started is straightforward:

1. Install the GitShop GitHub App on your repository.
2. Sign in to GitShop with GitHub.
3. Select your repository/shop.
4. Complete the guided setup checklist:
   - Connect Stripe
   - Configure email provider
   - Create required labels
   - Create `gitshop.yaml`
   - Create order template(s)

## How To Use GitShop

### 1. Define your catalog
Add `gitshop.yaml` to your repository root. This is your source of truth for products, prices, and options.

```yaml
shop:
  name: "My Shop"
  currency: "usd"
  manager: "octocat"
  shipping:
    flat_rate_cents: 900
    carrier: "USPS Priority"

products:
  - sku: "TSHIRT_BLACK_V1"
    name: "T-Shirt"
    description: "Black logo tee"
    unit_price_cents: 2500
    active: true
    options:
      - name: "size"
        label: "Size"
        type: "dropdown"
        required: true
        values: ["S", "M", "L", "XL"]
```

### 2. Create your order form
Create an issue template in `.github/ISSUE_TEMPLATE/*.yaml` with:
- Marker comment: `# gitshop:order-template`
- Label: `gitshop:order`

### 3. Accept orders
Customers open issues using your order template.
GitShop validates the order and posts a Stripe Checkout link.

### 4. Track payments and fulfillment
After payment, GitShop updates order labels and removes checkout-link comments.
You manage shipping and delivery from the admin dashboard.

### 5. Handle checkout retries when needed
If checkout-link creation fails, the issue author or repo admin can comment:

```text
.gitshop retry
```

## Required Labels
GitShop uses these labels for order state:
- `gitshop:order`
- `gitshop:status:pending-payment`
- `gitshop:status:paid`
- `gitshop:status:shipped`
- `gitshop:status:delivered`
- `gitshop:status:expired`

## Limitations
- USD only
- US shipping only
- Flat-rate shipping only
- One product SKU per order issue
- Refunds are handled in Stripe (not GitShop)
- Inventory is managed manually in `gitshop.yaml`
- If products need different option schemas, use separate order templates

## Need Help?
- Issues: https://github.com/gitshopapp/gitshop/issues
- Discussions: https://github.com/gitshopapp/gitshop/discussions

## Developers/Contributing

### Run GitShop locally
1. Copy `.env.example` to `.env` and fill required values.
2. Start local services:

```bash
make docker.dev-setup
make docker.up
```

3. Stop services:

```bash
make docker.down
```

### Common Make commands

```bash
# Local host commands
make test
make lint
make build

# Docker workflow commands
make docker.test
make docker.lint
make docker.sqlc
make docker.templ
make docker.ui.build
make docker.build
```

### Engineering standards (high level)
- Use dependency injection from `app/app.go`
- Keep handlers transport-focused; business logic in services
- Use `errors.Is` / `errors.As` for error handling
- Avoid `panic` outside `cmd/server/main.go`
- Prefer typed structs over ad-hoc map assertions
- Keep solutions simple and maintainable (KISS)

### Architecture (quick map)
- `cmd/server/main.go`: entrypoint
- `app/`: application wiring
- `internal/handlers`: HTTP and webhook transport
- `internal/services`: business logic
- `internal/db`: persistence layer
- `internal/models`: domain models
- `ui/`: templ views/components/assets

For full project conventions, workflows, and architecture details, see `AGENTS.md`.
