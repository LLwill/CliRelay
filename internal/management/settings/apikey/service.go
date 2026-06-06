package apikey

import (
	"errors"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

var (
	ErrInvalidProfileID   = errors.New("id is required")
	ErrInvalidProfileName = errors.New("name is required")
	ErrMissingValue       = errors.New("missing value")
)

type ChannelSanitizer func([]string) ([]string, error)

type Service struct {
	sanitizeChannels ChannelSanitizer
}

func NewService(sanitizeChannels ChannelSanitizer) *Service {
	return &Service{sanitizeChannels: sanitizeChannels}
}

func (s *Service) EnabledKeys() []string {
	rows := usage.ListAPIKeys()
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		if !row.Disabled {
			keys = append(keys, row.Key)
		}
	}
	return keys
}

func (s *Service) ReplaceKeys(keys []string) error {
	rows := make([]usage.APIKeyRow, 0, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			rows = append(rows, usage.APIKeyRow{Key: trimmed})
		}
	}
	return usage.ReplaceAllAPIKeys(rows)
}

func (s *Service) PatchKey(oldKey string, newKey string) error {
	oldKey = strings.TrimSpace(oldKey)
	newKey = strings.TrimSpace(newKey)
	if oldKey != "" {
		_ = usage.DeleteAPIKey(oldKey)
	}
	if newKey == "" {
		return nil
	}
	return usage.UpsertAPIKey(usage.APIKeyRow{Key: newKey})
}

func (s *Service) DeleteKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrMissingValue
	}
	return usage.DeleteAPIKey(key)
}

func (s *Service) PermissionProfiles() []usage.APIKeyPermissionProfileRow {
	return usage.ListAPIKeyPermissionProfiles()
}

func (s *Service) ReplacePermissionProfiles(profiles []usage.APIKeyPermissionProfileRow) error {
	normalized := make([]usage.APIKeyPermissionProfileRow, len(profiles))
	copy(normalized, profiles)
	for idx := range normalized {
		normalized[idx].ID = strings.TrimSpace(normalized[idx].ID)
		normalized[idx].Name = strings.TrimSpace(normalized[idx].Name)
		if normalized[idx].ID == "" {
			return ErrInvalidProfileID
		}
		if normalized[idx].Name == "" {
			return ErrInvalidProfileName
		}
		if s != nil && s.sanitizeChannels != nil {
			cleaned, err := s.sanitizeChannels(normalized[idx].AllowedChannels)
			if err != nil {
				return err
			}
			normalized[idx].AllowedChannels = cleaned
		}
	}
	return usage.ReplaceAllAPIKeyPermissionProfiles(normalized)
}
