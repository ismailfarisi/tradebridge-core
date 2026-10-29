// Package simseller is a deterministic seller agent for local development and
// demos. It speaks the real Agent Gateway protocol (SSE stream + commands):
// it quotes every rfq.matched with a persona-specific markup and answers
// buyer counters (message.received) by conceding toward a private floor, then
// holding — exactly the behaviour the buyer negotiation engine is built for.
// It is NOT used in production.
package simseller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Persona shapes a simulated seller's commercial behaviour.
type Persona struct {
	Name         string
	Markup       float64 // opening price multiplier over the fair base price
	Floor        float64 // lowest acceptable price as a fraction of opening
	Concede      float64 // fraction of the gap to the buyer's offer conceded per round
	DeliveryDays int32
	FlexDelivery bool // will match the buyer's requested delivery
}

// Personas are the built-in seller archetypes.
var Personas = []Persona{
	{Name: "anchor-high", Markup: 1.38, Floor: 0.74, Concede: 0.45, DeliveryDays: 10, FlexDelivery: true},
	{Name: "steady", Markup: 1.22, Floor: 0.84, Concede: 0.35, DeliveryDays: 7, FlexDelivery: false},
	{Name: "eager", Markup: 1.30, Floor: 0.80, Concede: 0.6, DeliveryDays: 18, FlexDelivery: true},
}

// Agent is one simulated seller connected to the gateway.
type Agent struct {
	GatewayURL string
	Credential string
	Persona    Persona
	Latency    time.Duration
	Logger     zerolog.Logger
	HTTP       *http.Client

	mu    sync.Mutex
	deals map[string]*deal // rfqID -> deal
	last  string           // Last-Event-ID
}

type deal struct {
	Opening  int64
	Current  int64
	Delivery int32
	Counters int
}

func (a *Agent) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return http.DefaultClient
}

// Run streams events until ctx is cancelled, reconnecting on errors.
func (a *Agent) Run(ctx context.Context) {
	a.mu.Lock()
	if a.deals == nil {
		a.deals = map[string]*deal{}
	}
	a.mu.Unlock()
	for ctx.Err() == nil {
		if err := a.stream(ctx); err != nil && ctx.Err() == nil {
			a.Logger.Warn().Err(err).Str("persona", a.Persona.Name).Msg("simseller: stream ended, reconnecting")
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}
}

type sseEvent struct {
	ID, Type string
	Data     []byte
}

func (a *Agent) stream(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.GatewayURL+"/agent/v1/stream", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Credential)
	req.Header.Set("Accept", "text/event-stream")
	if a.last != "" {
		req.Header.Set("Last-Event-ID", a.last)
	}
	resp, err := (&http.Client{Transport: a.client().Transport}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("stream status %d: %s", resp.StatusCode, b)
	}
	a.Logger.Info().Str("persona", a.Persona.Name).Msg("simseller: connected")
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var ev sseEvent
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if ev.Type != "" {
				a.handle(ctx, ev)
			}
			ev = sseEvent{}
		case strings.HasPrefix(line, "id: "):
			ev.ID = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			ev.Type = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			ev.Data = append(ev.Data, []byte(strings.TrimPrefix(line, "data: "))...)
		}
	}
	return sc.Err()
}

func (a *Agent) handle(ctx context.Context, ev sseEvent) {
	if a.Latency > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(a.Latency):
		}
	}
	var err error
	switch ev.Type {
	case "rfq.matched":
		err = a.onMatched(ctx, ev.Data)
	case "message.received":
		err = a.onCounter(ctx, ev.Data)
	default:
		a.Logger.Debug().Str("type", ev.Type).Msg("simseller: event")
	}
	if err != nil {
		a.Logger.Error().Err(err).Str("type", ev.Type).Str("persona", a.Persona.Name).Msg("simseller: handle failed")
		return
	}
	if ev.ID != "" {
		a.last = ev.ID
		_ = a.post(ctx, "/agent/v1/events/"+ev.ID+"/ack", map[string]any{})
	}
}

var numRe = regexp.MustCompile(`\d[\d,]*`)

// BasePrice is the deterministic "fair" price for an RFQ in cents, shared by
// every persona so competing quotes are comparable.
func BasePrice(rfqID, title, quantity string) int64 {
	qty := int64(100)
	if m := numRe.FindString(quantity + " " + title); m != "" {
		if n, err := strconv.ParseInt(strings.ReplaceAll(m, ",", ""), 10, 64); err == nil && n > 0 {
			qty = n
		}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(title)))
	unit := int64(150 + h.Sum32()%2350) // 1.50 .. 25.00 per unit
	base := qty * unit
	if base < 50_000 {
		base = 50_000
	}
	return base
}

