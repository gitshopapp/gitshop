-- name: CreateOrder :one
INSERT INTO orders (
    shop_id, github_issue_number, order_number, github_issue_url, github_username, sku,
    options, subtotal_cents, shipping_cents, tax_cents, total_cents,
    stripe_checkout_session_id, customer_email, customer_name, shipping_address, status
) VALUES (
    $1, $2, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
RETURNING id, shop_id, github_issue_number, order_number, github_issue_url, github_username, sku,
          options, subtotal_cents, shipping_cents, tax_cents, total_cents,
          stripe_checkout_session_id, stripe_payment_intent_id, customer_email, customer_name,
          shipping_address, tracking_number, tracking_url, carrier, status,
          created_at, paid_at, shipped_at, delivered_at;

-- name: GetOrderByStripeSessionID :one
SELECT id, shop_id, github_issue_number, order_number, github_issue_url, github_username, sku,
       options, subtotal_cents, shipping_cents, tax_cents, total_cents,
       stripe_checkout_session_id, stripe_payment_intent_id, customer_email, customer_name,
       shipping_address, tracking_number, tracking_url, carrier, status,
       created_at, paid_at, shipped_at, delivered_at
FROM orders 
WHERE stripe_checkout_session_id = $1;

-- name: GetOrderByID :one
SELECT id, shop_id, github_issue_number, order_number, github_issue_url, github_username, sku,
       options, subtotal_cents, shipping_cents, tax_cents, total_cents,
       stripe_checkout_session_id, stripe_payment_intent_id, customer_email, customer_name,
       shipping_address, tracking_number, tracking_url, carrier, status,
       created_at, paid_at, shipped_at, delivered_at
FROM orders
WHERE id = $1;

-- name: GetOrderByIssueNumber :one
SELECT id, shop_id, github_issue_number, order_number, github_issue_url, github_username, sku,
       options, subtotal_cents, shipping_cents, tax_cents, total_cents,
       stripe_checkout_session_id, stripe_payment_intent_id, customer_email, customer_name,
       shipping_address, tracking_number, tracking_url, carrier, status,
       created_at, paid_at, shipped_at, delivered_at
FROM orders
WHERE shop_id = $1 AND github_issue_number = $2;

-- name: GetOrdersByShop :many
SELECT id, shop_id, github_issue_number, order_number, github_issue_url, github_username, sku,
       options, subtotal_cents, shipping_cents, tax_cents, total_cents,
       stripe_checkout_session_id, stripe_payment_intent_id, customer_email, customer_name,
       shipping_address, tracking_number, tracking_url, carrier, status,
       created_at, paid_at, shipped_at, delivered_at
FROM orders 
WHERE shop_id = $1 
ORDER BY created_at DESC 
LIMIT $2;

-- name: UpdateOrderStatus :exec
UPDATE orders 
SET status = $2
WHERE id = $1;

-- name: UpdateOrderPaid :exec
UPDATE orders 
SET status = 'paid', paid_at = NOW()
WHERE id = $1;

-- name: UpdateOrderShipping :exec
UPDATE orders 
SET status = 'shipped', tracking_number = $2, carrier = $3, shipped_at = NOW()
WHERE id = $1;

-- name: UpdateOrderDelivered :exec
UPDATE orders 
SET status = 'delivered', delivered_at = NOW()
WHERE id = $1;
