package apikey

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const createAPIKeyPermissionProfilesTableSQL = `
CREATE TABLE IF NOT EXISTS api_key_permission_profiles (
  id                     TEXT PRIMARY KEY NOT NULL,
  name                   TEXT NOT NULL DEFAULT '',
  daily_limit            INTEGER NOT NULL DEFAULT 0,
  total_quota            INTEGER NOT NULL DEFAULT 0,
  concurrency_limit      INTEGER NOT NULL DEFAULT 0,
  rpm_limit              INTEGER NOT NULL DEFAULT 0,
  tpm_limit              INTEGER NOT NULL DEFAULT 0,
  allowed_models         TEXT NOT NULL DEFAULT '[]',
  allowed_channels       TEXT NOT NULL DEFAULT '[]',
  allowed_channel_groups TEXT NOT NULL DEFAULT '[]',
  system_prompt          TEXT NOT NULL DEFAULT '',
  created_at             TEXT NOT NULL DEFAULT '',
  updated_at             TEXT NOT NULL DEFAULT ''
);
`

type PermissionProfileRow struct {
	ID                   string   `json:"id" yaml:"id"`
	Name                 string   `json:"name" yaml:"name"`
	DailyLimit           int      `json:"daily-limit" yaml:"daily-limit"`
	TotalQuota           int      `json:"total-quota" yaml:"total-quota"`
	ConcurrencyLimit     int      `json:"concurrency-limit" yaml:"concurrency-limit"`
	RPMLimit             int      `json:"rpm-limit" yaml:"rpm-limit"`
	TPMLimit             int      `json:"tpm-limit" yaml:"tpm-limit"`
	AllowedModels        []string `json:"allowed-models" yaml:"allowed-models"`
	AllowedChannels      []string `json:"allowed-channels" yaml:"allowed-channels"`
	AllowedChannelGroups []string `json:"allowed-channel-groups" yaml:"allowed-channel-groups"`
	SystemPrompt         string   `json:"system-prompt" yaml:"system-prompt"`
	CreatedAt            string   `json:"created-at,omitempty" yaml:"created-at,omitempty"`
	UpdatedAt            string   `json:"updated-at,omitempty" yaml:"updated-at,omitempty"`
}

func InitPermissionProfilesTable(db *sql.DB) {
	if db == nil {
		return
	}
	if _, err := db.Exec(createAPIKeyPermissionProfilesTableSQL); err != nil {
		log.Errorf("sqlite/apikey: create api_key_permission_profiles table: %v", err)
	}
}

func (s Store) ListPermissionProfiles() []PermissionProfileRow {
	if s.db == nil {
		return nil
	}

	rows, err := s.db.Query(`SELECT id, name, daily_limit, total_quota, concurrency_limit,
		rpm_limit, tpm_limit, allowed_models, allowed_channels, allowed_channel_groups,
		system_prompt, created_at, updated_at
		FROM api_key_permission_profiles ORDER BY created_at ASC, id ASC`)
	if err != nil {
		log.Errorf("sqlite/apikey: list api_key_permission_profiles: %v", err)
		return nil
	}
	defer rows.Close()

	result := make([]PermissionProfileRow, 0)
	for rows.Next() {
		profile, ok := scanPermissionProfileRow(rows)
		if ok {
			result = append(result, *profile)
		}
	}
	if err := rows.Err(); err != nil {
		log.Warnf("sqlite/apikey: scan api_key_permission_profiles rows: %v", err)
	}
	return result
}

func (s Store) ReplaceAllPermissionProfiles(profiles []PermissionProfileRow) error {
	if s.db == nil {
		return fmt.Errorf("database not initialised")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM api_key_permission_profiles"); err != nil {
		_ = tx.Rollback()
		return err
	}

	stmt, err := tx.Prepare(`INSERT INTO api_key_permission_profiles
		(id, name, daily_limit, total_quota, concurrency_limit, rpm_limit, tpm_limit,
		 allowed_models, allowed_channels, allowed_channel_groups, system_prompt, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		profile = normalizePermissionProfile(profile)
		if profile.ID == "" {
			_ = tx.Rollback()
			return fmt.Errorf("id is required")
		}
		if profile.Name == "" {
			_ = tx.Rollback()
			return fmt.Errorf("name is required")
		}
		if _, exists := seen[profile.ID]; exists {
			_ = tx.Rollback()
			return fmt.Errorf("duplicate id %q", profile.ID)
		}
		seen[profile.ID] = struct{}{}
		if profile.CreatedAt == "" {
			profile.CreatedAt = now
		}
		profile.UpdatedAt = now

		if _, err := stmt.Exec(
			profile.ID, profile.Name, profile.DailyLimit, profile.TotalQuota,
			profile.ConcurrencyLimit, profile.RPMLimit, profile.TPMLimit,
			mustJSONStringList(profile.AllowedModels), mustJSONStringList(profile.AllowedChannels),
			mustJSONStringList(profile.AllowedChannelGroups), profile.SystemPrompt,
			profile.CreatedAt, profile.UpdatedAt,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

func normalizePermissionProfile(profile PermissionProfileRow) PermissionProfileRow {
	profile.ID = strings.TrimSpace(profile.ID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.DailyLimit = normalizeNonNegativeInt(profile.DailyLimit)
	profile.TotalQuota = normalizeNonNegativeInt(profile.TotalQuota)
	profile.ConcurrencyLimit = normalizeNonNegativeInt(profile.ConcurrencyLimit)
	profile.RPMLimit = normalizeNonNegativeInt(profile.RPMLimit)
	profile.TPMLimit = normalizeNonNegativeInt(profile.TPMLimit)
	profile.AllowedModels = normalizeStringSlice(profile.AllowedModels)
	profile.AllowedChannels = normalizeStringSlice(profile.AllowedChannels)
	profile.AllowedChannelGroups = normalizeStringSlice(profile.AllowedChannelGroups)
	profile.SystemPrompt = strings.TrimSpace(profile.SystemPrompt)
	return profile
}

func scanPermissionProfileRow(row scanner) (*PermissionProfileRow, bool) {
	var profile PermissionProfileRow
	var modelsJSON string
	var channelsJSON string
	var channelGroupsJSON string
	if err := row.Scan(
		&profile.ID, &profile.Name, &profile.DailyLimit, &profile.TotalQuota, &profile.ConcurrencyLimit,
		&profile.RPMLimit, &profile.TPMLimit, &modelsJSON, &channelsJSON, &channelGroupsJSON,
		&profile.SystemPrompt, &profile.CreatedAt, &profile.UpdatedAt,
	); err != nil {
		if err != sql.ErrNoRows {
			log.Warnf("sqlite/apikey: scan api_key_permission_profiles row: %v", err)
		}
		return nil, false
	}
	profile.AllowedModels = decodeJSONStringList(modelsJSON)
	profile.AllowedChannels = decodeJSONStringList(channelsJSON)
	profile.AllowedChannelGroups = decodeJSONStringList(channelGroupsJSON)
	return &profile, true
}

func normalizeNonNegativeInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if result == nil {
		return []string{}
	}
	return result
}

func decodeJSONStringList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	return normalizeStringSlice(values)
}
