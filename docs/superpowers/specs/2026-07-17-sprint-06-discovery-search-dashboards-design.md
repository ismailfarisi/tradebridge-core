# Sprint 6 — Discovery, Search & Dashboards — Design Spec

**Date:** 2026-07-17 · **Sprint:** 6 of 10 · **Repos:** `tradebridge-core` (+ `web/`)
**Depends on:** Sprint 4 (reputation) · Sprint 5 · **Status:** design approved (2026-07-17)

## Goal

Let buyers find supply and let both sides see their business. Add Postgres-FTS search/discovery over
suppliers and catalog (ranked by relevance + trust), and turn the placeholder dashboards into real,
tenant-scoped aggregations, including seller performance analytics.

## Locked decisions (user, 2026-07-17)

1. **Search backend = Postgres FTS + trigram** — `tsvector` (generated columns + GIN) for relevance,
   `pg_trgm` for typo tolerance. No external engine (Meilisearch/Typesense deferred). No new infra.
2. **Discovery is public + SEO** — an unauthenticated `/public/v1/discovery` surface + marketplace/category
   pages, reusing the tenant-safe public projection; fail-open rate-limited; public columns only. A guarded
   `/v1/discovery` mirror serves the same ranking to logged-in buyers.
3. **Ranking = transparent weighted score** — `score = 0.6·ts_rank_norm + 0.25·(avg_rating/5) +
   0.15·verified`, recency tiebreak. Tunable weights, one `ORDER BY`, explainable.
4. **Full seller analytics now** — win rate, revenue won, top buyers, **and** average response time, plus the
   operational dashboard tiles.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never pushed** (both repos). `.env`/secrets never committed.
- DB/integration tests use `tradebridge_core_test`, **NEVER** live `tradebridge_core`.
- Public discovery exposes **only** whitelisted public columns (reuse the `GetPublicSeller` projection
  discipline — never `SELECT *` of `companies`, which would leak `stripe_customer_id` etc.).
- Rank weights are named constants in one place, not scattered magic numbers.
- New migration numbers start at **025** (live DB currently at v24).
- Follow existing patterns: sqlc queries in `pkg/db/query/*.sql`, handlers `NewHandler(...).Register(app,
  guard...)`, `middleware.CompanyID/UserID/Role`, fail-open limiter from Sprint 3, zerolog, ULID.

## Architecture

Three parallel tracks, no new infrastructure:

1. **Search / discovery** (`internal/discovery`, new) — FTS + trigram queries, weighted ranking, facets;
   public + authenticated endpoints.
2. **Buyer dashboard** (`internal/dashboard`, new) — tenant-scoped aggregation read APIs + RFQ list
   tabs/search + new-quote badges.
3. **Seller dashboard + analytics** (`internal/dashboard` + `internal/analytics`, new) — operational tiles +
   performance analytics.

Existing hot paths (quote matching/ranking, order state machine) are untouched.

## Data / migration (025_search_discovery)

- `CREATE EXTENSION IF NOT EXISTS pg_trgm;`
- `supplier_profiles`: add `search_vector tsvector GENERATED ALWAYS AS
  (to_tsvector('english', coalesce(description,'') || ' ' || coalesce(location,'') || ' ' ||
   array_to_string(categories,' '))) STORED;` + `CREATE INDEX ... USING GIN (search_vector);`
- `catalog_summaries`: same treatment over `name || category || coalesce(summary,'')`.
- Company name is matched at query time via `pg_trgm` (`companies.name` GIN trigram index) — the name lives
  on `companies`, so it stays a join-time signal rather than being mirrored into the profile vector.
- `rfq_views(company_id, rfq_id, last_viewed_at)` — lightweight per-buyer last-seen for new-quote badges
  (UPSERT on view; badge = quotes on the RFQ with `created_at > last_viewed_at`). One row per (company,rfq).
- Down migration drops the columns/indexes/extension/table.

