ALTER TABLE negotiation_rounds
    DROP COLUMN message_source,
    DROP COLUMN offer_payment_terms_days,
    DROP COLUMN seller_payment_terms_days;

ALTER TABLE negotiation_strategies
    DROP CONSTRAINT negotiation_strategies_terms_order_check,
    DROP CONSTRAINT negotiation_strategies_weights_check,
    DROP COLUMN terms_weight,
    DROP COLUMN min_payment_terms_days,
    DROP COLUMN preferred_payment_terms_days;

ALTER TABLE quote_versions DROP COLUMN payment_terms_days;
ALTER TABLE quotes DROP COLUMN payment_terms_days;
