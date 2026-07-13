package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/postgres/compatdriver"
)

// TestApplyMigrationsUpgradeFromPublishedSchema applies only the first N
// migrations (simulating a previously published schema), seeds tenant-scoped
// rows, then upgrades to the full current set and verifies every migration
// version is recorded cleanly and seed data remains readable.
//
// Uses a disposable database so parallel Postgres tests are not disrupted.
func TestApplyMigrationsUpgradeFromPublishedSchema(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CLIRELAY_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("CLIRELAY_POSTGRES_TEST_DSN is not set")
	}
	ctx := context.Background()

	adminDB, err := sql.Open(driverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	if err := adminDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	dbName := fmt.Sprintf("mig_upgrade_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create disposable db: %v", err)
	}
	t.Cleanup(func() {
		// Terminate residual connections then drop.
		_, _ = adminDB.ExecContext(context.Background(), `
			SELECT pg_terminate_backend(pid)
			  FROM pg_stat_activity
			 WHERE datname = $1 AND pid <> pg_backend_pid()
		`, dbName)
		_, _ = adminDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+dbName)
	})

	testDSN, err := replacePostgresDatabase(dsn, dbName)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open(driverName, testDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	all := RuntimeMigrations()
	if len(all) < 3 {
		t.Fatalf("expected at least 3 migrations, got %d", len(all))
	}
	// "Published" baseline: everything before the latest migration.
	baseline := all[:len(all)-1]
	if err := ApplyMigrations(ctx, db, baseline); err != nil {
		t.Fatalf("apply baseline migrations: %v", err)
	}

	// Seed a standard tenant that later migrations must preserve.
	// UUID required; standard tenants require expires_at.
	seedID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tenants (id, slug, name, type, status, expires_at, description, created_at, updated_at, version)
		VALUES (?, ?, ?, 'standard', 'active', ?, '', now(), now(), 1)
		ON CONFLICT (id) DO NOTHING
	`, seedID, "seed-tenant", "Seed Tenant", expires); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants WHERE id = ?`, seedID).Scan(&count); err != nil {
		t.Fatalf("read seed tenant: %v", err)
	}
	if count != 1 {
		t.Fatalf("seed tenant count = %d", count)
	}

	if err := ApplyMigrations(ctx, db, all); err != nil {
		t.Fatalf("upgrade to current schema: %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT version, dirty FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	applied := map[string]bool{}
	for rows.Next() {
		var version string
		var dirty bool
		if err := rows.Scan(&version, &dirty); err != nil {
			t.Fatal(err)
		}
		if dirty {
			t.Fatalf("migration %s is dirty after upgrade", version)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if !applied[m.Version] {
			t.Fatalf("migration %s not applied after upgrade", m.Version)
		}
	}

	var tenantCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants WHERE id = ?`, seedID).Scan(&tenantCount); err != nil {
		t.Fatalf("read seed after upgrade: %v", err)
	}
	if tenantCount != 1 {
		t.Fatalf("tenant seed lost after upgrade: count=%d", tenantCount)
	}
}

func replacePostgresDatabase(dsn, dbName string) (string, error) {
	// Support URL-style DSN used in CI and local docs.
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		u.Path = "/" + dbName
		return u.String(), nil
	}
	// Fallback: key=value DSN
	parts := strings.Fields(dsn)
	out := make([]string, 0, len(parts))
	replaced := false
	for _, p := range parts {
		if strings.HasPrefix(p, "dbname=") {
			out = append(out, "dbname="+dbName)
			replaced = true
			continue
		}
		out = append(out, p)
	}
	if !replaced {
		out = append(out, "dbname="+dbName)
	}
	return strings.Join(out, " "), nil
}
