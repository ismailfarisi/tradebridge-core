-- name: SetOrderStatus :one
-- Conditional on the expected current status so a concurrent transition (which
-- would have moved the order out of @from) cannot be silently overwritten:
-- 0 rows updated => the caller's validated read is stale (treat as conflict).
UPDATE orders SET status = @status WHERE id = @id AND status = @from_status RETURNING *;

-- name: ListOrdersByCompany :many
SELECT * FROM orders
WHERE buyer_company_id = @company_id OR seller_company_id = @company_id
ORDER BY created_at DESC;

-- name: CreateOrderEvent :one
INSERT INTO order_events (order_id, from_status, to_status, actor, actor_company_id, note)
VALUES (@order_id, @from_status, @to_status, @actor, sqlc.narg(actor_company_id), @note)
RETURNING *;

-- name: ListOrderEvents :many
SELECT * FROM order_events WHERE order_id = @order_id ORDER BY created_at ASC;
