-- name: UpsertQuote :one
INSERT INTO quotes (rfq_id, seller_company_id, agent_command_id, price_cents, currency, delivery_days, notes, completeness)
VALUES ($1, $2, sqlc.narg(agent_command_id), $3, $4, sqlc.narg(delivery_days), $5, $6)
ON CONFLICT (rfq_id, seller_company_id) DO UPDATE
SET agent_command_id = EXCLUDED.agent_command_id,
    price_cents      = EXCLUDED.price_cents,
    currency         = EXCLUDED.currency,
    delivery_days    = EXCLUDED.delivery_days,
    notes            = EXCLUDED.notes,
    completeness     = EXCLUDED.completeness,
    status           = 'submitted',
    updated_at       = NOW()
RETURNING *;

-- name: GetQuote :one
SELECT * FROM quotes WHERE id = $1;

-- name: GetQuoteBySeller :one
SELECT * FROM quotes WHERE rfq_id = $1 AND seller_company_id = $2;

-- name: RankQuotesByRFQ :many
SELECT q.*, c.name AS seller_name, c.verification_status
FROM quotes q
JOIN companies c ON c.id = q.seller_company_id
WHERE q.rfq_id = $1
ORDER BY q.price_cents ASC, q.delivery_days ASC NULLS LAST, q.completeness DESC, q.created_at ASC;

-- name: AcceptQuote :one
UPDATE quotes SET status = 'accepted', updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: RejectOtherQuotes :exec
UPDATE quotes SET status = 'rejected', updated_at = NOW()
WHERE rfq_id = $1 AND id <> $2 AND status = 'submitted';

-- name: ListSubmittedQuoteSellersExcept :many
SELECT id, seller_company_id
FROM quotes
WHERE rfq_id = $1 AND id <> $2 AND status = 'submitted';

-- name: CreateQuoteMessage :one
INSERT INTO quote_messages (quote_id, author, body, attachments)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListQuoteMessages :many
SELECT * FROM quote_messages WHERE quote_id = $1 ORDER BY created_at;

-- name: CreateOrder :one
INSERT INTO orders (rfq_id, quote_id, buyer_company_id, seller_company_id, total_cents, currency)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetOrder :one
SELECT * FROM orders WHERE id = $1;

-- name: CountSubmittedQuotesByRFQ :one
SELECT COUNT(*) FROM quotes WHERE rfq_id = $1 AND status = 'submitted';
