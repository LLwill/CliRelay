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

// publishedBaselineVersion is the last migration shipped on origin/main before
// the multi-tenant upgrade series. Do not replace this with len(all)-1 — that
// drifts as new migrations land and stops testing the real production upgrade.
const publishedBaselineVersion = "202607100001_identity_fingerprint_profiles"

const systemTenantID = "00000000-0000-0000-0000-000000000001"

// TestApplyMigrationsUpgradeFromPublishedSchema applies only the migrations
// present on the currently published schema, seeds legacy (pre multi-tenant)
// rows, upgrades through the full RuntimeMigrations set, and asserts data
// retention, system-tenant backfill, composite keys, and idempotent re-apply.
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
	baseline := publishedBaselineMigrations(t, all)
	if len(baseline) != 2 {
		t.Fatalf("published baseline should be 2 migrations (runtime + fingerprint profiles), got %d", len(baseline))
	}
	if err := ApplyMigrations(ctx, db, baseline); err != nil {
		t.Fatalf("apply published baseline migrations: %v", err)
	}

	// Seed while multi-tenant tables (tenants, tenant_id columns) do not exist yet.
	seedPublishedSchemaFixture(t, ctx, db)

	if err := ApplyMigrations(ctx, db, all); err != nil {
		t.Fatalf("upgrade to current schema: %v", err)
	}
	assertAllMigrationsClean(t, ctx, db, all)
	assertMigratedFixture(t, ctx, db)

	// Re-apply must stay idempotent: no dirty rows, no data mutation.
	if err := ApplyMigrations(ctx, db, all); err != nil {
		t.Fatalf("second ApplyMigrations: %v", err)
	}
	assertAllMigrationsClean(t, ctx, db, all)
	assertMigratedFixture(t, ctx, db)
}

// publishedBaselineMigrations returns migrations up to and including
// publishedBaselineVersion. Fails immediately if that version is missing so a
// reordered/removed migration cannot silently change the baseline.
func publishedBaselineMigrations(t *testing.T, all []Migration) []Migration {
	t.Helper()
	for i, m := range all {
		if m.Version == publishedBaselineVersion {
			return all[:i+1]
		}
	}
	t.Fatalf("published baseline version %q not found in RuntimeMigrations()", publishedBaselineVersion)
	return nil
}

func seedPublishedSchemaFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	// Guard: multi-tenant tables must not exist yet on the published schema.
	var tenantsExists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'public' AND table_name = 'tenants'
		)
	`).Scan(&tenantsExists); err != nil {
		t.Fatalf("check tenants table: %v", err)
	}
	if tenantsExists {
		t.Fatal("tenants table exists before multi-tenant migrations; baseline is not the published schema")
	}

	ts := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO request_logs (
			timestamp, api_key, api_key_id, model, source, failed, streaming,
			latency_ms, input_tokens, output_tokens, total_tokens, cost
		) VALUES (?, 'legacy-key', 'legacy-key-id', 'gpt-test', 'seed', 0, 0, 10, 1, 2, 3, 0.01)
	`, ts); err != nil {
		t.Fatalf("seed request_logs: %v", err)
	}
	var logID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM request_logs WHERE api_key_id = ?`, "legacy-key-id").Scan(&logID); err != nil {
		t.Fatalf("read seeded request_logs id: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO request_log_content (log_id, timestamp, compression, session_id)
		VALUES (?, ?, 'none', 'sess-legacy')
	`, logID, ts); err != nil {
		t.Fatalf("seed request_log_content: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO model_configs (
			model_id, owned_by, description, enabled, pricing_mode, source, updated_at
		) VALUES ('gpt-test', 'openai', 'legacy model', 1, 'token', 'user', ?)
	`, ts); err != nil {
		t.Fatalf("seed model_configs: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO model_pricing (model_id, input_price_per_million, output_price_per_million, updated_at)
		VALUES ('gpt-test', 1.5, 2.5, ?)
	`, ts); err != nil {
		t.Fatalf("seed model_pricing: %v", err)
	}

	// identity_fingerprint_profiles already applied: PK is (provider, account_key, profile_key).
	if _, err := db.ExecContext(ctx, `
		INSERT INTO identity_fingerprints (
			provider, account_key, profile_key, auth_subject_id, client_product, client_variant,
			version, fields_json, observed_headers_json, created_at, updated_at, last_seen_at
		) VALUES (
			'codex', 'acct-legacy', 'codex_cli_rs', 'subj-legacy', 'codex_cli_rs', 'codex_cli_rs',
			'1.0.0', '{"ua":"cli"}', '{}', ?, ?, ?
		)
	`, ts.Format(time.RFC3339), ts.Format(time.RFC3339), ts.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed identity_fingerprints: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO identity_fingerprint_account_policies (
			provider, account_key, strategy, active_profile_key, revision, updated_at
		) VALUES ('codex', 'acct-legacy', 'cli_preferred', '', 1, ?)
	`, ts.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed identity_fingerprint_account_policies: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO proxy_pool (id, name, url, enabled, description, created_at, updated_at)
		VALUES ('proxy-legacy', 'Legacy Proxy', 'http://127.0.0.1:18080', 1, 'seed', ?, ?)
	`, ts.Format(time.RFC3339), ts.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed proxy_pool: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO routing_config (id, payload, updated_at)
		VALUES (1, '{"strategy":"round_robin"}', ?)
	`, ts.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed routing_config: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO ccswitch_import_configs (
			id, client_type, provider_name, note, default_model, model_mappings,
			allowed_channel_groups, route_path, endpoint_path, usage_auto_interval,
			api_key_field, created_at, updated_at
		) VALUES (
			'ccswitch-legacy', 'claude', 'anthropic', 'seed', 'claude-3', '[]',
			'[]', '/import/legacy', '/v1/messages', 30, 'x-api-key', ?, ?
		)
	`, ts.Format(time.RFC3339), ts.Format(time.RFC3339)); err != nil {
		t.Fatalf("seed ccswitch_import_configs: %v", err)
	}
}

func assertAllMigrationsClean(t *testing.T, ctx context.Context, db *sql.DB, all []Migration) {
	t.Helper()
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
}

func assertMigratedFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	// System tenant is seeded by multi_tenant_identity.
	var systemCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants WHERE id = ? AND type = 'system'`, systemTenantID).Scan(&systemCount); err != nil {
		t.Fatalf("read system tenant: %v", err)
	}
	if systemCount != 1 {
		t.Fatalf("system tenant count = %d", systemCount)
	}

	assertTenantBackfill(t, ctx, db, "request_logs", `api_key_id = 'legacy-key-id'`)
	assertTenantBackfill(t, ctx, db, "model_configs", `model_id = 'gpt-test'`)
	assertTenantBackfill(t, ctx, db, "model_pricing", `model_id = 'gpt-test'`)
	assertTenantBackfill(t, ctx, db, "proxy_pool", `id = 'proxy-legacy'`)
	assertTenantBackfill(t, ctx, db, "routing_config", `id = 1`)
	assertTenantBackfill(t, ctx, db, "ccswitch_import_configs", `id = 'ccswitch-legacy'`)
	assertTenantBackfill(t, ctx, db, "identity_fingerprints", `provider = 'codex' AND account_key = 'acct-legacy' AND profile_key = 'codex_cli_rs'`)
	assertTenantBackfill(t, ctx, db, "identity_fingerprint_account_policies", `provider = 'codex' AND account_key = 'acct-legacy'`)

	var contentTenant string
	var logID int64
	if err := db.QueryRowContext(ctx, `
		SELECT c.tenant_id, c.log_id
		  FROM request_log_content c
		  JOIN request_logs l ON l.tenant_id = c.tenant_id AND l.id = c.log_id
		 WHERE l.api_key_id = 'legacy-key-id'
	`).Scan(&contentTenant, &logID); err != nil {
		t.Fatalf("read request_log_content after upgrade: %v", err)
	}
	if contentTenant != systemTenantID {
		t.Fatalf("request_log_content.tenant_id = %q, want system", contentTenant)
	}

	var provider, account, profile, subject string
	if err := db.QueryRowContext(ctx, `
		SELECT provider, account_key, profile_key, auth_subject_id
		  FROM identity_fingerprints
		 WHERE tenant_id = ? AND provider = 'codex' AND account_key = 'acct-legacy' AND profile_key = 'codex_cli_rs'
	`, systemTenantID).Scan(&provider, &account, &profile, &subject); err != nil {
		t.Fatalf("identity fingerprint lost after upgrade: %v", err)
	}
	if provider != "codex" || account != "acct-legacy" || profile != "codex_cli_rs" || subject != "subj-legacy" {
		t.Fatalf("fingerprint row unexpected: provider=%s account=%s profile=%s subject=%s", provider, account, profile, subject)
	}

	var strategy string
	if err := db.QueryRowContext(ctx, `
		SELECT strategy FROM identity_fingerprint_account_policies
		 WHERE tenant_id = ? AND provider = 'codex' AND account_key = 'acct-legacy'
	`, systemTenantID).Scan(&strategy); err != nil {
		t.Fatalf("identity policy lost after upgrade: %v", err)
	}
	if strategy != "cli_preferred" {
		t.Fatalf("policy strategy = %q", strategy)
	}

	// Composite PK: same business key allowed for another tenant, rejected within system tenant.
	// ON CONFLICT keeps this assertion idempotent when assertMigratedFixture runs twice.
	otherTenant := "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tenants (id, slug, name, type, status, expires_at, description, created_at, updated_at, version)
		VALUES (?, 'other-tenant', 'Other Tenant', 'standard', 'active', ?, '', now(), now(), 1)
		ON CONFLICT (id) DO NOTHING
	`, otherTenant, expires); err != nil {
		t.Fatalf("insert other tenant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO model_configs (
			tenant_id, model_id, owned_by, description, enabled, pricing_mode, source, updated_at
		) VALUES (?, 'gpt-test', 'openai', 'other tenant model', 1, 'token', 'user', now())
		ON CONFLICT (tenant_id, model_id) DO NOTHING
	`, otherTenant); err != nil {
		t.Fatalf("composite PK should allow same model_id for different tenant: %v", err)
	}
	var crossTenantCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM model_configs WHERE model_id = 'gpt-test'
	`).Scan(&crossTenantCount); err != nil {
		t.Fatalf("count model_configs by model_id: %v", err)
	}
	if crossTenantCount != 2 {
		t.Fatalf("expected gpt-test for system + other tenant, count=%d", crossTenantCount)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO model_configs (
			tenant_id, model_id, owned_by, description, enabled, pricing_mode, source, updated_at
		) VALUES (?, 'gpt-test', 'openai', 'dup', 1, 'token', 'user', now())
	`, systemTenantID); err == nil {
		t.Fatal("expected duplicate model_id within same tenant to fail")
	}

	// Idempotency of fixture data: still exactly one legacy proxy / fingerprint under system tenant.
	var proxyCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM proxy_pool WHERE id = 'proxy-legacy'`).Scan(&proxyCount); err != nil {
		t.Fatalf("count proxy_pool: %v", err)
	}
	if proxyCount != 1 {
		t.Fatalf("proxy_pool legacy count = %d", proxyCount)
	}
	var fpCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM identity_fingerprints
		 WHERE provider = 'codex' AND account_key = 'acct-legacy'
	`).Scan(&fpCount); err != nil {
		t.Fatalf("count fingerprints: %v", err)
	}
	if fpCount != 1 {
		t.Fatalf("identity_fingerprints legacy count = %d", fpCount)
	}
}

func assertTenantBackfill(t *testing.T, ctx context.Context, db *sql.DB, table, where string) {
	t.Helper()
	query := fmt.Sprintf(`SELECT tenant_id FROM %s WHERE %s`, table, where)
	var tenantID string
	if err := db.QueryRowContext(ctx, query).Scan(&tenantID); err != nil {
		t.Fatalf("read %s after upgrade: %v", table, err)
	}
	if tenantID != systemTenantID {
		t.Fatalf("%s.tenant_id = %q, want system tenant", table, tenantID)
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
