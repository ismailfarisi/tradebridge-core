-- name: CreateReview :one
INSERT INTO reviews (order_id, reviewer_company_id, reviewee_company_id, direction, rating, body)
VALUES (@order_id, @reviewer_company_id, @reviewee_company_id, @direction, @rating, @body)
RETURNING *;

-- name: GetReview :one
SELECT * FROM reviews WHERE id = @id;

-- name: ListReviewsForOrder :many
SELECT * FROM reviews WHERE order_id = @order_id ORDER BY created_at ASC;

-- name: ListVisibleReviewsForCompany :many
SELECT r.*, c.name AS reviewer_name
FROM reviews r JOIN companies c ON c.id = r.reviewer_company_id
WHERE r.reviewee_company_id = @company_id AND r.status = 'visible'
ORDER BY r.created_at DESC;

-- name: GetCompanyReputation :one
SELECT COALESCE(AVG(rating), 0)::float8 AS avg_rating, COUNT(*)::bigint AS review_count
FROM reviews WHERE reviewee_company_id = @company_id AND status = 'visible';

-- name: RemoveReview :one
UPDATE reviews SET status = 'removed' WHERE id = @id RETURNING *;
