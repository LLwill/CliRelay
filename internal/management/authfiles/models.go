package authfiles

import (
	"context"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	sdkmodelcatalog "github.com/router-for-me/CLIProxyAPI/v6/sdk/modelcatalog"
)

type ModelSource interface {
	GetModelsForClient(clientID string) []*registry.ModelInfo
}

type ModelRegistrar interface {
	RegisterClient(clientID, clientProvider string, models []*registry.ModelInfo)
}

func ModelLookupAuthID(manager *coreauth.Manager, name string) string {
	return ModelLookupAuthIDForTenant(manager, "", name)
}

func ModelLookupAuthIDForTenant(manager *coreauth.Manager, tenantID, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if manager != nil {
		for _, auth := range manager.ListForTenant(NormalizeTenantID(tenantID)) {
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

// FindAuthForTenant resolves an auth by file name or ID within a tenant.
func FindAuthForTenant(manager *coreauth.Manager, tenantID, name string) *coreauth.Auth {
	name = strings.TrimSpace(name)
	if name == "" || manager == nil {
		return nil
	}
	for _, auth := range manager.ListForTenant(NormalizeTenantID(tenantID)) {
		if auth == nil {
			continue
		}
		if auth.FileName == name || auth.ID == name {
			return auth
		}
	}
	return nil
}

func ListModelEntries(manager *coreauth.Manager, source ModelSource, name string) []map[string]any {
	return ListModelEntriesForTenant(manager, source, "", name)
}

func ListModelEntriesForTenant(manager *coreauth.Manager, source ModelSource, tenantID, name string) []map[string]any {
	if source == nil {
		return nil
	}
	authID := ModelLookupAuthIDForTenant(manager, tenantID, name)
	models := source.GetModelsForClient(authID)
	return modelEntriesFromRegistry(models)
}

// ListModelEntriesLiveForTenant optionally re-fetches models from the upstream
// provider for claude/codex (and other live-capable providers), updates the
// registry when successful, then returns the public model payload.
//
// When live fetch fails, falls back to the existing registry list so the UI
// still shows known models.
func ListModelEntriesLiveForTenant(
	ctx context.Context,
	manager *coreauth.Manager,
	source ModelSource,
	registrar ModelRegistrar,
	cfg *config.Config,
	tenantID, name string,
	refresh bool,
) (models []map[string]any, sourceLabel string) {
	sourceLabel = "registry"
	if !refresh {
		return ListModelEntriesForTenant(manager, source, tenantID, name), sourceLabel
	}

	auth := FindAuthForTenant(manager, tenantID, name)
	if auth == nil {
		return ListModelEntriesForTenant(manager, source, tenantID, name), sourceLabel
	}

	live, provider := fetchLiveModelsForAuth(ctx, auth, cfg)
	if len(live) == 0 {
		return ListModelEntriesForTenant(manager, source, tenantID, name), sourceLabel
	}

	sourceLabel = "upstream"
	if registrar != nil {
		providerKey := provider
		if providerKey == "" {
			providerKey = strings.ToLower(strings.TrimSpace(auth.Provider))
		}
		registrar.RegisterClient(auth.ID, providerKey, live)
	}
	return modelEntriesFromRegistry(live), sourceLabel
}

func fetchLiveModelsForAuth(ctx context.Context, auth *coreauth.Auth, cfg *config.Config) ([]*registry.ModelInfo, string) {
	if auth == nil {
		return nil, ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()

	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	var sdkModels []*sdkmodelcatalog.ModelInfo
	switch provider {
	case "claude":
		sdkModels = executor.FetchClaudeModels(fetchCtx, auth, cfg)
	case "codex":
		sdkModels = executor.FetchCodexModels(fetchCtx, auth, cfg)
	case "xai":
		sdkModels = executor.FetchXAIModels(fetchCtx, auth, cfg)
	case "antigravity":
		sdkModels = executor.FetchAntigravityModels(fetchCtx, auth, cfg)
	default:
		return nil, provider
	}
	return cloneSDKModelsToRegistry(sdkModels), provider
}

func cloneSDKModelsToRegistry(models []*sdkmodelcatalog.ModelInfo) []*registry.ModelInfo {
	if len(models) == 0 {
		return nil
	}
	out := make([]*registry.ModelInfo, 0, len(models))
	for _, model := range models {
		if model == nil || strings.TrimSpace(model.ID) == "" {
			continue
		}
		out = append(out, &registry.ModelInfo{
			ID:                  model.ID,
			Object:              model.Object,
			Created:             model.Created,
			OwnedBy:             model.OwnedBy,
			Type:                model.Type,
			DisplayName:         model.DisplayName,
			UpstreamModelID:     model.UpstreamModelID,
			Name:                model.Name,
			Version:             model.Version,
			Description:         model.Description,
			InputTokenLimit:     model.InputTokenLimit,
			OutputTokenLimit:    model.OutputTokenLimit,
			ContextLength:       model.ContextLength,
			MaxCompletionTokens: model.MaxCompletionTokens,
			UserDefined:         model.UserDefined,
		})
	}
	return out
}

func modelEntriesFromRegistry(models []*registry.ModelInfo) []map[string]any {
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
