package negotiation

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/internal/quotes"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the buyer negotiation API.
type Handler struct {
	q   *sqlc.Queries
	eng *Engine
	ent *entitlements.Service
}

func NewHandler(q *sqlc.Queries, eng *Engine, ent *entitlements.Service) *Handler {
	return &Handler{q: q, eng: eng, ent: ent}
}

// Register mounts the routes behind the tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Add("GET", "/v1/negotiations", append(guard, h.list)...)
	app.Add("GET", "/v1/negotiations/savings", append(guard, h.savings)...)
	app.Add("GET", "/v1/rfqs/:id/negotiation", append(guard, h.get)...)
	app.Add("PUT", "/v1/rfqs/:id/negotiation", append(guard, h.upsert)...)
	app.Add("POST", "/v1/rfqs/:id/negotiation/pause", append(guard, h.pause)...)
	app.Add("POST", "/v1/rfqs/:id/negotiation/resume", append(guard, h.resume)...)
	app.Add("POST", "/v1/rfqs/:id/negotiation/run", append(guard, h.run)...)
	app.Add("POST", "/v1/rfqs/:id/negotiation/approve", append(guard, h.approve)...)
	app.Add("POST", "/v1/rfqs/:id/negotiation/decline", append(guard, h.decline)...)
}

type strategyInput struct {
	TargetPriceCents int64    `json:"target_price_cents"`
	MaxPriceCents    int64    `json:"max_price_cents"`
	MaxDeliveryDays  *int32   `json:"max_delivery_days"`
	PriceWeight      *float64 `json:"price_weight"`
	MaxRounds        *int32   `json:"max_rounds"`
	AutoAccept       bool     `json:"auto_accept"`
	Tone             string   `json:"tone"`
}

func (in *strategyInput) validate() error {
	if in.TargetPriceCents <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "target_price_cents must be positive")
	}
	if in.MaxPriceCents < in.TargetPriceCents {
		return fiber.NewError(fiber.StatusBadRequest, "max_price_cents must be at least the target price")
	}
	if in.MaxDeliveryDays != nil && *in.MaxDeliveryDays <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "max_delivery_days must be positive")
	}
	if in.PriceWeight == nil {
		w := 0.7
		in.PriceWeight = &w
	} else if *in.PriceWeight < 0 || *in.PriceWeight > 1 {
		return fiber.NewError(fiber.StatusBadRequest, "price_weight must be between 0 and 1")
	}
	if in.MaxRounds == nil {
		r := int32(4)
		in.MaxRounds = &r
	} else if *in.MaxRounds < 1 || *in.MaxRounds > 10 {
		return fiber.NewError(fiber.StatusBadRequest, "max_rounds must be between 1 and 10")
	}
	switch in.Tone {
	case "":
		in.Tone = "collaborative"
	case "collaborative", "firm":
	default:
		return fiber.NewError(fiber.StatusBadRequest, "tone must be collaborative or firm")
	}
	return nil
}

func (h *Handler) ownedRFQ(c *fiber.Ctx) (sqlc.Rfq, error) {
	r, err := h.q.GetRFQ(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && r.BuyerCompanyID != middleware.CompanyID(c)) {
		return r, fiber.NewError(fiber.StatusNotFound, "rfq not found")
	}
	return r, err
}

func (h *Handler) upsert(c *fiber.Ctx) error {
	r, err := h.ownedRFQ(c)
	if err != nil {
		return err
	}
	if r.Status != rfq.StatusOpen && r.Status != rfq.StatusQuoted {
		return fiber.NewError(fiber.StatusConflict, "negotiation can only be configured on an open RFQ")
	}
	var in strategyInput
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if err := in.validate(); err != nil {
		return err
	}
	if in.AutoAccept && h.ent != nil && !h.ent.For(c.Context(), r.BuyerCompanyID).AgentAutonomy {
		return fiber.NewError(fiber.StatusPaymentRequired, "auto-accept requires a plan with agent autonomy")
	}
	var createdBy *string
	if uid := middleware.UserID(c); uid != "" {
		createdBy = &uid
	}
	if _, err := h.q.UpsertNegotiationStrategy(c.Context(), sqlc.UpsertNegotiationStrategyParams{
		RfqID: r.ID, BuyerCompanyID: r.BuyerCompanyID, CreatedBy: createdBy,
		TargetPriceCents: in.TargetPriceCents, MaxPriceCents: in.MaxPriceCents, MaxDeliveryDays: in.MaxDeliveryDays,
		PriceWeight: *in.PriceWeight, MaxRounds: *in.MaxRounds, AutoAccept: in.AutoAccept, Tone: in.Tone,
	}); err != nil {
		return err
	}
	if err := h.eng.Tick(c.Context(), r.ID); err != nil {
		return err
	}
	return h.respond(c, r)
}

