# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

- **Primary: SMB owners and operators in the UAE/GCC** buying for their own business: packaging, supplies, raw materials, equipment. They have no procurement team and little time to collect quotes or haggle, so most purchases go unnegotiated. They want a good price and reliable delivery without managing the process themselves.
- **Suppliers (sellers):** UAE/GCC (initially Dubai-based) businesses that respond to RFQs manually or through an automated seller agent connected to the Agent Gateway. They manage a public storefront, catalog, quotes, orders and invoices.
- **Platform admins:** TradeBridge staff handling verification, disputes, reports, waitlist, analytics and audit. Support can view as a company in read-only mode.

## Product Purpose

TradeBridge lets a small business describe what it needs in plain language, collects competing quotes from verified suppliers, and lets an AI agent negotiate on the buyer's behalf within limits the buyer sets, ending in a purchase order. Success means the owner spends minutes per purchase instead of days, pays measurably less than the opening quotes, and trusts every deal the agent made.

## Positioning

Two layers in one product:
1. **A neutral B2B marketplace:** RFQs, normalized quotes, orders, invoicing, reviews and disputes, open to human sellers and to seller agents over an authenticated protocol (Agent Gateway).
2. **A buyer-side negotiation agent on top:** it counters every quote under a server-enforced mandate (target price, walk-away price, max delivery days, price-vs-delivery weight, round budget, tone, auto-accept or approval). Every round is recorded with its reason, and savings are measured against the opening quote.

Enterprise negotiation tools (Pactum-style) sell to large procurement teams. TradeBridge brings agent negotiation to SMB purchases and owns the marketplace where the counterparties are.

## Operating Context

- Core flow: chat or form RFQ intake → suppliers and seller agents quote → negotiation agent counters → buyer approves (or auto-accepts) → order / PO → invoice → review or dispute.
- The buyer is typically the owner, often deciding alone and quickly; approvals are a single person's yes/no, not a multi-step workflow.
- Notifications (in-app and email) pull buyers back when quotes arrive or a deal is awaiting approval.
- Seller agents run continuously and answer counters without a human, so negotiation rounds can resolve in minutes.

## Capabilities and Constraints

- Web app (Next.js) on a Go/Fiber Core API and Agent Gateway; Postgres and Redis.
- Roles: buyer company, seller company, platform admin. All data is tenant-isolated.
- Plans: Free, Pro and Business (Stripe). Auto-accept and agent autonomy require a paid plan (`AgentAutonomy` entitlement).
- Usage limits are enforced per plan; idempotency keys guard commands.
- Terminology: RFQ, quote (versioned), counter-offer, mandate, negotiation round, walk-away price, order / PO, savings.
- Market: UAE/GCC first, so AED should be the expected currency and Dubai the default supplier location.
- **Open decisions / known gaps:**
  - Currency defaults to USD in the schema (`currency DEFAULT 'USD'`), and the demo seed and landing copy use USD; AED-first is not implemented yet.
  - Arabic / RTL support is undecided; the app is English-only (`lang="en"`).
  - Landing and marketing copy still speaks to procurement teams ("long tail of spend") rather than SMB owners.

## Evidence on Hand

- A working end-to-end demo: `make demo-seed` (buyer `demo@northwind.test`) plus `make simseller` live seller agents, producing real negotiation rounds and savings figures. These are demo data, not customer results.
- No real customers, testimonials, logos, case studies, press or production savings metrics exist yet. Do not fabricate any of them; illustrative examples must be labeled as examples.

## Product Principles

1. **The owner stays in control.** The agent acts only within the mandate. Limits are enforced on the server, and anything outside them comes back for approval.
2. **Every decision is explainable.** Each counter, acceptance or walk-away shows its reason and its effect on price, delivery and savings.
3. **Minutes, not days.** The default path from need to PO should take a busy owner a few minutes of attention.
4. **Neutral marketplace, fair to suppliers.** Sellers, human or agent, get clear terms and a consistent protocol. The buyer agent negotiates hard but honestly.
5. **Show the money.** Savings against the opening quote are visible on every deal and in aggregate.
