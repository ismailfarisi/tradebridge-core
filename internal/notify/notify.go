// Package notify is the platform's single "reach a human" surface: it turns
// domain occurrences (an RFQ matched, a quote was submitted, an order was
// created, an email needs verifying) into notifications and transactional
// email, so the rest of the codebase never talks to a delivery provider or
// template directly.
package notify

const (
	TypeRFQMatched     = "rfq.matched"
	TypeQuoteSubmitted = "quote.submitted"
	TypeOrderCreated   = "order.created"
	TypeEmailVerify    = "email.verify"
)

// Event is a single thing worth telling a company about.
type Event struct {
	Type     string
	Title    string
	Body     string
	Data     map[string]any
	CTALabel string
	CTAPath  string
}
