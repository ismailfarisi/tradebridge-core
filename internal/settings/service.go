// Package settings stores per-company provider configuration (RFQ extraction
// model + API key) entered from the frontend. Secret keys are encrypted at
// rest; reads return only a masked hint. DB values override the env defaults.
package settings

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/cryptobox"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Service resolves and updates company settings. It implements
// rfq.ExtractorResolver.
type Service struct {
	q   *sqlc.Queries
	box *cryptobox.Box
	env rfq.ProviderConfig // process-env fallback
}

// NewService builds the settings service with the env-default extraction config.
func NewService(q *sqlc.Queries, box *cryptobox.Box, env rfq.ProviderConfig) *Service {
	return &Service{q: q, box: box, env: env}
}

// ExtractionView is the non-secret projection returned to the UI.
type ExtractionView struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	HasKey   bool   `json:"has_key"`
	KeyLast4 string `json:"key_last4"`
}

// ExtractionInput is a settings update from the UI. APIKey is applied only when
// non-empty; ClearKey removes a stored key.
type ExtractionInput struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	ClearKey bool   `json:"clear_api_key"`
}

// ExtractionConfig resolves the effective extraction config for a company:
// stored settings layered over the env defaults. Implements rfq.ExtractorResolver.
func (s *Service) ExtractionConfig(ctx context.Context, companyID string) (rfq.ProviderConfig, error) {
	row, err := s.q.GetCompanySettings(ctx, companyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.env, nil
	} else if err != nil {
		return rfq.ProviderConfig{}, err
	}

	cfg := s.env
	if row.ExtractionProvider != "" {
		cfg.Provider = row.ExtractionProvider
	}
	if row.ExtractionModel != "" {
		cfg.Model = row.ExtractionModel
	}
	if row.ExtractionBaseUrl != "" {
		cfg.BaseURL = row.ExtractionBaseUrl
	}
	if len(row.ExtractionApiKeyEnc) > 0 {
		key, err := s.box.Open(row.ExtractionApiKeyEnc)
		if err != nil {
			return rfq.ProviderConfig{}, err
		}
		cfg.APIKey = key
	}
	return cfg, nil
}

// Get returns the masked view of a company's settings.
func (s *Service) Get(ctx context.Context, companyID string) (ExtractionView, error) {
	row, err := s.q.GetCompanySettings(ctx, companyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ExtractionView{}, nil
	} else if err != nil {
		return ExtractionView{}, err
	}
	return ExtractionView{
		Provider: row.ExtractionProvider,
		Model:    row.ExtractionModel,
		BaseURL:  row.ExtractionBaseUrl,
		HasKey:   len(row.ExtractionApiKeyEnc) > 0,
		KeyLast4: row.ExtractionApiKeyLast4,
	}, nil
}

// UpdateExtraction upserts a company's extraction settings and returns the
// masked view. An empty APIKey preserves the stored key unless ClearKey is set.
func (s *Service) UpdateExtraction(ctx context.Context, companyID string, in ExtractionInput) (ExtractionView, error) {
	existing, err := s.q.GetCompanySettings(ctx, companyID)
	haveExisting := true
	if errors.Is(err, pgx.ErrNoRows) {
		haveExisting = false
	} else if err != nil {
		return ExtractionView{}, err
	}

	var enc []byte
	last4 := ""
	switch {
	case in.ClearKey:
		// leave enc nil / last4 empty
	case in.APIKey != "":
		sealed, err := s.box.Seal(in.APIKey)
		if err != nil {
			return ExtractionView{}, err
		}
		enc = sealed
		last4 = mask(in.APIKey)
	case haveExisting:
		enc = existing.ExtractionApiKeyEnc
		last4 = existing.ExtractionApiKeyLast4
	}

	row, err := s.q.UpsertCompanySettings(ctx, sqlc.UpsertCompanySettingsParams{
		CompanyID:             companyID,
		ExtractionProvider:    in.Provider,
		ExtractionModel:       in.Model,
		ExtractionBaseUrl:     in.BaseURL,
		ExtractionApiKeyEnc:   enc,
		ExtractionApiKeyLast4: last4,
	})
	if err != nil {
		return ExtractionView{}, err
	}
	return ExtractionView{
		Provider: row.ExtractionProvider,
		Model:    row.ExtractionModel,
		BaseURL:  row.ExtractionBaseUrl,
		HasKey:   len(row.ExtractionApiKeyEnc) > 0,
		KeyLast4: row.ExtractionApiKeyLast4,
	}, nil
}

// mask returns the last 4 characters of a secret for a UI hint.
func mask(secret string) string {
	if len(secret) <= 4 {
		return secret
	}
	return secret[len(secret)-4:]
}
