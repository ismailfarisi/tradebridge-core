-- name: ListSellerRFQs :many
-- RFQs matched to one seller, newest match first. Tabs are mutually exclusive:
-- open   = still accepting quotes and this seller has not quoted yet,
-- quoted = still live and this seller has a quote on it,
-- closed = closed or expired, whatever this seller did.
-- unseen is true until the seller opens the RFQ detail (rfq_views row).
-- Draft RFQs are never shown: they have not finished matching.
SELECT r.id, r.title, r.raw_text, r.requirements, r.category, r.status, r.created_at, r.expires_at,
    m.score, m.reason, m.created_at AS matched_at,
    q.id AS quote_id, q.status AS quote_status, q.price_cents AS quote_price_cents,
    q.currency AS quote_currency,
    (v.company_id IS NULL)::boolean AS unseen
FROM rfq_matches m
JOIN rfqs r ON r.id = m.rfq_id
LEFT JOIN quotes q ON q.rfq_id = m.rfq_id AND q.seller_company_id = m.seller_company_id
LEFT JOIN rfq_views v ON v.rfq_id = m.rfq_id AND v.company_id = m.seller_company_id
WHERE m.seller_company_id = @company_id
  AND r.status <> 'draft'
  AND (@tab::text = ''
       OR (@tab::text = 'open'   AND r.status IN ('open','quoted') AND q.id IS NULL)
       OR (@tab::text = 'quoted' AND r.status IN ('open','quoted') AND q.id IS NOT NULL)
       OR (@tab::text = 'closed' AND r.status IN ('closed','expired')))
  AND (@q::text = '' OR r.title ILIKE '%' || @q::text || '%' OR r.raw_text ILIKE '%' || @q::text || '%')
ORDER BY m.created_at DESC, r.id DESC
LIMIT @lim OFFSET @off;

-- name: GetSellerRFQ :one
-- One RFQ as seen by a matched seller. No row means the seller is not matched
-- to it (or it does not exist); callers return 404 either way. The buyer's
-- intake chat (rfqs.messages) and buyer identity are deliberately left out.
SELECT r.id, r.title, r.raw_text, r.requirements, r.category, r.status, r.created_at, r.expires_at,
    m.score, m.reason, m.created_at AS matched_at
FROM rfq_matches m
JOIN rfqs r ON r.id = m.rfq_id
WHERE m.rfq_id = @rfq_id AND m.seller_company_id = @company_id AND r.status <> 'draft';

-- name: ListBuyerAttachmentsByRFQ :many
-- Attachments the buyer attached to an RFQ (not files sellers uploaded later).
SELECT a.* FROM attachments a
JOIN rfqs r ON r.id = a.rfq_id
WHERE a.rfq_id = @rfq_id AND a.company_id = r.buyer_company_id
ORDER BY a.created_at, a.id;
