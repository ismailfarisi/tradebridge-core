// Package settings stores the platform-wide provider configuration (RFQ
// extraction model + API key + prompts) entered from the admin portal. The
// settings are global — a single row — not per-company. Secret keys are
// encrypted at rest; reads return only a masked hint. DB values override the
// env defaults.
package settings

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/cryptobox"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Service resolves and updates the global platform settings. It implements
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

// SettingsView is the full masked view returned to the admin portal: the
// extraction provider config, the two configurable system prompts, and the
// built-in defaults (so the UI can show what an empty prompt falls back to).
type SettingsView struct {
	Extraction              ExtractionView `json:"extraction"`
	ExtractionSystemPrompt  string         `json:"extraction_system_prompt"`
	IntakeSystemPrompt      string         `json:"intake_system_prompt"`
	DefaultExtractionPrompt string         `json:"default_extraction_prompt"`
	DefaultIntakePrompt     string         `json:"default_intake_prompt"`
}

// ExtractionInput is a settings update from the admin portal. APIKey is
// applied only when non-empty; ClearKey removes a stored key.
type ExtractionInput struct {
	Provider               string `json:"provider"`
	Model                  string `json:"model"`
	BaseURL                string `json:"base_url"`
	APIKey                 string `json:"api_key"`
	ClearKey               bool   `json:"clear_api_key"`
	ExtractionSystemPrompt string `json:"extraction_system_prompt"`
	IntakeSystemPrompt     string `json:"intake_system_prompt"`
}

// ExtractionConfig resolves the effective extraction config: the global
// platform settings layered over the env defaults. The companyID argument is
// ignored — settings are global, not per-company. Implements
// rfq.ExtractorResolver.
func (s *Service) ExtractionConfig(ctx context.Context, _ string) (rfq.ProviderConfig, error) {
	row, err := s.q.GetPlatformSettings(ctx)
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
	cfg.ExtractionPrompt = row.ExtractionSystemPrompt
	cfg.IntakePrompt = row.IntakeSystemPrompt
	return cfg, nil
}

// Get returns the masked view of the global platform settings.
func (s *Service) Get(ctx context.Context) (SettingsView, error) {
	row, err := s.q.GetPlatformSettings(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return SettingsView{
			DefaultExtractionPrompt: rfq.DefaultExtractionPrompt,
			DefaultIntakePrompt:     rfq.DefaultIntakePrompt,
		}, nil
	} else if err != nil {
		return SettingsView{}, err
	}
	return SettingsView{
		Extraction: ExtractionView{
			Provider: row.ExtractionProvider,
			Model:    row.ExtractionModel,
			BaseURL:  row.ExtractionBaseUrl,
			HasKey:   len(row.ExtractionApiKeyEnc) > 0,
			KeyLast4: row.ExtractionApiKeyLast4,
		},
		ExtractionSystemPrompt:  row.ExtractionSystemPrompt,
		IntakeSystemPrompt:      row.IntakeSystemPrompt,
		DefaultExtractionPrompt: rfq.DefaultExtractionPrompt,
		DefaultIntakePrompt:     rfq.DefaultIntakePrompt,
	}, nil
}

// Update upserts the global platform settings and returns the masked view. An
// empty APIKey preserves the stored key unless ClearKey is set.
func (s *Service) Update(ctx context.Context, in ExtractionInput) (SettingsView, error) {
	existing, err := s.q.GetPlatformSettings(ctx)
	haveExisting := true
	if errors.Is(err, pgx.ErrNoRows) {
		haveExisting = false
	} else if err != nil {
		return SettingsView{}, err
	}

	var enc []byte
	last4 := ""
	switch {
	case in.ClearKey:
		// leave enc nil / last4 empty
	case in.APIKey != "":
		sealed, err := s.box.Seal(in.APIKey)
		if err != nil {
			return SettingsView{}, err
		}
		enc = sealed
		last4 = mask(in.APIKey)
	case haveExisting:
		enc = existing.ExtractionApiKeyEnc
		last4 = existing.ExtractionApiKeyLast4
	}

	row, err := s.q.UpsertPlatformSettings(ctx, sqlc.UpsertPlatformSettingsParams{
		ExtractionProvider:     in.Provider,
		ExtractionModel:        in.Model,
		ExtractionBaseUrl:      in.BaseURL,
		ExtractionApiKeyEnc:    enc,
		ExtractionApiKeyLast4:  last4,
		ExtractionSystemPrompt: in.ExtractionSystemPrompt,
		IntakeSystemPrompt:     in.IntakeSystemPrompt,
	})
	if err != nil {
		return SettingsView{}, err
	}
	return SettingsView{
		Extraction: ExtractionView{
			Provider: row.ExtractionProvider,
			Model:    row.ExtractionModel,
			BaseURL:  row.ExtractionBaseUrl,
			HasKey:   len(row.ExtractionApiKeyEnc) > 0,
			KeyLast4: row.ExtractionApiKeyLast4,
		},
		ExtractionSystemPrompt:  row.ExtractionSystemPrompt,
		IntakeSystemPrompt:      row.IntakeSystemPrompt,
		DefaultExtractionPrompt: rfq.DefaultExtractionPrompt,
		DefaultIntakePrompt:     rfq.DefaultIntakePrompt,
	}, nil
}

// mask returns the last 4 characters of a secret for a UI hint.
func mask(secret string) string {
	if len(secret) <= 4 {
		return secret
	}
	return secret[len(secret)-4:]
}
