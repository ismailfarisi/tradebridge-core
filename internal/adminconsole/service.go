// Package adminconsole owns platform-admin company management: search, detail,
// and suspend/unsuspend. It mounts under the existing PlatformAdminGuard realm.
package adminconsole

import (
	"context"
	"time"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

type Service struct {
	q   *sqlc.Queries
	ent *entitlements.Service
}

func NewService(q *sqlc.Queries, ent *entitlements.Service) *Service {
	return &Service{q: q, ent: ent}
}

type CompanyDetail struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Type               string        `json:"type"`
	Status             string        `json:"status"`
	VerificationStatus string        `json:"verification_status"`
	PlanCode           string        `json:"plan_code"`
	Users              []UserSummary `json:"users"`
	Counts             CompanyCounts `json:"counts"`
}

// UserSummary is the admin-safe projection of sqlc.User — it deliberately
// omits PasswordHash (and EmailVerifiedAt, not needed here) so the admin
// company-detail endpoint never serializes a credential.
type UserSummary struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type CompanyCounts struct {
	RFQs   int64 `json:"rfqs"`
	Quotes int64 `json:"quotes"`
	Orders int64 `json:"orders"`
}

func (s *Service) Detail(ctx context.Context, id string) (CompanyDetail, error) {
	co, err := s.q.GetCompanyByID(ctx, id)
	if err != nil {
		return CompanyDetail{}, err
	}
	d := CompanyDetail{ID: co.ID, Name: co.Name, Type: co.Type, Status: co.Status, PlanCode: s.ent.PlanCode(ctx, id)}
	if v, err := s.q.GetCompanyVerification(ctx, id); err == nil {
		d.VerificationStatus = v.VerificationStatus
	}
	if users, err := s.q.ListUsersByCompany(ctx, id); err == nil {
		d.Users = make([]UserSummary, 0, len(users))
		for _, u := range users {
			d.Users = append(d.Users, UserSummary{ID: u.ID, Email: u.Email, Role: u.Role, CreatedAt: u.CreatedAt})
		}
	}
	if cnt, err := s.q.AdminCompanyCounts(ctx, id); err == nil {
		d.Counts = CompanyCounts{RFQs: cnt.Rfqs, Quotes: cnt.Quotes, Orders: cnt.Orders}
	}
	return d, nil
}