## Track 1 — Search & discovery (Core, `internal/discovery`)

**Ranking (named constants):**
```
score = W_REL*ts_rank_norm + W_RATING*(avg_rating/5.0) + W_VERIFIED*(verified?1:0)
W_REL=0.6  W_RATING=0.25  W_VERIFIED=0.15   -- recency (updated_at) as final tiebreak
```
`ts_rank_norm` uses `ts_rank_cd` normalized to 0..1; when `q` is empty, relevance term is 0 and results
rank by trust + recency (a browse-all list).

**Endpoints:**
- `GET /public/v1/discovery?q=&category=&verified=&location=&min_rating=&sort=&page=&per_page=`
  → `{results:[{company_id, name, verification_status, verified_domain, description, categories, location,
     avg_rating, review_count, matched_catalog:[{name,category}], score}], page, per_page, total}`.
  Public columns only. `sort` ∈ `relevance`(default) | `rating` | `newest`. Fail-open rate-limited.
- `GET /public/v1/discovery/facets?q=&...` → `{categories:[{value,count}], verified:{true:n,false:n},
  rating_buckets:[{min,count}]}` — counts computed under the current filter set (excluding the facet's own
  dimension so a user can widen it).
- `GET /v1/discovery?...` (guarded) — same service, same ranking; identical projection (parity with public
  so internal pages and the marketplace agree on order).

Catalog search folds in as a **matched sub-signal**: a supplier whose catalog items match `q` gets its
matched items surfaced as `matched_catalog` snippets and a relevance contribution; catalog is not a separate
top-level result list.

## Track 2 — Buyer dashboard + RFQ list (Core, `internal/dashboard`)

- `GET /v1/dashboard/buyer` → `{active_rfqs, quotes_received, new_quotes, orders_in_progress,
  recent_rfqs:[{id,title,status,quote_count,new_quotes,created_at}]}`. All tenant-scoped to
  `middleware.CompanyID(c)`. `orders_in_progress` = orders in `created|confirmed|fulfilled`.
