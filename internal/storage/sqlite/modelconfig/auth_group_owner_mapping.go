package modelconfig

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

type AuthGroupOwnerMappingRow struct {
	AuthGroup string `json:"auth_group"`
	Owner     string `json:"owner"`
	UpdatedAt string `json:"updated_at"`
}

var (
	authGroupOwnerMappingCache   map[string]AuthGroupOwnerMappingRow
	authGroupOwnerMappingCacheMu sync.RWMutex
)

func NormalizeAuthGroupKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), "-"))
}

func (s Store) ListAuthGroupOwnerMappings() []AuthGroupOwnerMappingRow {
	authGroupOwnerMappingCacheMu.RLock()
	defer authGroupOwnerMappingCacheMu.RUnlock()

	result := make([]AuthGroupOwnerMappingRow, 0, len(authGroupOwnerMappingCache))
	for _, row := range authGroupOwnerMappingCache {
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].AuthGroup) < strings.ToLower(result[j].AuthGroup)
	})
	return result
}

func (s Store) GetAuthGroupOwnerMapping(authGroup string) (AuthGroupOwnerMappingRow, bool) {
	authGroupOwnerMappingCacheMu.RLock()
	defer authGroupOwnerMappingCacheMu.RUnlock()

	row, ok := authGroupOwnerMappingCache[NormalizeAuthGroupKey(authGroup)]
	return row, ok
}

func (s Store) UpsertAuthGroupOwnerMapping(row AuthGroupOwnerMappingRow) error {
	if s.db == nil {
		return fmt.Errorf("database not initialised")
	}

	row.AuthGroup = NormalizeAuthGroupKey(row.AuthGroup)
	row.Owner = NormalizeModelOwnerValue(row.Owner)
	if row.AuthGroup == "" {
		return fmt.Errorf("auth group is required")
	}
	if row.Owner == "" {
		return fmt.Errorf("owner is required")
	}
	row.UpdatedAt = nowRFC3339()

	_, err := s.db.Exec(
		`INSERT INTO auth_group_model_owner_mappings (auth_group, owner, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(auth_group) DO UPDATE SET
		   owner = excluded.owner,
		   updated_at = excluded.updated_at`,
		row.AuthGroup,
		row.Owner,
		row.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert auth group owner mapping: %w", err)
	}

	reloadAuthGroupOwnerMappingCache(s.db)
	return nil
}

func (s Store) DeleteAuthGroupOwnerMapping(authGroup string) error {
	if s.db == nil {
		return fmt.Errorf("database not initialised")
	}

	authGroup = NormalizeAuthGroupKey(authGroup)
	if authGroup == "" {
		return fmt.Errorf("auth group is required")
	}
	if _, err := s.db.Exec("DELETE FROM auth_group_model_owner_mappings WHERE auth_group = ?", authGroup); err != nil {
		return fmt.Errorf("delete auth group owner mapping: %w", err)
	}

	reloadAuthGroupOwnerMappingCache(s.db)
	return nil
}

func reloadAuthGroupOwnerMappingCache(db *sql.DB) {
	rows, err := db.Query("SELECT auth_group, owner, updated_at FROM auth_group_model_owner_mappings")
	if err != nil {
		log.Errorf("sqlite/modelconfig: load auth group owner mapping cache: %v", err)
		return
	}
	defer rows.Close()

	cache := make(map[string]AuthGroupOwnerMappingRow)
	for rows.Next() {
		var row AuthGroupOwnerMappingRow
		if err := rows.Scan(&row.AuthGroup, &row.Owner, &row.UpdatedAt); err != nil {
			log.Errorf("sqlite/modelconfig: scan auth group owner mapping row: %v", err)
			continue
		}
		row.AuthGroup = NormalizeAuthGroupKey(row.AuthGroup)
		row.Owner = NormalizeModelOwnerValue(row.Owner)
		if row.AuthGroup == "" || row.Owner == "" {
			continue
		}
		cache[row.AuthGroup] = row
	}

	authGroupOwnerMappingCacheMu.Lock()
	authGroupOwnerMappingCache = cache
	authGroupOwnerMappingCacheMu.Unlock()
	log.Infof("sqlite/modelconfig: loaded %d auth group owner mappings into cache", len(cache))
}
