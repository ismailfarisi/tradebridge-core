-- name: CreateInvoice :one
-- invoice_number is generated in SQL so it is atomic with the row insert.
INSERT INTO invoices (order_id, invoice_number, buyer_company_id, seller_company_id, currency, subtotal_cents, total_cents)
VALUES (
    @order_id,
    'INV-' || to_char(NOW(), 'YYYY') || '-' || lpad(nextval('invoice_number_seq')::text, 6, '0'),
    @buyer_company_id, @seller_company_id, @currency, @subtotal_cents, @total_cents
)
RETURNING *;

-- name: CreateInvoiceLineItem :one
INSERT INTO invoice_line_items (invoice_id, description, quantity, unit_price_cents, amount_cents)
VALUES (@invoice_id, @description, @quantity, @unit_price_cents, @amount_cents)
RETURNING *;

-- name: GetInvoiceByOrder :one
SELECT * FROM invoices WHERE order_id = @order_id;

-- name: ListInvoiceLineItems :many
SELECT * FROM invoice_line_items WHERE invoice_id = @invoice_id ORDER BY id;
