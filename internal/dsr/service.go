// Package dsr implements GDPR data-subject rights: full data export and
// soft-delete/anonymize account deletion.
package dsr

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type Service struct {
	q    *sqlc.Queries
	pool *pgxpool.Pool
}

// NewService takes the pool now even though Export doesn't use it: Task 3's
// account-deletion flow needs a transaction across multiple tables, and
// defining the pool dependency here avoids a later signature change.
func NewService(q *sqlc.Queries, pool *pgxpool.Pool) *Service {
	return &Service{q: q, pool: pool}
}

// Bundle is the exported data, one entry per data section.
type Bundle map[string]any

// exportUser is a password-hash-free projection of sqlc.User. sqlc.User
// includes PasswordHash — a bcrypt hash must never leave the server in a
// data-export bundle (same lesson as the S8 adminconsole leak), so users are
// mapped through this DTO instead of being added to the bundle directly.
type exportUser struct {
	ID              string `json:"id"`
	CompanyID       string `json:"company_id"`
	Email           string `json:"email"`
	Role            string `json:"role"`
	CreatedAt       any    `json:"created_at"`
	EmailVerifiedAt any    `json:"email_verified_at"`
}

// Export assembles the company's full data footprint. Each section is
// best-effort: a section that errors is simply omitted rather than failing
// the whole export (export must never leave the subject unable to get their
// data). The company row itself is required.
func (s *Service) Export(ctx context.Context, companyID string) (Bundle, error) {
	b := Bundle{}

	company, err := s.q.GetCompanyByID(ctx, companyID)
	if err != nil {
		return nil, err
	}
	b["company"] = company

	if v, err := s.q.ListUsersByCompany(ctx, companyID); err == nil {
		users := make([]exportUser, 0, len(v))
		for _, u := range v {
			users = append(users, exportUser{
				ID:              u.ID,
				CompanyID:       u.CompanyID,
				Email:           u.Email,
				Role:            u.Role,
				CreatedAt:       u.CreatedAt,
				EmailVerifiedAt: u.EmailVerifiedAt,
			})
		}
		b["users"] = users
	}

	if v, err := s.q.GetSupplierProfileByCompany(ctx, companyID); err == nil {
		b["supplier_profile"] = v
	}

	if v, err := s.q.ListCatalogByCompany(ctx, companyID); err == nil {
		b["catalog"] = v
	}

	if v, err := s.q.ExportRFQsByCompany(ctx, companyID); err == nil {
		b["rfqs"] = v
	}

	if v, err := s.q.ExportQuotesByCompany(ctx, companyID); err == nil {
		b["quotes"] = v
	}

	if v, err := s.q.ListOrdersByCompany(ctx, companyID); err == nil {
		b["orders"] = v
	}

	if v, err := s.q.ExportInvoicesByCompany(ctx, companyID); err == nil {
		b["invoices"] = v
	}

	if v, err := s.q.ExportReviewsByCompany(ctx, companyID); err == nil {
		b["reviews"] = v
	}

	if v, err := s.q.ExportDisputesByCompany(ctx, companyID); err == nil {
		b["disputes"] = v
	}

	if v, err := s.q.GetSubscriptionByCompany(ctx, companyID); err == nil {
		b["subscription"] = v
	}

	if v, err := s.q.ExportActivationEventsByCompany(ctx, companyID); err == nil {
		b["activation_events"] = v
	}

	return b, nil
}
