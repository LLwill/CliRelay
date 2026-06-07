package management

import (
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	apikeysettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/apikey"
	oauthsettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/oauth"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func (h *Handler) renameChannelReferences(oldNames []string, newName string) error {
	newName = strings.TrimSpace(newName)
	oldNameSet := channelRenameSet(oldNames, newName)
	if h == nil || newName == "" || len(oldNameSet) == 0 {
		return nil
	}

	configChanged := false
	routingChanged := false
	if h.cfg != nil {
		if renameRoutingChannelReferences(&h.cfg.Routing, oldNameSet, newName) {
			configChanged = true
			routingChanged = true
		}
		if renameConfigAPIKeyChannels(h.cfg.APIKeyEntries, oldNameSet, newName) {
			configChanged = true
		}
		if renameOAuthModelAliasChannels(h.cfg, oldNameSet, newName) {
			configChanged = true
			if err := h.storeRuntimeSetting(usage.RuntimeSettingOAuthModelAlias, h.cfg.OAuthModelAlias); err != nil {
				return fmt.Errorf("failed to persist oauth model aliases: %w", err)
			}
		}
	}

	if routingChanged && h.cfg != nil {
		if err := usage.UpsertRoutingConfig(h.cfg.Routing); err != nil {
			return fmt.Errorf("failed to persist routing config: %w", err)
		}
	}
	if err := renameSQLiteAPIKeyChannels(oldNameSet, newName); err != nil {
		return err
	}
	if err := renameSQLiteAPIKeyPermissionProfileChannels(oldNameSet, newName); err != nil {
		return err
	}
	if configChanged && h.cfg != nil && strings.TrimSpace(h.configFilePath) != "" {
		if err := h.saveConfigFile(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}
	if configChanged && h.authManager != nil {
		h.authManager.SetConfig(h.cfg)
	}
	return nil
}

func (h *Handler) removeChannelReferences(oldNames []string) error {
	oldNameSet := channelRenameSet(oldNames, "")
	if h == nil || len(oldNameSet) == 0 {
		return nil
	}

	configChanged := false
	routingChanged := false
	if h.cfg != nil {
		if removeRoutingChannelReferences(&h.cfg.Routing, oldNameSet) {
			configChanged = true
			routingChanged = true
		}
		if removeConfigAPIKeyChannels(h.cfg.APIKeyEntries, oldNameSet) {
			configChanged = true
		}
		if removeOAuthModelAliasChannels(h.cfg, oldNameSet) {
			configChanged = true
			if err := h.storeRuntimeSetting(usage.RuntimeSettingOAuthModelAlias, h.cfg.OAuthModelAlias); err != nil {
				return fmt.Errorf("failed to persist oauth model aliases: %w", err)
			}
		}
	}

	if routingChanged && h.cfg != nil {
		if err := usage.UpsertRoutingConfig(h.cfg.Routing); err != nil {
			return fmt.Errorf("failed to persist routing config: %w", err)
		}
	}
	if err := removeSQLiteAPIKeyChannels(oldNameSet); err != nil {
		return err
	}
	if err := removeSQLiteAPIKeyPermissionProfileChannels(oldNameSet); err != nil {
		return err
	}
	if configChanged && h.cfg != nil && strings.TrimSpace(h.configFilePath) != "" {
		if err := h.saveConfigFile(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
	}
	if configChanged && h.authManager != nil {
		h.authManager.SetConfig(h.cfg)
	}
	return nil
}

func channelRenameSet(oldNames []string, newName string) map[string]struct{} {
	newKey := strings.ToLower(strings.TrimSpace(newName))
	oldNameSet := make(map[string]struct{}, len(oldNames))
	for _, oldName := range oldNames {
		oldKey := strings.ToLower(strings.TrimSpace(oldName))
		if oldKey == "" || oldKey == newKey {
			continue
		}
		oldNameSet[oldKey] = struct{}{}
	}
	return oldNameSet
}

func shouldRenameChannel(value string, oldNameSet map[string]struct{}) bool {
	_, exists := oldNameSet[strings.ToLower(strings.TrimSpace(value))]
	return exists
}

func renameChannelList(values []string, oldNameSet map[string]struct{}, newName string) ([]string, bool) {
	if len(values) == 0 {
		return values, false
	}
	changed := false
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if shouldRenameChannel(trimmed, oldNameSet) {
			trimmed = newName
			changed = true
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			changed = true
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		out = nil
	}
	return out, changed
}

func removeChannelList(values []string, oldNameSet map[string]struct{}) ([]string, bool) {
	if len(values) == 0 {
		return values, false
	}
	changed := false
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if shouldRenameChannel(trimmed, oldNameSet) {
			changed = true
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			changed = true
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		out = nil
	}
	return out, changed
}

func renameRoutingChannelReferences(routing *config.RoutingConfig, oldNameSet map[string]struct{}, newName string) bool {
	if routing == nil || len(routing.ChannelGroups) == 0 {
		return false
	}
	changed := false
	for i := range routing.ChannelGroups {
		channels, channelsChanged := renameChannelList(routing.ChannelGroups[i].Match.Channels, oldNameSet, newName)
		if channelsChanged {
			routing.ChannelGroups[i].Match.Channels = channels
			changed = true
		}
		if priorities := routing.ChannelGroups[i].ChannelPriorities; len(priorities) > 0 {
			for channel, priority := range priorities {
				if !shouldRenameChannel(channel, oldNameSet) {
					continue
				}
				delete(priorities, channel)
				if existing, exists := priorities[newName]; !exists || priority > existing {
					priorities[newName] = priority
				}
				changed = true
			}
			if len(priorities) == 0 {
				routing.ChannelGroups[i].ChannelPriorities = nil
			}
		}
	}
	return changed
}

func removeRoutingChannelReferences(routing *config.RoutingConfig, oldNameSet map[string]struct{}) bool {
	if routing == nil || len(routing.ChannelGroups) == 0 {
		return false
	}
	changed := false
	for i := range routing.ChannelGroups {
		channels, channelsChanged := removeChannelList(routing.ChannelGroups[i].Match.Channels, oldNameSet)
		if channelsChanged {
			routing.ChannelGroups[i].Match.Channels = channels
			changed = true
		}
		if priorities := routing.ChannelGroups[i].ChannelPriorities; len(priorities) > 0 {
			for channel := range priorities {
				if !shouldRenameChannel(channel, oldNameSet) {
					continue
				}
				delete(priorities, channel)
				changed = true
			}
			if len(priorities) == 0 {
				routing.ChannelGroups[i].ChannelPriorities = nil
			}
		}
	}
	return changed
}

func renameConfigAPIKeyChannels(entries []config.APIKeyEntry, oldNameSet map[string]struct{}, newName string) bool {
	changed := false
	for i := range entries {
		channels, channelsChanged := renameChannelList(entries[i].AllowedChannels, oldNameSet, newName)
		if channelsChanged {
			entries[i].AllowedChannels = channels
			changed = true
		}
	}
	return changed
}

func removeConfigAPIKeyChannels(entries []config.APIKeyEntry, oldNameSet map[string]struct{}) bool {
	changed := false
	for i := range entries {
		channels, channelsChanged := removeChannelList(entries[i].AllowedChannels, oldNameSet)
		if channelsChanged {
			entries[i].AllowedChannels = channels
			changed = true
		}
	}
	return changed
}

func renameSQLiteAPIKeyChannels(oldNameSet map[string]struct{}, newName string) error {
	svc := apikeysettings.NewService(nil)
	if err := svc.RenameAllowedChannelRestrictions(oldNameSet, newName); err != nil {
		return fmt.Errorf("failed to persist api key channel restrictions: %w", err)
	}
	return nil
}

func removeSQLiteAPIKeyChannels(oldNameSet map[string]struct{}) error {
	svc := apikeysettings.NewService(nil)
	if err := svc.RemoveAllowedChannelRestrictions(oldNameSet); err != nil {
		return fmt.Errorf("failed to persist api key channel restrictions: %w", err)
	}
	return nil
}

func renameSQLiteAPIKeyPermissionProfileChannels(oldNameSet map[string]struct{}, newName string) error {
	svc := apikeysettings.NewService(nil)
	if err := svc.RenamePermissionProfileChannelRestrictions(oldNameSet, newName); err != nil {
		return fmt.Errorf("failed to persist api key permission profile channel restrictions: %w", err)
	}
	return nil
}

func removeSQLiteAPIKeyPermissionProfileChannels(oldNameSet map[string]struct{}) error {
	svc := apikeysettings.NewService(nil)
	if err := svc.RemovePermissionProfileChannelRestrictions(oldNameSet); err != nil {
		return fmt.Errorf("failed to persist api key permission profile channel restrictions: %w", err)
	}
	return nil
}

func renameOAuthModelAliasChannels(cfg *config.Config, oldNameSet map[string]struct{}, newName string) bool {
	if cfg == nil || len(cfg.OAuthModelAlias) == 0 {
		return false
	}
	newKey := strings.ToLower(strings.TrimSpace(newName))
	changed := false
	for channel, aliases := range cfg.OAuthModelAlias {
		if !shouldRenameChannel(channel, oldNameSet) {
			continue
		}
		delete(cfg.OAuthModelAlias, channel)
		cfg.OAuthModelAlias[newKey] = append(cfg.OAuthModelAlias[newKey], aliases...)
		changed = true
	}
	if changed {
		cfg.OAuthModelAlias = oauthsettings.NormalizeModelAlias(cfg.OAuthModelAlias)
	}
	return changed
}

func removeOAuthModelAliasChannels(cfg *config.Config, oldNameSet map[string]struct{}) bool {
	if cfg == nil || len(cfg.OAuthModelAlias) == 0 {
		return false
	}
	changed := false
	for channel := range cfg.OAuthModelAlias {
		if !shouldRenameChannel(channel, oldNameSet) {
			continue
		}
		delete(cfg.OAuthModelAlias, channel)
		changed = true
	}
	if changed {
		cfg.OAuthModelAlias = oauthsettings.NormalizeModelAlias(cfg.OAuthModelAlias)
	}
	return changed
}