- `GET /v1/rfqs?tab=open|quoted|closed&q=&page=` — server-side tab filter + text search over title/text.
  Tabs: `open`→`open`; `quoted`→`quoted`; `closed`→`completed|cancelled|expired`. Each row carries
  `new_quotes` (quotes since the buyer's `rfq_views.last_viewed_at`).
- `POST /v1/rfqs/:id/view` (or fold into the existing RFQ GET) upserts `rfq_views.last_viewed_at=NOW()` for
  the buyer, clearing that RFQ's badge.

## Track 3 — Seller dashboard + analytics (Core, `internal/dashboard` + `internal/analytics`)

- `GET /v1/dashboard/seller` → `{incoming_rfqs, review_queue, active_orders,
  agent:{online, last_seen, autonomy}, plan_usage:{plan, rfq:{used,limit}, assist:{used,limit}}}`.
  - `incoming_rfqs` = open RFQs this seller is matched to and has not yet quoted.
  - `review_queue` = quotes/actions awaiting the seller (e.g. buyer counters pending a seller reply).
  - `active_orders` = orders in `created|confirmed|fulfilled` for this seller.
  - `agent.online` = `agent_credentials.last_seen` within a freshness window AND credential valid
    (not revoked, not expired); `autonomy` from entitlements.
  - `plan_usage` from the entitlements usage counters + plan limits (`-1` = unlimited).
- `GET /v1/analytics/seller` → `{win_rate, quotes_submitted, quotes_won, revenue_won_cents,
  avg_response_hours, top_buyers:[{company_id,name,orders,revenue_cents}]}`.
  - `win_rate` = `quotes_won / decided`, where `decided` = quotes with status `accepted|rejected`
    (0 when `decided=0`, reported alongside the raw counts).
  - `revenue_won_cents` = Σ `orders.total_cents` for this seller where status ≠ `cancelled`.
  - `avg_response_hours` = avg(`quote.created_at − rfq.created_at`) over this seller's quotes.
  - `top_buyers` = orders grouped by `buyer_company_id` (status ≠ cancelled), Σ revenue + count, top 5.

## Track 4 — Frontend (`web/`)

- `web/lib/discovery.ts` (`searchDiscovery`, `getFacets`), `web/lib/dashboard.ts`
  (`getBuyerDashboard`, `getSellerDashboard`), `web/lib/analytics.ts` (`getSellerAnalytics`) — each with the
  local `req<T>`/`call<T>` pattern and vitest (mocked fetch).
- Public marketplace: `app/(marketplace)/page.tsx` (browse home + search box + facet sidebar + ranked cards
  with verified/rating badges) and `app/(marketplace)/s/[category]/page.tsx` (category landing). Public,
  no auth, SEO-friendly (server-rendered where practical); calls `/public/v1/discovery`.
- `/dashboard` split by role: real buyer dashboard (active RFQs, quotes-received w/ new badges, orders in
  progress, recent list) and real seller dashboard (operational tiles + agent status + plan usage).
- Seller **analytics** panel/section (win rate, revenue won, avg response time, top buyers).
- `/rfqs` gets tabs (open/quoted/closed) + a search box; new-quote badge on rows.
- Reuse existing CSS utilities (`.card/.stat/.badge/.tbl/.muted/.ok/.err`); verify via `tsc --noEmit` +
  bare `npm run build`.

## Testing / E2E

- **Unit (Go):** rank-score ordering (verified/high-rated/relevant beats the rest deterministically); facet
  counts; each aggregation query; win-rate + response-time + revenue math; public projection leaks no
  private columns; `rfq_views` badge logic; agent-online freshness window.
- **Web:** vitest for each lib client (incl. facet parsing + empty-state).
- **E2E #6** (`test/integration/e2e_06_discovery_dashboards_test.go`): seed a pilot supplier (verified,
  reviewed) + a matching buyer RFQ + an accepted order, then assert
  (a) `/public/v1/discovery?q=<need>` finds the supplier with the expected rank and non-empty facets and
      leaks no private columns,
  (b) `/v1/dashboard/buyer` shows real active-RFQ / quotes-received / orders-in-progress numbers,
  (c) `/v1/dashboard/seller` + `/v1/analytics/seller` render real win-rate / revenue / response-time /
      top-buyer aggregations.

## Plan breakdown (5 plans; subagent-driven; merged locally only)

1. **Search infra + discovery service** — migration 025 (pg_trgm, generated tsvector columns/indexes,
   `rfq_views`), FTS/trigram sqlc queries, weighted ranking with named constants, `GET /v1/discovery` +
   `GET /public/v1/discovery` + fail-open rate limit, unit tests (rank ordering, projection safety).
2. **Facets + catalog sub-signal + projection hardening** — `/public/v1/discovery/facets`, matched-catalog
   snippets folded into results, projection-leak test.
3. **Buyer dashboard + RFQ tabs/search + new-quote badges** — `rfq_views` upsert, `GET /v1/dashboard/buyer`,
   `GET /v1/rfqs?tab=&q=`, badge query, unit tests.
4. **Seller dashboard + full analytics** — `GET /v1/dashboard/seller`, `GET /v1/analytics/seller`, agent
   presence + plan usage, win-rate/revenue/response-time/top-buyers queries, unit tests.
5. **Frontend + E2E #6** — marketplace/category/search UI, real buyer & seller dashboards, analytics panel,
   `/rfqs` tabs; then the spanning E2E.

## Scope / deferred (documented, not built)

External search engine (Meilisearch/Typesense); personalized/ML ranking; saved searches & alerts; buyer-side
analytics; cross-tenant admin analytics (Sprint 8); real-time dashboard push via SSE (dashboards fetch on
load); geo/radius location search (string match only for now). Neither repo pushed to GitHub.
