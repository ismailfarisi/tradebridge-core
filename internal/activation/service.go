package activation

import (
	"context"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Step is a single onboarding step.
type Step struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Done    bool   `json:"done"`
	CTAHref string `json:"cta_href"`
	Hint    string `json:"hint"`
}

// Status is the activation snapshot for a company.
type Status struct {
	Role            string `json:"role"`
	PercentComplete int    `json:"percent_complete"`
	Steps           []Step `json:"steps"`
}

type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service { return &Service{q: q} }

// Status computes the ordered step list + percent for a company. sellers/both
// get the seller path; buyers get the buyer path.
func (s *Service) Status(ctx context.Context, companyID string) (Status, error) {
	company, err := s.q.GetCompanyByID(ctx, companyID)
	if err != nil {
		return Status{}, err
	}
	sig, err := s.q.GetActivationSignals(ctx, companyID)
	if err != nil {
		return Status{}, err
	}
	role := "buyer"
	if company.Type == "seller" || company.Type == "both" {
		role = "seller"
	}
	steps := buildSteps(role, company.VerificationStatus, sig)
	done := 0
	for _, st := range steps {
		if st.Done {
			done++
		}
	}
	pct := 0
	if len(steps) > 0 {
		pct = int(float64(done) / float64(len(steps)) * 100.0)
	}
	return Status{Role: role, PercentComplete: pct, Steps: steps}, nil
}

func buildSteps(role, verificationStatus string, sig sqlc.GetActivationSignalsRow) []Step {
	verified := verificationStatus == "verified"
	if role == "seller" {
		return []Step{
			{Key: "profile", Label: "Complete your profile", Done: sig.HasProfile, CTAHref: "/seller/profile", Hint: "Add a description and at least one category."},
			{Key: "catalog", Label: "Add catalog items", Done: sig.CatalogCount > 0, CTAHref: "/seller/catalog", Hint: "Upload products so your agent can quote."},
			{Key: "verified", Label: "Verify your domain", Done: verified, CTAHref: "/settings/verification", Hint: "Verify to earn the trust badge and rank higher."},
			{Key: "agent_connected", Label: "Connect your agent", Done: sig.AgentConnected, CTAHref: "/seller/agent", Hint: "Issue a credential and connect your seller agent."},
			{Key: "first_quote", Label: "Send your first quote", Done: sig.QuotesSubmitted > 0, CTAHref: "/seller/agent", Hint: "Respond to an incoming RFQ to go live."},
		}
	}
	return []Step{
		{Key: "first_rfq", Label: "Post your first RFQ", Done: sig.RfqsPosted > 0, CTAHref: "/chat", Hint: "Describe what you need in plain language."},
		{Key: "received_quote", Label: "Receive a quote", Done: sig.QuotesReceived > 0, CTAHref: "/rfqs", Hint: "Sellers' agents quote your matched RFQs."},
		{Key: "first_order", Label: "Place your first order", Done: sig.OrdersAsBuyer > 0, CTAHref: "/rfqs", Hint: "Accept a quote to create a purchase order."},
	}
}
