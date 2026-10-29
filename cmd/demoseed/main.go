// Command demoseed populates a LOCAL TradeBridge stack with a demo buyer,
// three simulated sellers and a set of RFQs negotiated end-to-end by the
// buyer negotiation engine, so every screen has realistic data. It talks to
// the public API like a real client, plus direct DB writes for dev-only
// shortcuts (email verification, plan, back-dating deals for charts).
// Never point it at production.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/simseller"
)

var (
	apiURL  = flag.String("api", "http://localhost:8080", "Core API base URL")
	gwURL   = flag.String("gateway", "http://localhost:8081", "Agent Gateway base URL")
	dbURL   = flag.String("db", envOr("DATABASE_URL", "postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable"), "Postgres URL")
	pass    = flag.String("password", "Password123!", "password for demo accounts")
	credOut = flag.String("creds-out", ".simseller.env", "file to write simulated seller credentials to")
)

type account struct {
	Name, Email, Type string
	Token, CompanyID  string
}

type demoRFQ struct {
	Title, Text string
	AutoAccept  bool
	Negotiate   bool
	Tone        string
	MonthsAgo   int
}

var rfqs = []demoRFQ{
	{"5,000 corrugated shipping cartons", "Need 5000 double-wall corrugated shipping cartons, 40x30x30cm, printed logo, for packaging. Deliver to Dubai.", true, true, "collaborative", 5},
	{"1,200 heat-treated wooden pallets", "Looking for 1200 ISPM-15 heat-treated wooden pallets 1200x1000 for packaging and export, Jebel Ali.", true, true, "firm", 4},
	{"20,000 custom printed mailer boxes", "20000 custom printed kraft mailer boxes for e-commerce packaging, 2-colour print, Dubai warehouse.", true, true, "collaborative", 3},
	{"800 rolls stretch wrap film", "800 rolls of 23 micron machine stretch wrap film for pallet packaging, delivery Abu Dhabi.", true, true, "firm", 2},
	{"3,000 food-grade packaging tubs", "3000 food-grade PP packaging tubs 1L with lids, delivered to Sharjah.", true, true, "collaborative", 1},
	{"10,000 bubble wrap mailers", "10000 padded bubble wrap mailers A4 size for packaging, Dubai.", false, true, "collaborative", 0},
	{"2,500 moving boxes with handles", "2500 heavy-duty moving boxes with hand holes, packaging for relocation business, Dubai.", false, false, "", 0},
}

