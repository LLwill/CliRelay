package auth

import (
	"strings"

	sdkmodelcatalog "github.com/router-for-me/CLIProxyAPI/v6/sdk/modelcatalog"
)

type ModelRegistry interface {
	ClearModelQuotaExceeded(clientID, modelID string)
	SetModelQuotaExceeded(clientID, modelID string)
	SuspendClientModel(clientID, modelID string, reason string)
	ResumeClientModel(clientID, modelID string)
	ClientSupportsModel(clientID, modelID string) bool
	GetModelsForClient(clientID string) []*sdkmodelcatalog.ModelInfo
}

func (m *Manager) resumeRecoveredQuotaModels(authID string, models []string) {
	if len(models) == 0 || strings.TrimSpace(authID) == "" {
		return
	}
	registryRef := m.modelRegistry
	if registryRef == nil {
		return
	}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		registryRef.ClearModelQuotaExceeded(authID, model)
		registryRef.ResumeClientModel(authID, model)
	}
}

// KnownChannelGroups returns the currently known explicit and implicit channel groups.
func (m *Manager) KnownChannelGroups() map[string]struct{} {
	out := make(map[string]struct{})
	if m == nil {
		return out
	}
	cfg := m.currentRuntimeConfig()
	if cfg != nil {
		for i := range cfg.Routing.ChannelGroups {
			if name := normalizeGroupName(cfg.Routing.ChannelGroups[i].Name); name != "" {
				out[name] = struct{}{}
			}
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, auth := range m.auths {
		for group := range authGroups(cfg, auth) {
			out[group] = struct{}{}
		}
	}
	return out
}

// CanServeModelWithScopes reports whether at least one active auth can serve the model under the given restrictions.
func (m *Manager) CanServeModelWithScopes(modelID string, allowedChannels, allowedGroups map[string]struct{}, routeGroup string) bool {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || m == nil {
		return false
	}
	registryRef := m.modelRegistry
	cfg := m.currentRuntimeConfig()
	selectionRouteGroup := effectiveRouteGroupForSelection(cfg, routeGroup, allowedGroups, modelID)

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, candidate := range m.auths {
		if candidate == nil || candidate.Disabled || candidate.Status == StatusDisabled {
			continue
		}
		if !authAllowedByChannels(candidate, allowedChannels) {
			continue
		}
		if !authAllowedByGroups(cfg, candidate, allowedGroups) {
			continue
		}
		if !authInRouteGroup(cfg, candidate, selectionRouteGroup) {
			continue
		}
		if candidateSupportsModel(cfg, registryRef, candidate, modelID, selectionRouteGroup, allowedGroups) {
			return true
		}
	}
	return false
}
