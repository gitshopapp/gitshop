CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE shops (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    github_installation_id BIGINT NOT NULL,
    github_repo_id BIGINT NOT NULL,
    github_repo_full_name TEXT NOT NULL,
    owner_email TEXT NOT NULL,
    email_provider TEXT DEFAULT 'postmark',
    email_config JSONB DEFAULT '{}',
    email_verified BOOLEAN DEFAULT FALSE,
    stripe_connect_account_id TEXT,
    disconnected_at TIMESTAMPTZ,
    onboarded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (github_installation_id, github_repo_id)
);

CREATE INDEX idx_shops_installation_id ON shops(github_installation_id);
CREATE INDEX idx_shops_repo_id ON shops(github_repo_id);

CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shop_id UUID NOT NULL REFERENCES shops(id),
    github_issue_number INTEGER NOT NULL,
    order_number INTEGER NOT NULL,
    github_issue_url TEXT,
    github_username TEXT NOT NULL,
    sku TEXT NOT NULL,
    options JSONB,
    subtotal_cents INTEGER NOT NULL,
    shipping_cents INTEGER NOT NULL,
    tax_cents INTEGER DEFAULT 0,
    total_cents INTEGER NOT NULL,
    stripe_checkout_session_id TEXT UNIQUE,
    stripe_payment_intent_id TEXT,
    customer_email TEXT,
    customer_name TEXT,
    shipping_address JSONB,
    tracking_number TEXT,
    tracking_url TEXT,
    carrier TEXT,
    failure_reason TEXT,
    status TEXT NOT NULL DEFAULT 'pending_payment',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    paid_at TIMESTAMPTZ,
    shipped_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    CONSTRAINT orders_order_number_matches_issue CHECK (order_number = github_issue_number)
);

CREATE UNIQUE INDEX idx_orders_shop_issue ON orders(shop_id, github_issue_number);
CREATE UNIQUE INDEX idx_orders_shop_order_number ON orders(shop_id, order_number);
CREATE INDEX idx_orders_stripe_session ON orders(stripe_checkout_session_id);
CREATE INDEX idx_orders_status ON orders(status);

COMMENT ON COLUMN shops.github_repo_id IS 'GitHub repository ID (numeric) - immutable and unique, used as primary lookup key';
COMMENT ON COLUMN shops.github_repo_full_name IS 'GitHub repository full name (e.g., owner/repo) - for display only, can change';
