package usage

import (
	"database/sql"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sqlproxypool "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/sqlite/proxypool"
)

func initProxyPoolTable(db *sql.DB) {
	sqlproxypool.InitTable(db)
}

func proxyPoolStore() sqlproxypool.Store {
	return sqlproxypool.NewStore(getDB())
}

// ProxyPoolStoreAvailable reports whether the SQLite store is ready for proxy-pool operations.
func ProxyPoolStoreAvailable() bool {
	return proxyPoolStore().Available()
}

// ListProxyPool retrieves all reusable proxies from SQLite.
func ListProxyPool() []config.ProxyPoolEntry {
	return proxyPoolStore().List()
}

// GetProxyPoolEntry retrieves one reusable proxy by ID.
func GetProxyPoolEntry(id string) *config.ProxyPoolEntry {
	return proxyPoolStore().Get(id)
}

// ReplaceProxyPool atomically replaces all SQLite proxy entries after normalization.
func ReplaceProxyPool(entries []config.ProxyPoolEntry) error {
	return proxyPoolStore().Replace(entries)
}

// ApplyStoredProxyPool overlays the DB-backed proxy pool onto the runtime config.
func ApplyStoredProxyPool(cfg *config.Config) bool {
	if cfg == nil || !ProxyPoolStoreAvailable() {
		return false
	}
	return proxyPoolStore().ApplyToConfig(cfg)
}

// MigrateProxyPoolFromConfig moves legacy YAML proxy-pool entries into SQLite.
func MigrateProxyPoolFromConfig(cfg *config.Config, configFilePath string) int {
	if cfg == nil || !ProxyPoolStoreAvailable() {
		return 0
	}
	migrated, hadStored, shouldClean := proxyPoolStore().MigrateFromConfig(cfg)
	if hadStored {
		cleanProxyPoolFromYAML(configFilePath)
		return 0
	}
	if shouldClean && configFilePath != "" {
		if backupConfigForMigration(configFilePath, proxyPoolMigrationBackupSuffix) {
			cleanProxyPoolFromYAML(configFilePath)
		}
	}
	return migrated
}
