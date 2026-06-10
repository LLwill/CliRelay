package modelcatalog

import (
	"context"
	"errors"
	"strings"
	"time"

	modelconfigsettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/modelconfig"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

var ErrModelIDRequired = errors.New("model id is required")
var ErrAuthGroupRequired = errors.New("auth group is required")

// Config contract:
// - Owner: model config / owner preset / pricing management boundary.
// - Responsibility: CRUD and response shaping for stored model catalog settings.
func (s *Service) ListModelConfigs(scope string) map[string]any {
	rows := modelconfigsettings.ListConfigs(scope)
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, modelConfigResponse(row))
	}
	return map[string]any{"object": "list", "data": items}
}

func (s *Service) UpsertModelConfig(payload ModelConfigPayload, originalID, scope string) (map[string]any, error) {
	saved, err := modelconfigsettings.UpsertConfig(modelconfigsettings.UpsertConfigInput{
		OriginalID:                originalID,
		Scope:                     scope,
		ModelID:                   payload.ID,
		OwnedBy:                   payload.OwnedBy,
		Description:               payload.Description,
		Enabled:                   payload.Enabled,
		InputModalities:           payload.InputModalities,
		OutputModalities:          payload.OutputModalities,
		PricingMode:               payload.Pricing.Mode,
		InputPricePerMillion:      payload.Pricing.InputPricePerMillion,
		OutputPricePerMillion:     payload.Pricing.OutputPricePerMillion,
		CachedPricePerMillion:     payload.Pricing.CachedPricePerMillion,
		CacheReadPricePerMillion:  payload.Pricing.CacheReadPricePerMillion,
		CacheWritePricePerMillion: payload.Pricing.CacheWritePricePerMillion,
		PricePerCall:              payload.Pricing.PricePerCall,
	})
	if err != nil {
		if errors.Is(err, modelconfigsettings.ErrModelIDRequired) {
			return nil, ErrModelIDRequired
		}
		return nil, err
	}
	return modelConfigResponse(saved), nil
}

func (s *Service) DeleteModelConfig(modelID string) error {
	if err := modelconfigsettings.DeleteConfig(modelID); err != nil {
		if errors.Is(err, modelconfigsettings.ErrModelIDRequired) {
			return ErrModelIDRequired
		}
		return err
	}
	return nil
}

func (s *Service) OwnerPresets() map[string]any {
	rows := modelconfigsettings.ListOwnerPresetsWithCounts()
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"value":       row.Value,
			"label":       row.Label,
			"description": row.Description,
			"enabled":     row.Enabled,
			"updated_at":  row.UpdatedAt,
			"model_count": row.ModelCount,
		})
	}
	return map[string]any{"items": items}
}

func (s *Service) ReplaceOwnerPresets(rows []usage.ModelOwnerPresetRow) (map[string]any, error) {
	if err := modelconfigsettings.ReplaceOwnerPresets(rows); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok", "updated": len(rows)}, nil
}

func (s *Service) AuthGroupOwnerMappings() map[string]any {
	rows := modelconfigsettings.ListAuthGroupOwnerMappings()
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"auth_group": row.AuthGroup,
			"owner":      row.Owner,
			"updated_at": row.UpdatedAt,
		})
	}
	return map[string]any{"items": items}
}

func (s *Service) PatchAuthGroupOwnerMapping(authGroup, owner string) (map[string]any, error) {
	authGroup = strings.TrimSpace(authGroup)
	owner = strings.TrimSpace(owner)
	if authGroup == "" {
		return nil, ErrAuthGroupRequired
	}
	if owner == "" {
		if err := modelconfigsettings.DeleteAuthGroupOwnerMapping(authGroup); err != nil {
			if errors.Is(err, modelconfigsettings.ErrAuthGroupRequired) {
				return nil, ErrAuthGroupRequired
			}
			return nil, err
		}
		return map[string]any{
			"status":     "ok",
			"auth_group": strings.ToLower(strings.Join(strings.Fields(authGroup), "-")),
			"deleted":    true,
		}, nil
	}

	saved, err := modelconfigsettings.UpsertAuthGroupOwnerMapping(authGroup, owner)
	if err != nil {
		if errors.Is(err, modelconfigsettings.ErrAuthGroupRequired) {
			return nil, ErrAuthGroupRequired
		}
		return nil, err
	}
	return map[string]any{
		"status":     "ok",
		"auth_group": saved.AuthGroup,
		"owner":      saved.Owner,
		"updated_at": saved.UpdatedAt,
	}, nil
}

func (s *Service) Pricing() map[string]any {
	rows := modelconfigsettings.ListPricing()
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"model_id":                      row.ModelID,
			"input_price_per_million":       row.InputPricePerMillion,
			"output_price_per_million":      row.OutputPricePerMillion,
			"cached_price_per_million":      row.CachedPricePerMillion,
			"cache_read_price_per_million":  row.CacheReadPricePerMillion,
			"cache_write_price_per_million": row.CacheWritePricePerMillion,
			"updated_at":                    row.UpdatedAt,
		})
	}
	return map[string]any{"items": items}
}

func (s *Service) UpdatePricing(items []ModelPricingUpdateItem) (map[string]any, error) {
	upserts := make([]modelconfigsettings.PricingUpsertItem, 0, len(items))
	for _, item := range items {
		upserts = append(upserts, modelconfigsettings.PricingUpsertItem{
			ModelID:                   item.ModelID,
			InputPricePerMillion:      item.InputPricePerMillion,
			OutputPricePerMillion:     item.OutputPricePerMillion,
			CachedPricePerMillion:     item.CachedPricePerMillion,
			CacheReadPricePerMillion:  item.CacheReadPricePerMillion,
			CacheWritePricePerMillion: item.CacheWritePricePerMillion,
		})
	}
	updated, err := modelconfigsettings.UpsertPricing(upserts)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok", "updated": updated}, nil
}

func normalizeContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, 2*time.Minute)
}