// jitter is a deterministic ±12% per-RFQ price spread so different personas
// win different RFQs.
func jitter(key string) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return 0.88 + float64(h.Sum32()%241)/1000
}

func roundDollars(c int64) int64 { return int64(math.Round(float64(c)/100)) * 100 }

func (a *Agent) onMatched(ctx context.Context, data []byte) error {
	var ev struct {
		RFQID        string `json:"rfq_id"`
		Title        string `json:"title"`
		Requirements struct {
			Quantity string `json:"quantity"`
		} `json:"requirements"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	opening := roundDollars(int64(float64(BasePrice(ev.RFQID, ev.Title, ev.Requirements.Quantity)) * a.Persona.Markup * jitter(ev.RFQID+a.Persona.Name)))
	a.mu.Lock()
	a.deals[ev.RFQID] = &deal{Opening: opening, Current: opening, Delivery: a.Persona.DeliveryDays}
	a.mu.Unlock()
	return a.command(ctx, "quote.submit", "sim-"+ev.RFQID+"-submit", ev.RFQID, map[string]any{
		"price": float64(opening) / 100, "currency": "AED", "delivery_days": a.Persona.DeliveryDays,
		"notes": "Standard spec, FOB origin. Price valid 14 days.",
	})
}

// NextPrice is the persona's response to a buyer offer: concede a fraction of
// the gap, never below the floor, never below the buyer's own offer.
func NextPrice(p Persona, opening, current, offer int64) int64 {
	floor := int64(float64(opening) * p.Floor)
	next := current - int64(float64(current-offer)*p.Concede)
	if next < floor {
		next = floor
	}
	if next < offer {
		next = offer
	}
	next = roundDollars(next)
	if next > current {
		next = current
	}
	return next
}

func (a *Agent) onCounter(ctx context.Context, data []byte) error {
	var ev struct {
		QuoteID string `json:"quote_id"`
		RFQID   string `json:"rfq_id"`
		Counter struct {
			PriceCents   *int64 `json:"price_cents"`
			DeliveryDays *int32 `json:"delivery_days"`
		} `json:"counter"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return err
	}
	if ev.Counter.PriceCents == nil {
		return nil // plain message, nothing to price
	}
	offer := *ev.Counter.PriceCents
	a.mu.Lock()
	d := a.deals[ev.RFQID]
	if d == nil { // restarted: reconstruct a plausible position
		cur := roundDollars(offer * 120 / 100)
		d = &deal{Opening: cur, Current: cur, Delivery: a.Persona.DeliveryDays}
		a.deals[ev.RFQID] = d
	}
	next := NextPrice(a.Persona, d.Opening, d.Current, offer)
	if ev.Counter.DeliveryDays != nil && *ev.Counter.DeliveryDays < d.Delivery && a.Persona.FlexDelivery {
		d.Delivery = *ev.Counter.DeliveryDays
	}
	held := next >= d.Current
	d.Current = next
	d.Counters++
	n, delivery := d.Counters, d.Delivery
	a.mu.Unlock()

	notes := fmt.Sprintf("We can do %s.", aed(next))
	if held {
		notes = fmt.Sprintf("%s is our best and final price.", aed(next))
	} else if next == offer {
		notes = "Agreed — we can meet your price."
	}
	return a.command(ctx, "quote.counter", fmt.Sprintf("sim-%s-c%d-%d", ev.QuoteID, n, offer), "", map[string]any{
		"quote_id": ev.QuoteID, "price": float64(next) / 100, "currency": "AED", "delivery_days": delivery, "notes": notes,
	})
}

func aed(c int64) string { return fmt.Sprintf("AED %.2f", float64(c)/100) }

func (a *Agent) command(ctx context.Context, typ, key, rfqID string, payload map[string]any) error {
	body := map[string]any{"type": typ, "idempotency_key": key, "payload": payload}
	if rfqID != "" {
		body["rfq_id"] = rfqID
	}
	return a.post(ctx, "/agent/v1/commands", body)
}

func (a *Agent) post(ctx context.Context, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.GatewayURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Credential)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, rb)
	}
	return nil
}
