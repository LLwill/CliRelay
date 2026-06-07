package routing

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

const createRoutingConfigTableSQL = `
CREATE TABLE IF NOT EXISTS routing_config (
  id         INTEGER PRIMARY KEY NOT NULL CHECK (id = 1),
  payload    TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL DEFAULT ''
);
`

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) Store {
	return Store{db: db}
}

func InitTable(db *sql.DB) {
	if db == nil {
		return
	}
	if _, err := db.Exec(createRoutingConfigTableSQL); err != nil {
		log.Errorf("sqlite/routing: create routing_config table: %v", err)
	}
}

func normalize(input config.RoutingConfig) config.RoutingConfig {
	holder := &config.Config{Routing: input}
	holder.SanitizeRouting()
	return holder.Routing
}

func meaningful(cfg config.RoutingConfig) bool {
	return cfg.Strategy != "" || !cfg.IncludeDefaultGroup || len(cfg.ChannelGroups) > 0 || len(cfg.PathRoutes) > 0
}

func (s Store) Get() *config.RoutingConfig {
	if s.db == nil {
		return nil
	}

	var payload string
	if err := s.db.QueryRow(`SELECT payload FROM routing_config WHERE id = 1`).Scan(&payload); err != nil {
		if err != sql.ErrNoRows {
			log.Warnf("sqlite/routing: load routing_config: %v", err)
		}
		return nil
	}

	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil
	}

	var cfg config.RoutingConfig
	if err := json.Unmarshal([]byte(payload), &cfg); err != nil {
		log.Warnf("sqlite/routing: decode routing_config: %v", err)
		return nil
	}
	normalized := normalize(cfg)
	return &normalized
}

func (s Store) Upsert(cfg config.RoutingConfig) error {
	if s.db == nil {
		return nil
	}

	normalized := normalize(cfg)
	payload, err := json.Marshal(normalized)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		`INSERT INTO routing_config (id, payload, updated_at)
		 VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`,
		string(payload),
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s Store) ApplyToConfig(cfg *config.Config) bool {
	if s.db == nil || cfg == nil {
		return false
	}
	stored := s.Get()
	if stored == nil {
		return false
	}
	cfg.Routing = *stored
	return true
}

func (s Store) MigrateFromConfig(cfg *config.Config) (migrated bool, hadStored bool) {
	if s.db == nil || cfg == nil {
		return false, false
	}
	if s.Get() != nil {
		return false, true
	}
	if !meaningful(cfg.Routing) {
		return false, false
	}
	if err := s.Upsert(cfg.Routing); err != nil {
		log.Errorf("sqlite/routing: migrate routing config: %v", err)
		return false, false
	}
	return true, false
}
