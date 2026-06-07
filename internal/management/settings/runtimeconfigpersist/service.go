package runtimeconfigpersist

import (
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func SaveConfig(cfg *config.Config, configFilePath string) error {
	if usage.ConfigStoreAvailable() {
		usage.PersistRuntimeSettingsFromConfig(cfg)
	}
	if err := config.SaveConfigPreserveComments(configFilePath, cfg); err != nil {
		return err
	}
	if usage.ConfigStoreAvailable() {
		usage.CleanDBBackedConfigFromYAML(configFilePath)
	}
	return nil
}

func PersistRuntimeSetting(cfg *config.Config, configFilePath string, key string, value any) error {
	if !usage.ConfigStoreAvailable() {
		return SaveConfig(cfg, configFilePath)
	}
	if err := usage.UpsertRuntimeSetting(key, value); err != nil {
		return err
	}
	if strings.TrimSpace(configFilePath) != "" {
		usage.CleanDBBackedConfigFromYAML(configFilePath)
	}
	return nil
}

func SyncReloadedConfigAfterYAMLSave(cfg *config.Config, configFilePath string, yamlContent []byte) {
	if cfg == nil || !usage.ConfigStoreAvailable() {
		return
	}
	usage.PersistRuntimeSettingsPresentInYAML(cfg, yamlContent)
	usage.MigrateRuntimeSettingsFromConfig(cfg, configFilePath)
	usage.ApplyStoredRuntimeSettings(cfg)
}
