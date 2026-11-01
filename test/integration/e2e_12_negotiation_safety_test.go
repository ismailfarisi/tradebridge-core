package integration

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/internal/invoicing"
	"github.com/tradebridge/core/internal/negotiation"
	"github.com/tradebridge/core/internal/quotes"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// newQuotesService builds the real quotes service the engine drives.
func newQuotesService(t *testing.T) *quotes.Service {
	t.Helper()
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	return quotes.NewService(q, pool, eventbus.New(q, nil, zerolog.Nop()), nil, zerolog.Nop(), nil,
		invoicing.NewService(zerolog.Nop()), entitlements.NewService(q, zerolog.Nop()), nil)
}

// newEngine builds an independent engine — its own in-process lock map, as a
// separate API replica would have.
func newEngine(t *testing.T, actor negotiation.QuoteActor) *negotiation.Engine {
	t.Helper()
	pool := poolOrSkip(t)
	return negotiation.NewEngine(sqlc.New(pool), pool, actor, nil, zerolog.Nop(), time.Hour)
}

// seedStrategy writes a mandate straight to the DB, without the handler's
// immediate tick, so the test controls who ticks first.
func seedStrategy(t *testing.T, rfqID, buyerCo string, autoAccept bool) {
	t.Helper()
	md := int32(14)
	_, err := sqlc.New(poolOrSkip(t)).UpsertNegotiationStrategy(context.Background(), sqlc.UpsertNegotiationStrategyParams{
		RfqID: rfqID, BuyerCompanyID: buyerCo, TargetPriceCents: 85000, MaxPriceCents: 100000,
		MaxDeliveryDays: &md, PriceWeight: 0.7, MaxRounds: 2, AutoAccept: autoAccept, Tone: "collaborative",
	})
	require.NoError(t, err)
}

func countRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, poolOrSkip(t).QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

func buyerMessages(t *testing.T, rfqID string) int {
	return countRows(t, `SELECT COUNT(*) FROM quote_messages m JOIN quotes q ON q.id = m.quote_id
		WHERE q.rfq_id = $1 AND m.author = 'buyer'`, rfqID)
}

func roundsWhere(t *testing.T, rfqID, cond string) int {
	return countRows(t, `SELECT COUNT(*) FROM negotiation_rounds nr JOIN negotiation_strategies s ON s.id = nr.strategy_id
		WHERE s.rfq_id = $1 AND `+cond, rfqID)
}

func quoteIDsBySeller(t *testing.T, rfqID string) map[string]string {
	t.Helper()
	rows, err := poolOrSkip(t).Query(context.Background(), `SELECT seller_company_id, id FROM quotes WHERE rfq_id = $1`, rfqID)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var co, id string
		require.NoError(t, rows.Scan(&co, &id))
		out[co] = id
	}
	return out
}

