package usage

import (
	"database/sql"
	"strings"
	"time"

	sqlmodelconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/sqlite/modelconfig"
)

type ModelConfigRow = sqlmodelconfig.ModelConfigRow
type ModelOwnerPresetRow = sqlmodelconfig.ModelOwnerPresetRow

func initModelConfigTables(db *sql.DB) {
	sqlmodelconfig.InitTables(db)
}

func modelConfigStore() sqlmodelconfig.Store {
	return sqlmodelconfig.NewStore(getDB())
}

func normalizeModelOwnerValue(value string) string {
	return sqlmodelconfig.NormalizeModelOwnerValue(value)
}

func normalizePricingMode(mode string) string {
	return sqlmodelconfig.NormalizePricingMode(mode)
}

func normalizeModelModalities(values []string) []string {
	return sqlmodelconfig.NormalizeModelModalities(values)
}

func ownerLabelForValue(value string) string {
	return sqlmodelconfig.OwnerLabelForValue(value)
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intToBool(value int) bool {
	return value != 0
}

func upsertLegacyPricingIntoModelConfig(db *sql.DB, modelID string, input, output, cached float64, updatedAt string) {
	sqlmodelconfig.UpsertLegacyPricingIntoModelConfig(db, modelID, input, output, cached, updatedAt)
}

func ListModelConfigs() []ModelConfigRow {
	return modelConfigStore().ListModelConfigs()
}

func GetModelConfig(modelID string) (ModelConfigRow, bool) {
	return modelConfigStore().GetModelConfig(modelID)
}

func UpsertModelConfig(row ModelConfigRow) error {
	modelID := strings.TrimSpace(row.ModelID)
	if modelID == "" {
		return modelConfigStore().UpsertModelConfig(row)
	}
	row.ModelID = modelID
	if err := modelConfigStore().UpsertModelConfig(row); err != nil {
		return err
	}

	saved, ok := modelConfigStore().GetModelConfig(modelID)
	if !ok {
		return nil
	}

	if saved.PricingMode == "token" {
		if err := UpsertModelPricingV2(
			saved.ModelID,
			saved.InputPricePerMillion,
			saved.OutputPricePerMillion,
			saved.CachedPricePerMillion,
			saved.CacheReadPricePerMillion,
			saved.CacheWritePricePerMillion,
		); err != nil {
			return err
		}
	} else if err := DeleteModelPricing(saved.ModelID); err != nil {
		return err
	}

	if saved.OwnedBy != "" {
		if err := UpsertModelOwnerPreset(ModelOwnerPresetRow{
			Value:   saved.OwnedBy,
			Label:   ownerLabelForValue(saved.OwnedBy),
			Enabled: true,
		}); err != nil {
			return err
		}
	}

	return nil
}

func DeleteModelConfig(modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if err := modelConfigStore().DeleteModelConfig(modelID); err != nil {
		return err
	}
	return DeleteModelPricing(modelID)
}

func ListModelOwnerPresets() []ModelOwnerPresetRow {
	return modelConfigStore().ListModelOwnerPresets()
}

func GetModelOwnerPreset(value string) (ModelOwnerPresetRow, bool) {
	return modelConfigStore().GetModelOwnerPreset(value)
}

func UpsertModelOwnerPreset(row ModelOwnerPresetRow) error {
	return modelConfigStore().UpsertModelOwnerPreset(row)
}

func ReplaceModelOwnerPresets(rows []ModelOwnerPresetRow) error {
	return modelConfigStore().ReplaceModelOwnerPresets(rows)
}
