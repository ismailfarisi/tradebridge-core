-- name: CreateAttachment :one
INSERT INTO attachments (company_id, filename, mime, size_bytes, sha256)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = $1;

-- name: DeleteAttachment :exec
DELETE FROM attachments WHERE id = $1;

-- name: SetAttachmentsRFQ :exec
UPDATE attachments SET rfq_id = @rfq_id
WHERE id = ANY(@ids::text[]) AND company_id = @company_id;

-- name: SellerMatchedToRFQ :one
SELECT EXISTS(
    SELECT 1 FROM rfq_matches
    WHERE rfq_id = $1 AND seller_company_id = $2
) AS matched;