func main() {
	flag.Parse()
	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatal().Err(err).Msg("db connect")
	}
	defer pool.Close()

	buyer := &account{Name: "Northwind Procurement", Email: "demo@northwind.test", Type: "buyer"}
	sellers := []*account{
		{Name: "Gulf Packaging Co", Email: "agent@gulfpack.test", Type: "seller"},
		{Name: "Emirates Box Works", Email: "agent@emiratesbox.test", Type: "seller"},
		{Name: "Desert Pallet & Crate", Email: "agent@desertpallet.test", Type: "seller"},
	}
	all := append([]*account{buyer}, sellers...)
	for _, a := range all {
		must(log, signup(ctx, a), "signup "+a.Email)
	}
	// Dev shortcuts: verified emails + Pro plan (agent autonomy, auto-accept).
	for _, a := range all {
		_, err := pool.Exec(ctx, `UPDATE users SET email_verified_at = COALESCE(email_verified_at, NOW()) WHERE email = $1`, a.Email)
		must(log, err, "verify")
		_, err = pool.Exec(ctx, `INSERT INTO subscriptions (company_id, plan, status, current_period_end)
			VALUES ($1, 'pro', 'active', NOW() + INTERVAL '1 year')
			ON CONFLICT (company_id) DO UPDATE SET plan = 'pro', status = 'active', current_period_end = EXCLUDED.current_period_end`, a.CompanyID)
		must(log, err, "subscription")
	}
	var creds []string
	for _, s := range sellers {
		_, err := call(ctx, "PUT", "/v1/suppliers/me", s.Token, map[string]any{
			"description": s.Name + " — packaging, cartons, pallets and shipping supplies.",
			"categories":  []string{"packaging"}, "location": "Dubai",
		})
		must(log, err, "supplier profile")
		out, err := call(ctx, "POST", "/v1/agent-credentials", s.Token, map[string]any{"label": "simseller"})
		must(log, err, "agent credential")
		creds = append(creds, out["secret"].(string))
	}
	must(log, os.WriteFile(*credOut, []byte("SIM_CREDENTIALS="+strings.Join(creds, ",")+"\n"), 0o600), "write creds")

	agentCtx, stopAgents := context.WithCancel(ctx)
	defer stopAgents()
	for i, c := range creds {
		a := &simseller.Agent{GatewayURL: *gwURL, Credential: c, Persona: simseller.Personas[i%len(simseller.Personas)], Latency: 150 * time.Millisecond, Logger: log.Level(zerolog.WarnLevel)}
		go a.Run(agentCtx)
	}
	time.Sleep(time.Second) // let agents connect

	for _, r := range rfqs {
		out, err := call(ctx, "POST", "/v1/rfqs", buyer.Token, map[string]any{"title": r.Title, "text": r.Text})
		must(log, err, "create rfq")
		id := out["rfq"].(map[string]any)["id"].(string)
		if err := waitFor(ctx, 30*time.Second, func() (bool, error) {
			q, err := call(ctx, "GET", "/v1/rfqs/"+id+"/quotes", buyer.Token, nil)
			if err != nil {
				return false, err
			}
			qs, _ := q["quotes"].([]any)
			return len(qs) >= len(sellers), nil
		}); err != nil {
			log.Warn().Err(err).Str("rfq", r.Title).Msg("not all sellers quoted")
		}
		if !r.Negotiate {
			log.Info().Str("rfq", r.Title).Msg("left un-negotiated")
			continue
		}
		view, err := call(ctx, "GET", "/v1/rfqs/"+id+"/negotiation", buyer.Token, nil)
		must(log, err, "get negotiation")
		sug, _ := view["suggested"].(map[string]any)
		if sug == nil {
			log.Warn().Str("rfq", r.Title).Msg("no quotes to negotiate")
			continue
		}
		target := int64(sug["target_price_cents"].(float64))
		maxP := int64(sug["max_price_cents"].(float64))
		_, err = call(ctx, "PUT", "/v1/rfqs/"+id+"/negotiation", buyer.Token, map[string]any{
			"target_price_cents": target, "max_price_cents": maxP, "max_delivery_days": 14,
			"price_weight": 0.75, "max_rounds": 4, "auto_accept": r.AutoAccept, "tone": r.Tone,
		})
		must(log, err, "set strategy")
		var status string
		_ = waitFor(ctx, 60*time.Second, func() (bool, error) {
			v, err := call(ctx, "POST", "/v1/rfqs/"+id+"/negotiation/run", buyer.Token, map[string]any{})
			if err != nil {
				return false, err
			}
			status = v["strategy"].(map[string]any)["status"].(string)
			return status != "active", nil
		})
		log.Info().Str("rfq", r.Title).Str("status", status).Msg("negotiated")
		if r.MonthsAgo > 0 {
			ts := fmt.Sprintf("NOW() - INTERVAL '%d months' - INTERVAL '%d days'", r.MonthsAgo, 3+r.MonthsAgo)
			_, err = pool.Exec(ctx, `UPDATE orders SET created_at = `+ts+` WHERE rfq_id = $1`, id)
			must(log, err, "backdate order")
			_, err = pool.Exec(ctx, `UPDATE rfqs SET created_at = `+ts+` WHERE id = $1`, id)
			must(log, err, "backdate rfq")
		}
	}
	stopAgents()

	fmt.Printf("\nDemo data ready.\n  Buyer login: %s / %s\n  Seller credentials written to %s\n  Live sellers: set -a; . ./%s; set +a; go run ./cmd/simseller\n",
		buyer.Email, *pass, *credOut, *credOut)
}

func signup(ctx context.Context, a *account) error {
	out, err := call(ctx, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": a.Name, "company_type": a.Type, "email": a.Email, "password": *pass,
	})
	if err != nil {
		out, err = call(ctx, "POST", "/v1/auth/login", "", map[string]any{"email": a.Email, "password": *pass})
		if err != nil {
			return err
		}
	}
	a.Token, _ = out["token"].(string)
	a.CompanyID, _ = out["company_id"].(string)
	if a.Token == "" || a.CompanyID == "" {
		return errors.New("auth response missing token/company_id")
	}
	return nil
}

func call(ctx context.Context, method, path, token string, body any) (map[string]any, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, *apiURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

func waitFor(ctx context.Context, d time.Duration, f func() (bool, error)) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		ok, err := f()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
	return errors.New("timed out")
}

func must(log zerolog.Logger, err error, what string) {
	if err != nil {
		log.Fatal().Err(err).Msg(what)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