func (h *Handler) get(c *fiber.Ctx) error {
	r, err := h.ownedRFQ(c)
	if err != nil {
		return err
	}
	return h.respond(c, r)
}

func (h *Handler) run(c *fiber.Ctx) error {
	r, err := h.ownedRFQ(c)
	if err != nil {
		return err
	}
	if err := h.eng.Tick(c.Context(), r.ID); err != nil {
		return err
	}
	return h.respond(c, r)
}

func (h *Handler) pause(c *fiber.Ctx) error  { return h.setPaused(c, true) }
func (h *Handler) resume(c *fiber.Ctx) error { return h.setPaused(c, false) }

func (h *Handler) setPaused(c *fiber.Ctx, paused bool) error {
	r, err := h.ownedRFQ(c)
	if err != nil {
		return err
	}
	if _, err := h.eng.SetPaused(c.Context(), r.BuyerCompanyID, r.ID, paused); err != nil {
		return mapErr(err)
	}
	if !paused {
		if err := h.eng.Tick(c.Context(), r.ID); err != nil {
			return err
		}
	}
	return h.respond(c, r)
}

func (h *Handler) approve(c *fiber.Ctx) error {
	r, err := h.ownedRFQ(c)
	if err != nil {
		return err
	}
	var in struct {
		QuoteID string `json:"quote_id"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&in); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid body")
		}
	}
	order, err := h.eng.Approve(c.Context(), r.BuyerCompanyID, r.ID, strings.TrimSpace(in.QuoteID))
	if err != nil {
		return mapErr(err)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"order_id": order.ID, "total_cents": order.TotalCents, "currency": order.Currency})
}

func (h *Handler) decline(c *fiber.Ctx) error {
	r, err := h.ownedRFQ(c)
	if err != nil {
		return err
	}
	if _, err := h.eng.Decline(c.Context(), r.BuyerCompanyID, r.ID); err != nil {
		return mapErr(err)
	}
	return h.respond(c, r)
}

func mapErr(err error) error {
	var conflict ConflictError
	var qconflict quotes.ConflictError
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, quotes.ErrNotFound):
		return fiber.NewError(fiber.StatusNotFound, "negotiation not found")
	case errors.As(err, &conflict):
		return fiber.NewError(fiber.StatusConflict, conflict.Msg)
	case errors.As(err, &qconflict):
		return fiber.NewError(fiber.StatusConflict, qconflict.Msg)
	}
	return err
}

type supplierView struct {
	QuoteID             string  `json:"quote_id"`
	SellerCompanyID     string  `json:"seller_company_id"`
	SellerName          string  `json:"seller_name"`
	VerificationStatus  string  `json:"verification_status"`
	SellerAvgRating     float64 `json:"seller_avg_rating"`
	Currency            string  `json:"currency"`
	InitialPriceCents   int64   `json:"initial_price_cents"`
	InitialDeliveryDays *int32  `json:"initial_delivery_days"`
	PriceCents          int64   `json:"price_cents"`
	DeliveryDays        *int32  `json:"delivery_days"`
	Version             int32   `json:"version"`
	Status              string  `json:"status"`
	Counters            int     `json:"counters"`
	LastAction          string  `json:"last_action"`
	Utility             float64 `json:"utility"`
	Recommended         bool    `json:"recommended"`
}

type summaryView struct {
	Currency         string  `json:"currency"`
	InitialBestCents int64   `json:"initial_best_cents"`
	CurrentBestCents int64   `json:"current_best_cents"`
	SavingsCents     int64   `json:"savings_cents"`
	SavingsPct       float64 `json:"savings_pct"`
	QuoteCount       int     `json:"quote_count"`
	CounterCount     int     `json:"counter_count"`
}

type suggestionView struct {
	TargetPriceCents int64 `json:"target_price_cents"`
	MaxPriceCents    int64 `json:"max_price_cents"`
}

type negotiationView struct {
	RFQ       fiber.Map                 `json:"rfq"`
	Strategy  *sqlc.NegotiationStrategy `json:"strategy"`
	Rounds    []roundView               `json:"rounds"`
	Suppliers []supplierView            `json:"suppliers"`
	Summary   summaryView               `json:"summary"`
	Suggested *suggestionView           `json:"suggested"`
}

type roundView struct {
	sqlc.NegotiationRound
	SellerName string `json:"seller_name"`
}

func (h *Handler) respond(c *fiber.Ctx, r sqlc.Rfq) error {
	ctx := c.Context()
	// Re-read: a tick may have closed the RFQ.
	if fresh, err := h.q.GetRFQ(ctx, r.ID); err == nil {
		r = fresh
	}
	v := negotiationView{
		RFQ:       fiber.Map{"id": r.ID, "title": r.Title, "status": r.Status, "category": r.Category, "created_at": r.CreatedAt},
		Rounds:    []roundView{},
		Suppliers: []supplierView{},
	}
	qs, err := h.q.RankQuotesByRFQ(ctx, r.ID)
	if err != nil {
		return err
	}
	first, err := h.q.ListQuoteFirstVersions(ctx, r.ID)
	if err != nil {
		return err
	}
	firstBy := map[string]sqlc.ListQuoteFirstVersionsRow{}
	for _, f := range first {
		firstBy[f.QuoteID] = f
	}
	names := map[string]string{}
	for _, q := range qs {
		names[q.ID] = q.SellerName
	}

	s, err := h.q.GetNegotiationStrategyByRFQ(ctx, r.ID)
	var rounds []sqlc.NegotiationRound
	if err == nil {
		v.Strategy = &s
		if rounds, err = h.q.ListNegotiationRounds(ctx, s.ID); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	counters := map[string]int{}
	lastAction := map[string]string{}
	for _, rd := range rounds {
		v.Rounds = append(v.Rounds, roundView{NegotiationRound: rd, SellerName: names[rd.QuoteID]})
		if rd.Action == ActionCounter {
			counters[rd.QuoteID]++
			v.Summary.CounterCount++
		}
		lastAction[rd.QuoteID] = rd.Action
	}

	var m Mandate
	if v.Strategy != nil {
		m = mandateOf(s)
	}
	var recommendedCents int64
	for _, q := range qs {
		sv := supplierView{
			QuoteID: q.ID, SellerCompanyID: q.SellerCompanyID, SellerName: q.SellerName,
			VerificationStatus: q.VerificationStatus, SellerAvgRating: q.SellerAvgRating, Currency: q.Currency,
			InitialPriceCents: q.PriceCents, InitialDeliveryDays: q.DeliveryDays,
			PriceCents: q.PriceCents, DeliveryDays: q.DeliveryDays, Version: q.Version, Status: q.Status,
			Counters: counters[q.ID], LastAction: lastAction[q.ID],
		}
		if f, ok := firstBy[q.ID]; ok {
			sv.InitialPriceCents, sv.InitialDeliveryDays = f.PriceCents, f.DeliveryDays
		}
		if v.Strategy != nil {
			sv.Utility = Utility(m, Offer{PriceCents: q.PriceCents, DeliveryDays: q.DeliveryDays})
			sv.Recommended = s.OutcomeQuoteID != nil && *s.OutcomeQuoteID == q.ID
			if sv.Recommended {
				recommendedCents = q.PriceCents
			}
		}
		if v.Summary.Currency == "" {
			v.Summary.Currency = q.Currency
		}
		if v.Summary.InitialBestCents == 0 || sv.InitialPriceCents < v.Summary.InitialBestCents {
			v.Summary.InitialBestCents = sv.InitialPriceCents
		}
		if q.Status != "rejected" && (v.Summary.CurrentBestCents == 0 || q.PriceCents < v.Summary.CurrentBestCents) {
			v.Summary.CurrentBestCents = q.PriceCents
		}
		v.Suppliers = append(v.Suppliers, sv)
	}
	// Once the agent has picked a deal, report that deal rather than the cheapest quote.
	if recommendedCents > 0 {
		v.Summary.CurrentBestCents = recommendedCents
	}
	v.Summary.QuoteCount = len(qs)
	if v.Summary.InitialBestCents > v.Summary.CurrentBestCents && v.Summary.CurrentBestCents > 0 {
		v.Summary.SavingsCents = v.Summary.InitialBestCents - v.Summary.CurrentBestCents
		v.Summary.SavingsPct = round3(float64(v.Summary.SavingsCents) / float64(v.Summary.InitialBestCents) * 100)
	}
	if v.Strategy == nil && v.Summary.InitialBestCents > 0 {
		// Anchor 12% under the best opening quote; walk away above it.
		best := v.Summary.CurrentBestCents
		v.Suggested = &suggestionView{TargetPriceCents: roundDown(best * 88 / 100), MaxPriceCents: best}
	}
	return c.JSON(v)
}

type listItem struct {
	sqlc.ListNegotiationsByBuyerRow
	SavingsCents int64 `json:"savings_cents"`
}

func (h *Handler) list(c *fiber.Ctx) error {
	rows, err := h.q.ListNegotiationsByBuyer(c.Context(), sqlc.ListNegotiationsByBuyerParams{BuyerCompanyID: middleware.CompanyID(c), Lim: 200})
	if err != nil {
		return err
	}
	out := make([]listItem, 0, len(rows))
	for _, r := range rows {
		it := listItem{ListNegotiationsByBuyerRow: r}
		if r.InitialBestCents > r.CurrentBestCents && r.CurrentBestCents > 0 {
			it.SavingsCents = r.InitialBestCents - r.CurrentBestCents
		}
		out = append(out, it)
	}
	return c.JSON(fiber.Map{"negotiations": out})
}

func (h *Handler) savings(c *fiber.Ctx) error {
	ctx := c.Context()
	me := middleware.CompanyID(c)
	totals, err := h.q.BuyerSavingsTotals(ctx, me)
	if err != nil {
		return err
	}
	counts, err := h.q.BuyerNegotiationCounts(ctx, me)
	if err != nil {
		return err
	}
	monthly, err := h.q.BuyerMonthlySpend(ctx, me)
	if err != nil {
		return err
	}
	top, err := h.q.BuyerTopSuppliers(ctx, me)
	if err != nil {
		return err
	}
	recent, err := h.q.BuyerRecentDeals(ctx, me)
	if err != nil {
		return err
	}
	pct := 0.0
	if totals.BaselineCents > 0 {
		pct = round3(float64(totals.SavingsCents) / float64(totals.BaselineCents) * 100)
	}
	return c.JSON(fiber.Map{
		"totals":        totals,
		"savings_pct":   pct,
		"negotiations":  counts,
		"monthly":       fillMonths(monthly, time.Now()),
		"top_suppliers": top,
		"recent_deals":  recent,
	})
}

// fillMonths returns the trailing 12 months with zero rows for gaps so charts
// render a continuous axis.
func fillMonths(rows []sqlc.BuyerMonthlySpendRow, now time.Time) []sqlc.BuyerMonthlySpendRow {
	by := map[string]sqlc.BuyerMonthlySpendRow{}
	for _, r := range rows {
		by[r.Month] = r
	}
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -11, 0)
	out := make([]sqlc.BuyerMonthlySpendRow, 0, 12)
	for i := 0; i < 12; i++ {
		k := start.AddDate(0, i, 0).Format("2006-01")
		r, ok := by[k]
		if !ok {
			r = sqlc.BuyerMonthlySpendRow{Month: k}
		}
		out = append(out, r)
	}
	return out
}

// roundDown trims a suggested price to a figure a person would type:
// whole AED 100 above AED 10,000, whole AED 10 above AED 1,000, else whole AED.
func roundDown(cents int64) int64 {
	step := int64(100)
	switch {
	case cents >= 1_000_000:
		step = 10_000
	case cents >= 100_000:
		step = 1_000
	}
	if r := cents - cents%step; r > 0 {
		return r
	}
	return cents
}
