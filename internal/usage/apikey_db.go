package usage

import (
	"database/sql"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sqlapikey "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/sqlite/apikey"
	log "github.com/sirupsen/logrus"
)

type APIKeyRow = sqlapikey.APIKeyRow

func APIKeyRowFromConfig(entry config.APIKeyEntry) APIKeyRow {
	return sqlapikey.APIKeyRowFromConfig(entry)
}

func initAPIKeysTable(db *sql.DB) {
	sqlapikey.InitTable(db)
}

func apiKeyStore() sqlapikey.Store {
	return sqlapikey.NewStore(getDB())
}

func defaultAPIKeyName(index int) string {
	return sqlapikey.DefaultAPIKeyName(index)
}

func backfillAPIKeyNames(db *sql.DB) {
	sqlapikey.BackfillNames(db)
}

// MigrateAPIKeysFromConfig moves API key entries from YAML config into SQLite.
// It only migrates if the api_keys table is empty AND the config has entries.
// After migration, it backs up config.yaml and re-saves it without the API key
// fields so the YAML file stays clean.
func MigrateAPIKeysFromConfig(cfg *config.Config, configFilePath string) int {
	db := getDB()
	if db == nil || cfg == nil {
		return 0
	}

	var count int64
	if err := db.QueryRow("SELECT COUNT(*) FROM api_keys").Scan(&count); err != nil {
		log.Errorf("usage: migration count api_keys: %v", err)
		return 0
	}
	if count > 0 {
		cfg.APIKeys = nil
		cfg.APIKeyEntries = nil
		if configFilePath != "" {
			cleanAPIKeysFromYAML(configFilePath)
		}
		return 0
	}

	seen := make(map[string]struct{})
	rows := make([]APIKeyRow, 0)
	now := time.Now().UTC().Format(time.RFC3339)

	for _, entry := range cfg.APIKeyEntries {
		trimmed := strings.TrimSpace(entry.Key)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		row := APIKeyRowFromConfig(entry)
		row.Key = trimmed
		row.Name = strings.TrimSpace(row.Name)
		if row.Name == "" {
			row.Name = sqlapikey.DefaultAPIKeyName(len(rows))
		}
		if row.CreatedAt == "" {
			row.CreatedAt = now
		}
		row.UpdatedAt = now
		rows = append(rows, row)
	}

	for _, key := range cfg.APIKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		rows = append(rows, APIKeyRow{
			Key:       trimmed,
			Name:      sqlapikey.DefaultAPIKeyName(len(rows)),
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	if len(rows) == 0 {
		return 0
	}

	if err := apiKeyStore().ReplaceAll(rows); err != nil {
		log.Errorf("usage: migrate api_keys into SQLite: %v", err)
		return 0
	}

	log.Infof("usage: migrated %d API keys from config to SQLite", len(rows))

	cfg.APIKeys = nil
	cfg.APIKeyEntries = nil

	if configFilePath != "" {
		if backupConfigForMigration(configFilePath, apiKeysMigrationBackupSuffix) {
			cleanAPIKeysFromYAML(configFilePath)
		}
	}

	return len(rows)
}

// EffectiveAPIKeyRow applies the currently linked permission profile to an API key row.
// If the profile is missing, the row's copied/custom settings remain the fallback.
func EffectiveAPIKeyRow(row APIKeyRow) APIKeyRow {
	return EffectiveAPIKeyRowWithProfiles(row, ListAPIKeyPermissionProfiles())
}

// EffectiveAPIKeyRowWithProfiles applies a preloaded permission profile snapshot.
func EffectiveAPIKeyRowWithProfiles(row APIKeyRow, profiles []APIKeyPermissionProfileRow) APIKeyRow {
	return sqlapikey.EffectiveAPIKeyRowWithProfiles(row, toPermissionProfileSnapshots(profiles))
}

// EffectiveAPIKeyRows applies the current permission profile snapshot to each row.
func EffectiveAPIKeyRows(rows []APIKeyRow) []APIKeyRow {
	if len(rows) == 0 {
		return rows
	}
	return sqlapikey.EffectiveAPIKeyRowsWithProfiles(rows, toPermissionProfileSnapshots(ListAPIKeyPermissionProfiles()))
}

// ListAPIKeys retrieves all API key entries from SQLite.
func ListAPIKeys() []APIKeyRow {
	return apiKeyStore().List()
}

// GetAPIKey retrieves a single API key entry by key string.
func GetAPIKey(key string) *APIKeyRow {
	return apiKeyStore().Get(key)
}

// UpsertAPIKey inserts or updates an API key entry.
func UpsertAPIKey(entry APIKeyRow) error {
	return apiKeyStore().Upsert(entry)
}

// DeleteAPIKey removes an API key entry by key string.
func DeleteAPIKey(key string) error {
	return apiKeyStore().Delete(key)
}

// ReplaceAllAPIKeys atomically replaces all API keys with the given list.
func ReplaceAllAPIKeys(entries []APIKeyRow) error {
	return apiKeyStore().ReplaceAll(entries)
}

func toPermissionProfileSnapshots(profiles []APIKeyPermissionProfileRow) []sqlapikey.PermissionProfileSnapshot {
	if len(profiles) == 0 {
		return nil
	}

	snapshots := make([]sqlapikey.PermissionProfileSnapshot, 0, len(profiles))
	for _, profile := range profiles {
		snapshots = append(snapshots, sqlapikey.PermissionProfileSnapshot{
			ID:                   profile.ID,
			DailyLimit:           profile.DailyLimit,
			TotalQuota:           profile.TotalQuota,
			ConcurrencyLimit:     profile.ConcurrencyLimit,
			RPMLimit:             profile.RPMLimit,
			TPMLimit:             profile.TPMLimit,
			AllowedModels:        append([]string(nil), profile.AllowedModels...),
			AllowedChannels:      append([]string(nil), profile.AllowedChannels...),
			AllowedChannelGroups: append([]string(nil), profile.AllowedChannelGroups...),
			SystemPrompt:         profile.SystemPrompt,
		})
	}
	return snapshots
}
