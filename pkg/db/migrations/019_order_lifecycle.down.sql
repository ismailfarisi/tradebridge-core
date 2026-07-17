DROP TABLE IF EXISTS order_events;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
