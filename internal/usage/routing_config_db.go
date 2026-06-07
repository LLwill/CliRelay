package usage

import (
	"database/sql"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sqlrouting "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/sqlite/routing"
)

func initRoutingConfigTable(db *sql.DB) {
	sqlrouting.InitTable(db)
}

func routingConfigStore() sqlrouting.Store {
	return sqlrouting.NewStore(getDB())
}

func ApplyStoredRoutingConfig(cfg *config.Config) bool {
	if cfg == nil || !ConfigStoreAvailable() {
		return false
	}
	return routingConfigStore().ApplyToConfig(cfg)
}

func MigrateRoutingConfigFromConfig(cfg *config.Config, configFilePath string) bool {
	if cfg == nil || !ConfigStoreAvailable() {
		return false
	}
	migrated, hadStored := routingConfigStore().MigrateFromConfig(cfg)
	if hadStored {
		cleanRoutingConfigFromYAML(configFilePath)
		return false
	}
	if !migrated {
		return false
	}
	if strings.TrimSpace(configFilePath) != "" {
		if backupConfigForMigration(configFilePath, routingMigrationBackupSuffix) {
			cleanRoutingConfigFromYAML(configFilePath)
		}
	}
	return true
}

func GetRoutingConfig() *config.RoutingConfig {
	return routingConfigStore().Get()
}

func UpsertRoutingConfig(cfg config.RoutingConfig) error {
	return routingConfigStore().Upsert(cfg)
}
