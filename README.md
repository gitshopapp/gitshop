<p align="center">
  <img src="docs/assets/gitshop-logo.png" alt="GitShop logo" width="250" />
</p>

# GitShop 🛍️

Sell from the place you already work: GitHub.

GitShop turns any GitHub repository into a transparent storefront. Customers place orders through GitHub Issues, pay with Stripe Checkout, and sellers manage setup and fulfillment from one GitShop admin dashboard.

See it live: [gitcoffee](https://github.com/gitshopapp/gitcoffee), the first working shop built on GitShop.

![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)
![Stripe](https://img.shields.io/badge/Payments-Stripe-635BFF?logo=stripe&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-111111)

[Quick Start](#quick-start-) • [How It Works](#how-it-works-) • [Local Development](#local-development-) • [Need Help](#need-help-) • [Admin Login](http://gitshop.app)

## What Is GitShop? ✨

GitShop is for creators and teams who want a lightweight, GitHub-native way to sell products without running a separate ecommerce site.

- Product catalog in `gitshop.yaml`
- Order forms in GitHub issue templates
- Order status tracked with GitHub labels
- Payments and payouts through Stripe
- Seller operations managed in the GitShop admin dashboard

## Quick Start 🚀

1. Install the [GitShop GitHub App](https://github.com/apps/gitshopapp) on your repository.
2. Sign in to [GitShop](http://gitshop.app) with GitHub.
3. Select your repository/shop.
4. Complete the setup checklist in the dashboard.

## How It Works 🔄

1. Define your catalog in `gitshop.yaml`.
2. Create an issue template in `.github/ISSUE_TEMPLATE/*.yaml` with the marker `# gitshop:order-template` and label `gitshop:order`.
3. A customer opens an issue from that order template.
4. GitShop validates the order and posts a Stripe Checkout link.
5. After payment, GitShop updates order labels and removes the checkout-link comment.
6. You manage shipping and delivery from the admin dashboard.

## `gitshop.yaml` Example 🧾

```yaml
shop:
  name: "My Shop"
  currency: "usd"
  manager: "octocat"
  shipping:
    flat_rate_cents: 500
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

## Current Limitations ⚠️

- USD only
- US shipping only
- Flat-rate shipping only
- One product SKU per order issue
- Refunds are handled in Stripe (not GitShop)
- Products are managed manually in `gitshop.yaml`
- If products need different option schemas, use separate order templates

## Need Help? 🤝

- Issues: https://github.com/gitshopapp/gitshop/issues

## Local Development 🛠️

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

Common commands:

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

## Architecture Quick Map 🧭

- `cmd/server/main.go`: entrypoint
- `app/`: application wiring
- `internal/handlers`: HTTP and webhook transport
- `internal/services`: business logic
- `internal/db`: persistence layer
- `internal/models`: domain models
- `ui/`: templ views/components/assets

For full project conventions, workflows, and architecture details, see `AGENTS.md`.
