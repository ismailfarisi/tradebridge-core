-- Post-completion reputation: each party may review the other once per order.
-- direction is buyer_on_seller | seller_on_buyer; reviewee_company_id is the
-- subject. status flips to 'removed' by an admin (drops out of aggregates/lists).
CREATE TABLE reviews (
    id                  TEXT PRIMARY KEY DEFAULT gen_ulid(),
    order_id            TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    reviewer_company_id TEXT NOT NULL REFERENCES companies(id),
    reviewee_company_id TEXT NOT NULL REFERENCES companies(id),
    direction           TEXT NOT NULL CHECK (direction IN ('buyer_on_seller','seller_on_buyer')),
    rating              INT  NOT NULL CHECK (rating BETWEEN 1 AND 5),
    body                TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'visible' CHECK (status IN ('visible','removed')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (order_id, reviewer_company_id)
);

CREATE INDEX idx_reviews_reviewee ON reviews (reviewee_company_id);
