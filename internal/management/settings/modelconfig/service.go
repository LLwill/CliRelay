package modelconfig

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

var ErrModelIDRequired = errors.New("model id is required")
var ErrAuthGroupRequired = errors.New("auth group is required")

type UpsertConfigInput struct {
	OriginalID                string
	Scope                     string
	ModelID                   string
	OwnedBy                   string
	Description               string
	Enabled                   bool
	InputModalities           *[]string
	OutputModalities          *[]string
	PricingMode               string
	InputPricePerMillion      float64
	OutputPricePerMillion     float64
	CachedPricePerMillion     float64
	CacheReadPricePerMillion  float64
	CacheWritePricePerMillion float64
	PricePerCall              float64
}

type OwnerPresetWithCount struct {
	usage.ModelOwnerPresetRow
	ModelCount int `json:"model_count"`
}

type PricingUpsertItem struct {
	ModelID                   string
	InputPricePerMillion      float64
	OutputPricePerMillion     float64
	CachedPricePerMillion     float64
	CacheReadPricePerMillion  float64
	CacheWritePricePerMillion float64
}

func NormalizeScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "all", "library":
		return strings.ToLower(strings.TrimSpace(scope))
	default:
		return "active"
	}
}

func ListConfigs(scope string) []usage.ModelConfigRow {
	return filterRowsByScope(usage.ListModelConfigs(), NormalizeScope(scope))
}

func ListAllConfigs() []usage.ModelConfigRow {
	return usage.ListModelConfigs()
}

func GetConfig(modelID string) (usage.ModelConfigRow, bool) {
	return usage.GetModelConfig(strings.TrimSpace(modelID))
}

func UpsertConfig(input UpsertConfigInput) (usage.ModelConfigRow, error) {
	scope := NormalizeScope(input.Scope)
	originalID := strings.TrimSpace(input.OriginalID)
	row := usage.ModelConfigRow{
		ModelID:                   strings.TrimSpace(input.ModelID),
		OwnedBy:                   strings.TrimSpace(input.OwnedBy),
		Description:               strings.TrimSpace(input.Description),
		Enabled:                   input.Enabled,
		PricingMode:               strings.TrimSpace(input.PricingMode),
		InputPricePerMillion:      input.InputPricePerMillion,
		OutputPricePerMillion:     input.OutputPricePerMillion,
		CachedPricePerMillion:     input.CachedPricePerMillion,
		CacheReadPricePerMillion:  input.CacheReadPricePerMillion,
		CacheWritePricePerMillion: input.CacheWritePricePerMillion,
		PricePerCall:              input.PricePerCall,
		Source:                    sourceForScope(scope),
	}
	if input.InputModalities != nil {
		row.InputModalities = *input.InputModalities
	}
	if input.OutputModalities != nil {
		row.OutputModalities = *input.OutputModalities
	}
	if row.ModelID == "" {
		row.ModelID = originalID
	}
	if row.ModelID == "" {
		return usage.ModelConfigRow{}, ErrModelIDRequired
	}

	lookupID := row.ModelID
	if originalID != "" {
		lookupID = originalID
	}
	if existing, ok := usage.GetModelConfig(lookupID); ok {
		if input.InputModalities == nil {
			row.InputModalities = existing.InputModalities
		}
		if input.OutputModalities == nil {
			row.OutputModalities = existing.OutputModalities
		}
	}

	if originalID != "" && originalID != row.ModelID {
		if err := usage.DeleteModelConfig(originalID); err != nil {
			return usage.ModelConfigRow{}, err
		}
	}
	if err := usage.UpsertModelConfig(row); err != nil {
		return usage.ModelConfigRow{}, err
	}

	saved, ok := usage.GetModelConfig(row.ModelID)
	if !ok {
		return row, nil
	}
	return saved, nil
}

func DeleteConfig(modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ErrModelIDRequired
	}
	return usage.DeleteModelConfig(modelID)
}

func ListOwnerPresetsWithCounts() []OwnerPresetWithCount {
	modelCounts := make(map[string]int)
	for _, model := range usage.ListModelConfigs() {
		if model.OwnedBy != "" {
			modelCounts[model.OwnedBy]++
		}
	}

	rows := usage.ListModelOwnerPresets()
	items := make([]OwnerPresetWithCount, 0, len(rows))
	for _, row := range rows {
		items = append(items, OwnerPresetWithCount{
			ModelOwnerPresetRow: row,
			ModelCount:          modelCounts[row.Value],
		})
	}
	return items
}

