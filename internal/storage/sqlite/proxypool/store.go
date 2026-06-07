package proxypool

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

const createProxyPoolTableSQL = `
CREATE TABLE IF NOT EXISTS proxy_pool (
  id          TEXT PRIMARY KEY NOT NULL,
  name        TEXT NOT NULL DEFAULT '',
  url         TEXT NOT NULL,
  enabled     INTEGER NOT NULL DEFAULT 1,
  description TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT '',
  updated_at  TEXT NOT NULL DEFAULT ''
);
`

type Store struct {
	db *sql.DB
}

type scanner interface {
	Scan(dest ...any) error
}

func NewStore(db *sql.DB) Store {
	return Store{db: db}
}

func InitTable(db *sql.DB) {
	if db == nil {
		return
	}
	if _, err := db.Exec(createProxyPoolTableSQL); err != nil {
		log.Errorf("sqlite/proxypool: create proxy_pool table: %v", err)
	}
}

func (s Store) Available() bool {
	return s.db != nil
}

func (s Store) List() []config.ProxyPoolEntry {
	if s.db == nil {
		return nil
	}

	rows, err := s.db.Query(`SELECT id, name, url, enabled, description FROM proxy_pool ORDER BY created_at ASC, id ASC`)
	if err != nil {
		log.Errorf("sqlite/proxypool: list proxy_pool: %v", err)
		return nil
	}
	defer rows.Close()

	entries := make([]config.ProxyPoolEntry, 0)
	for rows.Next() {
		entry, ok := scanEntry(rows)
		if ok {
			entries = append(entries, entry)
		}
	}
	if err := rows.Err(); err != nil {
		log.Warnf("sqlite/proxypool: scan proxy_pool rows: %v", err)
	}
	return entries
}

func (s Store) Get(id string) *config.ProxyPoolEntry {
	if s.db == nil {
		return nil
	}

	normalizedID := normalizeEntryID(id)
	if normalizedID == "" {
		return nil
	}
	row := s.db.QueryRow(`SELECT id, name, url, enabled, description FROM proxy_pool WHERE id = ?`, normalizedID)
	entry, ok := scanEntry(row)
	if !ok {
		return nil
	}
	return &entry
}

func (s Store) Replace(entries []config.ProxyPoolEntry) error {
	if s.db == nil {
		return fmt.Errorf("database not initialised")
	}

	normalized := config.NormalizeProxyPool(entries)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM proxy_pool"); err != nil {
		_ = tx.Rollback()
		return err
	}
	if len(normalized) == 0 {
		return tx.Commit()
	}

	stmt, err := tx.Prepare(`INSERT INTO proxy_pool
		(id, name, url, enabled, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, entry := range normalized {
		enabledInt := 0
		if entry.Enabled {
			enabledInt = 1
		}
		if _, err := stmt.Exec(entry.ID, entry.Name, entry.URL, enabledInt, entry.Description, now, now); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s Store) ApplyToConfig(cfg *config.Config) bool {
	if s.db == nil || cfg == nil {
		return false
	}
	cfg.ProxyPool = s.List()
	return true
}

func (s Store) MigrateFromConfig(cfg *config.Config) (migrated int, hadStored bool, shouldClean bool) {
	if s.db == nil || cfg == nil {
		return 0, false, false
	}
	if len(s.List()) > 0 {
		cfg.ProxyPool = nil
		return 0, true, true
	}
	if len(cfg.ProxyPool) == 0 {
		return 0, false, false
	}

	normalized := config.NormalizeProxyPool(cfg.ProxyPool)
	cfg.ProxyPool = nil
	if len(normalized) == 0 {
		return 0, false, true
	}
	if err := s.Replace(normalized); err != nil {
		log.Errorf("sqlite/proxypool: migrate proxy_pool: %v", err)
		return 0, false, false
	}
	log.Infof("sqlite/proxypool: migrated %d proxy_pool entries from config to SQLite", len(normalized))
	return len(normalized), false, true
}

func scanEntry(scanner scanner) (config.ProxyPoolEntry, bool) {
	var entry config.ProxyPoolEntry
	var enabledInt int
	if err := scanner.Scan(&entry.ID, &entry.Name, &entry.URL, &enabledInt, &entry.Description); err != nil {
		if err != sql.ErrNoRows {
			log.Warnf("sqlite/proxypool: scan proxy_pool row: %v", err)
		}
		return config.ProxyPoolEntry{}, false
	}
	entry.Enabled = enabledInt != 0
	return entry, true
}

func normalizeEntryID(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
