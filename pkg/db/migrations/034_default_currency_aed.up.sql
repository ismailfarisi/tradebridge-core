-- UAE/GCC first: new quotes, orders and invoices default to AED.
-- Existing rows keep their recorded currency.
ALTER TABLE quotes ALTER COLUMN currency SET DEFAULT 'AED';
ALTER TABLE quote_versions ALTER COLUMN currency SET DEFAULT 'AED';
ALTER TABLE orders ALTER COLUMN currency SET DEFAULT 'AED';
ALTER TABLE invoices ALTER COLUMN currency SET DEFAULT 'AED';