func ReplaceOwnerPresets(rows []usage.ModelOwnerPresetRow) error {
	return usage.ReplaceModelOwnerPresets(rows)
}

func ListAuthGroupOwnerMappings() []usage.AuthGroupOwnerMappingRow {
	return usage.ListAuthGroupOwnerMappings()
}

func UpsertAuthGroupOwnerMapping(authGroup, owner string) (usage.AuthGroupOwnerMappingRow, error) {
	row := usage.AuthGroupOwnerMappingRow{
		AuthGroup: strings.TrimSpace(authGroup),
		Owner:     strings.TrimSpace(owner),
	}
	if row.AuthGroup == "" {
		return usage.AuthGroupOwnerMappingRow{}, ErrAuthGroupRequired
	}
	if err := usage.UpsertAuthGroupOwnerMapping(row); err != nil {
		return usage.AuthGroupOwnerMappingRow{}, err
	}
	saved, ok := usage.GetAuthGroupOwnerMapping(row.AuthGroup)
	if !ok {
		return row, nil
	}
	return saved, nil
}

func DeleteAuthGroupOwnerMapping(authGroup string) error {
	authGroup = strings.TrimSpace(authGroup)
	if authGroup == "" {
		return ErrAuthGroupRequired
	}
	return usage.DeleteAuthGroupOwnerMapping(authGroup)
}

func ListPricing() []usage.ModelPricingRow {
	pricingMap := usage.GetAllModelPricing()
	items := make([]usage.ModelPricingRow, 0, len(pricingMap))
	for _, row := range pricingMap {
		items = append(items, row)
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(items[i].ModelID)) < strings.ToLower(strings.TrimSpace(items[j].ModelID))
	})
	return items
}

func UpsertPricing(items []PricingUpsertItem) (int, error) {
	updated := 0
	for _, item := range items {
		modelID := strings.TrimSpace(item.ModelID)
		if modelID == "" {
			continue
		}
		if err := usage.UpsertModelPricingV2(
			modelID,
			item.InputPricePerMillion,
			item.OutputPricePerMillion,
			item.CachedPricePerMillion,
			item.CacheReadPricePerMillion,
			item.CacheWritePricePerMillion,
		); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func GetOpenRouterSyncState() usage.OpenRouterModelSyncState {
	return usage.GetOpenRouterModelSyncState()
}

func UpdateOpenRouterSyncSettings(enabled bool, intervalMinutes int) (usage.OpenRouterModelSyncState, error) {
	return usage.UpdateOpenRouterModelSyncSettings(enabled, intervalMinutes)
}

func RunOpenRouterSync(ctx context.Context) (usage.OpenRouterModelSyncResult, usage.OpenRouterModelSyncState, error) {
	return usage.RunOpenRouterModelSync(ctx)
}

func sourceForScope(scope string) string {
	if scope == "library" {
		return "seed"
	}
	return "user"
}

func availableModelIDSet() map[string]bool {
	modelRegistry := registry.GetGlobalRegistry()
	availableModels := modelRegistry.GetAvailableModels("openai")
	result := make(map[string]bool, len(availableModels))
	for _, model := range availableModels {
		id, _ := model["id"].(string)
		id = strings.TrimSpace(id)
		if id != "" {
			result[id] = true
		}
	}
	return result
}

func filterRowsByScope(rows []usage.ModelConfigRow, scope string) []usage.ModelConfigRow {
	availableIDs := map[string]bool(nil)
	if scope == "active" {
		availableIDs = availableModelIDSet()
	}

	filtered := make([]usage.ModelConfigRow, 0, len(rows))
	for _, row := range rows {
		source := strings.ToLower(strings.TrimSpace(row.Source))
		switch scope {
		case "all":
			filtered = append(filtered, row)
		case "library":
			if source == "seed" || source == "openrouter" {
				filtered = append(filtered, row)
			}
		default:
			if source == "user" || (source == "seed" && availableIDs[row.ModelID]) {
				filtered = append(filtered, row)
			}
		}
	}
	return filtered
}