// hammer ticks the RFQ from every engine at once, many times over.
func hammer(t *testing.T, rfqID string, engines ...*negotiation.Engine) {
	t.Helper()
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, 64)
	for _, e := range engines {
		for g := 0; g < 6; g++ {
			wg.Add(1)
			go func(e *negotiation.Engine) {
				defer wg.Done()
				<-start
				for i := 0; i < 5; i++ {
					if err := e.Tick(context.Background(), rfqID); err != nil {
						errs <- err
						return
					}
				}
			}(e)
		}
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

// TestE2E12_TwoEnginesSameRFQ runs two engines (two "replicas") concurrently
// on the same RFQ through a full auto-accept negotiation: every seller
// version gets exactly one counter, and exactly one order is placed.
func TestE2E12_TwoEnginesSameRFQ(t *testing.T) {
	api := buildAPI(t)
	_, coA, secretA := registerSeller(t, api, "SafePackA")
	_, coB, secretB := registerSeller(t, api, "SafePackB")
	_, buyerCo, rfqID := negotiationBuyerRFQ(t, "SafeBuyer")
	ensureMatched(t, rfqID, coA)
	ensureMatched(t, rfqID, coB)
	submitQuote(t, secretA, "safe-a-submit-"+rfqID, rfqID, 1000)
	submitQuote(t, secretB, "safe-b-submit-"+rfqID, rfqID, 1100)
	makePro(t, buyerCo)
	seedStrategy(t, rfqID, buyerCo, true)

	qs := newQuotesService(t)
	e1, e2 := newEngine(t, qs), newEngine(t, qs)

	hammer(t, rfqID, e1, e2)
	require.Equal(t, 2, roundsWhere(t, rfqID, "nr.action = 'counter'"), "one counter per seller version")
	require.Equal(t, 2, buyerMessages(t, rfqID), "exactly one counter message per seller version")
	require.Equal(t, 0, countRows(t, `SELECT COUNT(*) FROM (SELECT quote_id, responded_to_version FROM negotiation_rounds nr
		JOIN negotiation_strategies s ON s.id = nr.strategy_id WHERE s.rfq_id = $1
		GROUP BY 1, 2 HAVING COUNT(*) > 1) d`, rfqID))
	require.Equal(t, 0, roundsWhere(t, rfqID, "nr.delivery_status = 'pending'"))

	ids := quoteIDsBySeller(t, rfqID)
	counterQuote(t, secretA, "safe-a-c1-"+rfqID, ids[coA], 920)
	counterQuote(t, secretB, "safe-b-c1-"+rfqID, ids[coB], 1100)

	hammer(t, rfqID, e1, e2)
	require.Equal(t, 1, countRows(t, `SELECT COUNT(*) FROM orders WHERE rfq_id = $1`, rfqID), "at most one accepted order")
	require.Equal(t, 1, countRows(t, `SELECT COUNT(*) FROM quotes WHERE rfq_id = $1 AND status = 'accepted'`, rfqID))
	require.Equal(t, 4, roundsWhere(t, rfqID, "TRUE"))
	require.Equal(t, 2, buyerMessages(t, rfqID), "no counters after the close")
	var status string
	var outcome *string
	require.NoError(t, poolOrSkip(t).QueryRow(context.Background(),
		`SELECT status, outcome_quote_id FROM negotiation_strategies WHERE rfq_id = $1`, rfqID).Scan(&status, &outcome))
	require.Equal(t, "completed", status)
	require.NotNil(t, outcome)
	require.Equal(t, ids[coA], *outcome)
}

// TestE2E12_AdvisoryLockExcludesOtherInstances: while another session holds
// the RFQ's negotiation lock, a tick on this instance is a no-op.
func TestE2E12_AdvisoryLockExcludesOtherInstances(t *testing.T) {
	api := buildAPI(t)
	_, co, secret := registerSeller(t, api, "SafeLockPack")
	_, buyerCo, rfqID := negotiationBuyerRFQ(t, "SafeLockBuyer")
	ensureMatched(t, rfqID, co)
	submitQuote(t, secret, "safe-lock-"+rfqID, rfqID, 1000)
	seedStrategy(t, rfqID, buyerCo, false)
	ctx := context.Background()

	tx, err := poolOrSkip(t).Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('negotiation:' || $1::text, 0))`, rfqID)
	require.NoError(t, err)

	e := newEngine(t, newQuotesService(t))
	require.NoError(t, e.Tick(ctx, rfqID))
	require.Equal(t, 0, roundsWhere(t, rfqID, "TRUE"), "tick must skip an RFQ locked by another instance")

	require.NoError(t, tx.Rollback(ctx))
	require.NoError(t, e.Tick(ctx, rfqID))
	require.Equal(t, 1, roundsWhere(t, rfqID, "nr.action = 'counter'"))
}

// crashAfterCommit dies (runtime.Goexit, so deferred lock releases run like a
// dropped connection would) right after the move transaction commits, before
// any live side effect.
type crashAfterCommit struct{ *quotes.Service }

func (crashAfterCommit) DeliverCounter(context.Context, quotes.CounterDelivery) { runtime.Goexit() }

type countingActor struct {
	*quotes.Service
	delivered atomic.Int32
}

func (c *countingActor) DeliverCounter(ctx context.Context, d quotes.CounterDelivery) {
	c.delivered.Add(1)
	c.Service.DeliverCounter(ctx, d)
}

type failingRecord struct{ *quotes.Service }

func (f failingRecord) RecordCounterTx(ctx context.Context, qtx *sqlc.Queries, plan quotes.CounterPlan, p *int64, d, t *int32, msg string) (quotes.CounterDelivery, error) {
	// Write the message, then fail: the round and the message must roll back together.
	if _, err := f.Service.RecordCounterTx(ctx, qtx, plan, p, d, t, msg); err != nil {
		return quotes.CounterDelivery{}, err
	}
	return quotes.CounterDelivery{}, errors.New("injected failure")
}

// TestE2E12_CounterDeliveryIsCrashSafe: a failed counter write records
// nothing; a crash after commit leaves a pending round that the next tick
// (on any instance) delivers exactly once, without a second counter.
func TestE2E12_CounterDeliveryIsCrashSafe(t *testing.T) {
	api := buildAPI(t)
	_, co, secret := registerSeller(t, api, "SafeCrashPack")
	_, buyerCo, rfqID := negotiationBuyerRFQ(t, "SafeCrashBuyer")
	ensureMatched(t, rfqID, co)
	submitQuote(t, secret, "safe-crash-"+rfqID, rfqID, 1000)
	seedStrategy(t, rfqID, buyerCo, false)
	ctx := context.Background()
	qs := newQuotesService(t)

	// (a) The counter write fails inside the move transaction: no round, no message.
	require.NoError(t, newEngine(t, failingRecord{qs}).Tick(ctx, rfqID))
	require.Equal(t, 0, roundsWhere(t, rfqID, "TRUE"))
	require.Equal(t, 0, buyerMessages(t, rfqID))

	// (b) Crash between commit and delivery.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = newEngine(t, crashAfterCommit{qs}).Tick(ctx, rfqID)
	}()
	<-done
	require.Equal(t, 1, roundsWhere(t, rfqID, "nr.delivery_status = 'pending' AND nr.quote_message_id IS NOT NULL"))
	require.Equal(t, 1, buyerMessages(t, rfqID), "the counter is durable in the seller's thread")

	// Another instance replays the delivery once and never re-counters.
	survivor := &countingActor{Service: qs}
	e := newEngine(t, survivor)
	require.NoError(t, e.Tick(ctx, rfqID))
	require.NoError(t, e.Tick(ctx, rfqID))
	require.EqualValues(t, 1, survivor.delivered.Load())
	require.Equal(t, 1, roundsWhere(t, rfqID, "nr.delivery_status = 'sent'"))
	require.Equal(t, 1, roundsWhere(t, rfqID, "TRUE"))
	require.Equal(t, 1, buyerMessages(t, rfqID))
}

// TestE2E12_CompanyKillSwitch: an owner pauses all of the company's
// negotiations; the engine stops countering and the API says why; members
// cannot flip it; resuming lets the engine continue.
func TestE2E12_CompanyKillSwitch(t *testing.T) {
	api := buildAPI(t)
	_, co, secret := registerSeller(t, api, "KillCoPack")
	ownerToken, buyerCo, rfqID := negotiationBuyerRFQ(t, "KillCoBuyer")
	ensureMatched(t, rfqID, co)
	submitQuote(t, secret, "kill-co-"+rfqID, rfqID, 1000)

	st, ctrl := doJSON(t, api, "GET", "/v1/negotiations/controls", ownerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, false, ctrl["paused"])

	st, _ = doJSON(t, api, "PUT", "/v1/negotiations/controls", ownerToken, map[string]any{"reason": "x"})
	require.Equal(t, http.StatusBadRequest, st, "paused is required")

	st, ctrl = doJSON(t, api, "PUT", "/v1/negotiations/controls", ownerToken, map[string]any{"paused": true, "reason": "Budget review"})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, true, ctrl["paused"])
	require.Equal(t, "company", ctrl["paused_by"])
	require.Contains(t, ctrl["message"], "paused by your company: Budget review")

	// Members can read but not flip the switch.
	st, inv := doJSON(t, api, "POST", "/v1/users/invite", ownerToken, map[string]any{"email": uniqueEmail("kill-m-"), "role": "member"})
	require.Equal(t, http.StatusCreated, st)
	st, acc := doJSON(t, api, "POST", "/v1/auth/accept-invite", "", map[string]any{"token": inv["token"], "password": "memberpass"})
	require.Equal(t, http.StatusCreated, st)
	memberToken := acc["token"].(string)
	st, _ = doJSON(t, api, "PUT", "/v1/negotiations/controls", memberToken, map[string]any{"paused": false})
	require.Equal(t, http.StatusForbidden, st)
	st, _ = doJSON(t, api, "GET", "/v1/negotiations/controls", memberToken, nil)
	require.Equal(t, http.StatusOK, st)

	st, view := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", ownerToken, map[string]any{
		"target_price_cents": 85000, "max_price_cents": 100000,
	})
	require.Equal(t, http.StatusOK, st)
	require.Empty(t, view["rounds"], "no counters while paused")
	require.Equal(t, "active", view["strategy"].(map[string]any)["status"])
	require.Equal(t, "company", view["pause"].(map[string]any)["paused_by"])

	st, view = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/run", ownerToken, map[string]any{})
	require.Equal(t, http.StatusOK, st)
	require.Empty(t, view["rounds"])

	st, list := doJSON(t, api, "GET", "/v1/negotiations", ownerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, true, list["pause"].(map[string]any)["paused"])

	require.Equal(t, 1, countRows(t, `SELECT COUNT(*) FROM audit_logs WHERE company_id = $1 AND action = 'negotiation.company_pause'`, buyerCo))
	require.Equal(t, 1, countRows(t, `SELECT COUNT(*) FROM negotiation_kill_switch_log WHERE company_id = $1 AND paused`, buyerCo))

	st, ctrl = doJSON(t, api, "PUT", "/v1/negotiations/controls", ownerToken, map[string]any{"paused": false})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, false, ctrl["paused"])
	st, view = doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/negotiation", ownerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Len(t, view["rounds"], 1, "resuming lets the engine counter")
	require.Equal(t, false, view["pause"].(map[string]any)["paused"])
}

// TestE2E12_PlatformKillSwitch: a platform admin pauses every negotiation;
// the engine neither counters nor closes and approvals are refused until the
// switch is released. The flip is admin-only and logged.
func TestE2E12_PlatformKillSwitch(t *testing.T) {
	const email, pass = "kill-admin@test.local", "admin-supersecret"
	api := buildAPIWithAdmin(t, email, pass)
	st, login := doJSON(t, api, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": pass})
	require.Equal(t, http.StatusOK, st)
	adminTok := login["token"].(string)
	t.Cleanup(func() {
		_, _ = poolOrSkip(t).Exec(context.Background(), `UPDATE negotiation_platform_control SET paused = FALSE, reason = ''`)
	})

	_, co, secret := registerSeller(t, api, "KillPlatPack")
	buyerToken, _, rfqID := negotiationBuyerRFQ(t, "KillPlatBuyer")
	ensureMatched(t, rfqID, co)
	submitQuote(t, secret, "kill-plat-"+rfqID, rfqID, 800)

	// A deal at target is held for approval before the switch is engaged.
	st, view := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, map[string]any{
		"target_price_cents": 85000, "max_price_cents": 95000,
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "awaiting_approval", view["strategy"].(map[string]any)["status"])

	st, _ = doJSON(t, api, "PUT", "/admin/v1/negotiations/kill-switch", buyerToken, map[string]any{"paused": true, "reason": "x"})
	require.Equal(t, http.StatusUnauthorized, st, "company tokens cannot reach the admin realm")
	st, _ = doJSON(t, api, "PUT", "/admin/v1/negotiations/kill-switch", adminTok, map[string]any{"paused": true})
	require.Equal(t, http.StatusBadRequest, st, "pausing the platform needs a reason")
	st, body := doJSON(t, api, "PUT", "/admin/v1/negotiations/kill-switch", adminTok, map[string]any{"paused": true, "reason": "Pricing incident"})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, true, body["control"].(map[string]any)["paused"])
	require.Equal(t, "admin:"+email, body["control"].(map[string]any)["updated_by"])
	require.NotEmpty(t, body["history"])

	st, _ = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{})
	require.Equal(t, http.StatusConflict, st, "no agent accept while the platform is paused")
	require.Equal(t, 0, countRows(t, `SELECT COUNT(*) FROM orders WHERE rfq_id = $1`, rfqID))
	st, view = doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	p := view["pause"].(map[string]any)
	require.Equal(t, "platform", p["paused_by"])
	require.Contains(t, p["message"], "paused by TradeBridge: Pricing incident")

	// A fresh negotiation makes no move while paused.
	_, co2, secret2 := registerSeller(t, api, "KillPlatPack2")
	buyer3Token, _, rfq3 := negotiationBuyerRFQ(t, "KillPlatBuyer3")
	ensureMatched(t, rfq3, co2)
	submitQuote(t, secret2, "kill-plat3-"+rfq3, rfq3, 1000)
	st, view = doJSON(t, api, "PUT", "/v1/rfqs/"+rfq3+"/negotiation", buyer3Token, map[string]any{
		"target_price_cents": 85000, "max_price_cents": 100000,
	})
	require.Equal(t, http.StatusOK, st)
	require.Empty(t, view["rounds"])

	st, body = doJSON(t, api, "PUT", "/admin/v1/negotiations/kill-switch", adminTok, map[string]any{"paused": false})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, false, body["control"].(map[string]any)["paused"])
	st, body = doJSON(t, api, "GET", "/admin/v1/negotiations/kill-switch", adminTok, nil)
	require.Equal(t, http.StatusOK, st)
	require.GreaterOrEqual(t, len(body["history"].([]any)), 2)

	st, order := doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{})
	require.Equal(t, http.StatusCreated, st)
	require.EqualValues(t, 80000, order["total_cents"])
	st, view = doJSON(t, api, "POST", "/v1/rfqs/"+rfq3+"/negotiation/run", buyer3Token, map[string]any{})
	require.Equal(t, http.StatusOK, st)
	require.Len(t, view["rounds"], 1)
}
