-- name: UpsertQuote :one
INSERT INTO quotes (rfq_id, seller_company_id, agent_command_id, price_cents, currency, delivery_days, notes, completeness, payment_terms_days)
VALUES ($1, $2, sqlc.narg(agent_command_id), $3, $4, sqlc.narg(delivery_days), $5, $6, sqlc.narg(payment_terms_days))
ON CONFLICT (rfq_id, seller_company_id) DO UPDATE
SET agent_command_id = EXCLUDED.agent_command_id,
    price_cents      = EXCLUDED.price_cents,
    currency         = EXCLUDED.currency,
    delivery_days    = EXCLUDED.delivery_days,
    notes            = EXCLUDED.notes,
    completeness     = EXCLUDED.completeness,
    payment_terms_days = COALESCE(EXCLUDED.payment_terms_days, quotes.payment_terms_days),
    status           = 'submitted',
    updated_at       = NOW()
RETURNING *;

-- name: GetQuote :one
SELECT * FROM quotes WHERE id = $1;

-- name: GetQuoteBySeller :one
SELECT * FROM quotes WHERE rfq_id = $1 AND seller_company_id = $2;

-- name: RankQuotesByRFQ :many
SELECT q.*, c.name AS seller_name, c.verification_status,
       COALESCE(rep.avg_rating, 0)::float8 AS seller_avg_rating,
       COALESCE(rep.review_count, 0)::bigint AS seller_review_count
FROM quotes q
JOIN companies c ON c.id = q.seller_company_id
LEFT JOIN LATERAL (
    SELECT AVG(rating)::float8 AS avg_rating, COUNT(*)::bigint AS review_count
    FROM reviews WHERE reviewee_company_id = q.seller_company_id AND status = 'visible'
) rep ON true
WHERE q.rfq_id = $1
ORDER BY q.price_cents ASC, q.delivery_days ASC NULLS LAST, q.completeness DESC, q.created_at ASC;

-- name: AcceptQuote :one
-- Only a still-submitted quote can be accepted: a concurrent accept that got
-- there first leaves zero rows (pgx.ErrNoRows), never a second winner.
UPDATE quotes SET status = 'accepted', updated_at = NOW()
WHERE id = $1 AND status = 'submitted'
RETURNING *;

-- name: LockRFQForAccept :one
-- Row-locks the RFQ for the rest of the accept transaction so two accepts on
-- the same RFQ (different quotes, different API instances) serialise.
SELECT * FROM rfqs WHERE id = $1 FOR UPDATE;

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

-- name: UpsertQuoteVersion :one
-- Idempotent per (quote_id, version): a seller resubmit keeps v1 current.
INSERT INTO quote_versions (quote_id, version, price_cents, currency, delivery_days, notes, completeness, source, agent_command_id, payment_terms_days)
VALUES (@quote_id, @version, @price_cents, @currency, @delivery_days, @notes, @completeness, @source, sqlc.narg(agent_command_id), sqlc.narg(payment_terms_days))
ON CONFLICT (quote_id, version) DO UPDATE
SET price_cents = EXCLUDED.price_cents, currency = EXCLUDED.currency,
    delivery_days = EXCLUDED.delivery_days, notes = EXCLUDED.notes,
    completeness = EXCLUDED.completeness, source = EXCLUDED.source,
    agent_command_id = EXCLUDED.agent_command_id, payment_terms_days = EXCLUDED.payment_terms_days
RETURNING *;

-- name: BumpQuoteHead :one
-- Applies a counter's terms to the head and increments the version atomically.
UPDATE quotes
SET price_cents = @price_cents, currency = @currency, delivery_days = @delivery_days,
    notes = @notes, completeness = @completeness, payment_terms_days = COALESCE(sqlc.narg(payment_terms_days), payment_terms_days),
    version = version + 1,
    status = 'submitted', updated_at = NOW()
WHERE id = @id
RETURNING *;

-- name: ListQuoteVersions :many
SELECT * FROM quote_versions WHERE quote_id = @quote_id ORDER BY version ASC;

-- name: ReviseSubmittedQuote :one
-- A seller's revised offer on its own live quote: new head terms plus a
-- version bump. Only a still-submitted quote can be revised, so a quote the
-- buyer accepted (or that lost) in the meantime returns no row.
UPDATE quotes
SET price_cents = @price_cents, currency = @currency, delivery_days = @delivery_days,
    notes = @notes, completeness = @completeness, payment_terms_days = COALESCE(sqlc.narg(payment_terms_days), payment_terms_days),
    version = version + 1,
    agent_command_id = NULL, updated_at = NOW()
WHERE id = @id AND seller_company_id = @seller_company_id AND status = 'submitted'
RETURNING *;
