package authfiles

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type ModelSource interface {
	GetModelsForClient(clientID string) []*registry.ModelInfo
}

func ModelLookupAuthID(manager *coreauth.Manager, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if manager != nil {
		for _, auth := range manager.List() {
			if auth == nil {
				continue
			}
			if auth.FileName == name || auth.ID == name {
				return auth.ID
			}
		}
	}
	return name
}

func ListModelEntries(manager *coreauth.Manager, source ModelSource, name string) []map[string]any {
	if source == nil {
		return nil
	}
	authID := ModelLookupAuthID(manager, name)
	models := source.GetModelsForClient(authID)
	result := make([]map[string]any, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		entry := map[string]any{
			"id": model.ID,
		}
		if model.DisplayName != "" {
			entry["display_name"] = model.DisplayName
		}
		if model.Type != "" {
			entry["type"] = model.Type
		}
		if model.OwnedBy != "" {
			entry["owned_by"] = model.OwnedBy
		}
		result = append(result, entry)
	}
	return result
}
