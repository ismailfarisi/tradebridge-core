package billing

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves checkout/portal (authed) and the webhook (public).
type Handler struct {
	q          *sqlc.Queries
	provider   Billing
	svc        *Service
	appBaseURL string
	// active gates all billing routes: true when a real Stripe provider is
	// configured (Enabled()==true) or an operator has explicitly opted into
	// BILLING_DEV_MODE for the unsigned StubBilling flow (dev/test only).
	// When false, checkout/portal/webhook all fail closed with 503 rather
	// than parsing an unsigned webhook or returning a stub checkout URL.
	active bool
}

// NewHandler builds the billing handler. active must be true only when a
// real Stripe provider is configured or BILLING_DEV_MODE is explicitly set;
// otherwise all billing routes fail closed (see active).
func NewHandler(q *sqlc.Queries, provider Billing, svc *Service, appBaseURL string, active bool) *Handler {
	return &Handler{q: q, provider: provider, svc: svc, appBaseURL: appBaseURL, active: active}
}

// Register mounts the PUBLIC webhook (Stripe calls it with a signature, not a
// JWT) and the authed checkout/portal routes under the tenant guard.
//
// The webhook is registered via app.Post BEFORE the guarded group below: in
// Fiber, a Use-middleware group (what app.Group(prefix, guard...) installs)
// only intercepts routes registered AFTER it at a matching path prefix, so
// registering the plain webhook route first keeps it unauthenticated even
// though it shares the "/v1/billing" prefix with the guarded group.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Post("/v1/billing/webhook", h.webhook) // PUBLIC — signature-verified inside

	g := app.Group("/v1/billing", guard...)
	g.Post("/checkout", middleware.RequireRole("owner", "admin"), h.checkout)
	g.Post("/portal", middleware.RequireRole("owner", "admin"), h.portal)
}

type checkoutReq struct {
	Plan     string `json:"plan"`
	Interval string `json:"interval"`
}

func (h *Handler) checkout(c *fiber.Ctx) error {
	if !h.active {
		return fiber.NewError(fiber.StatusServiceUnavailable, "billing not configured")
	}
	var req checkoutReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Plan != "pro" && req.Plan != "business" {
		return fiber.NewError(fiber.StatusBadRequest, "plan must be pro|business")
	}
	if req.Interval != "month" && req.Interval != "year" {
		return fiber.NewError(fiber.StatusBadRequest, "interval must be month|year")
	}

	ctx := c.Context()
	companyID := middleware.CompanyID(c)

	co, err := h.q.GetCompanyByID(ctx, companyID)
	if err != nil {
		return err
	}
	existing := ""
	if co.StripeCustomerID != nil {
		existing = *co.StripeCustomerID
	}
	email := h.callerEmail(c)

	customerID, err := h.provider.EnsureCustomer(ctx, companyID, email, existing)
	if err != nil {
		return err
	}
	if existing == "" && customerID != "" {
		if err := h.q.SetCompanyStripeCustomer(ctx, sqlc.SetCompanyStripeCustomerParams{
			ID: companyID, StripeCustomerID: &customerID,
		}); err != nil {
			return err
		}
	}

	url, err := h.provider.CreateCheckoutSession(ctx, CheckoutParams{
		CompanyID: companyID, CustomerID: customerID, CustomerEmail: email,
		Plan: req.Plan, Interval: req.Interval,
		SuccessURL: h.appBaseURL + "/settings/billing?checkout=success",
		CancelURL:  h.appBaseURL + "/settings/billing?checkout=cancel",
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"url": url})
}

func (h *Handler) portal(c *fiber.Ctx) error {
	if !h.active {
		return fiber.NewError(fiber.StatusServiceUnavailable, "billing not configured")
	}
	ctx := c.Context()
	co, err := h.q.GetCompanyByID(ctx, middleware.CompanyID(c))
	if err != nil {
		return err
	}
	if co.StripeCustomerID == nil || *co.StripeCustomerID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "no billing account yet")
	}
	url, err := h.provider.CreatePortalSession(ctx, *co.StripeCustomerID, h.appBaseURL+"/settings/billing")
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"url": url})
}

func (h *Handler) webhook(c *fiber.Ctx) error {
	if !h.active {
		return fiber.NewError(fiber.StatusServiceUnavailable, "billing not configured")
	}
	ev, err := h.provider.ParseWebhook(c.Body(), c.Get("Stripe-Signature"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook")
	}
	if err := h.svc.ApplyWebhookEvent(c.Context(), ev); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"received": true})
}

// callerEmail best-effort fetches the acting user's email for the Stripe customer.
func (h *Handler) callerEmail(c *fiber.Ctx) string {
	u, err := h.q.GetUserByID(c.Context(), middleware.UserID(c))
	if err != nil || errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	return u.Email
}
