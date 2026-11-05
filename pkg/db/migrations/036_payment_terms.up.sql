-- Payment terms as a negotiable issue. payment_terms_days is the number of
-- days after delivery the buyer has to pay: 0 = cash / on delivery, 30, 60,
-- 90 ... NULL means the party did not state terms, which keeps every quote
-- and mandate written before this migration behaving exactly as before.
ALTER TABLE quotes
    ADD COLUMN payment_terms_days INT CHECK (payment_terms_days IS NULL OR payment_terms_days BETWEEN 0 AND 365);

ALTER TABLE quote_versions
    ADD COLUMN payment_terms_days INT CHECK (payment_terms_days IS NULL OR payment_terms_days BETWEEN 0 AND 365);

-- Mandate: the buyer's preferred terms (longer = better for cash flow), the
-- shortest terms they will accept, and how much terms matter. Weights are
-- shares of one: price_weight + terms_weight <= 1, delivery gets the rest, so
-- an existing mandate (terms_weight 0) keeps its price-vs-delivery split.
ALTER TABLE negotiation_strategies
    ADD COLUMN preferred_payment_terms_days INT
        CHECK (preferred_payment_terms_days IS NULL OR preferred_payment_terms_days BETWEEN 0 AND 180),
    ADD COLUMN min_payment_terms_days INT
        CHECK (min_payment_terms_days IS NULL OR min_payment_terms_days BETWEEN 0 AND 180),
    ADD COLUMN terms_weight DOUBLE PRECISION NOT NULL DEFAULT 0
        CHECK (terms_weight >= 0 AND terms_weight <= 1),
    ADD CONSTRAINT negotiation_strategies_weights_check CHECK (price_weight + terms_weight <= 1),
    ADD CONSTRAINT negotiation_strategies_terms_order_check CHECK (
        min_payment_terms_days IS NULL OR preferred_payment_terms_days IS NULL
        OR min_payment_terms_days <= preferred_payment_terms_days);

-- Rounds record the terms on both sides and whether the supplier-facing
-- message was phrased by the LLM or rendered from the deterministic template.
ALTER TABLE negotiation_rounds
    ADD COLUMN seller_payment_terms_days INT,
    ADD COLUMN offer_payment_terms_days  INT,
    ADD COLUMN message_source TEXT NOT NULL DEFAULT 'template' CHECK (message_source IN ('template', 'llm'));
