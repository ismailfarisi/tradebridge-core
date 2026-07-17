-- name: SetOrderStatus :one
UPDATE orders SET status = @status WHERE id = @id RETURNING *;

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
