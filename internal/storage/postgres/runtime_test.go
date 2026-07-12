package postgres

import (
	"strings"
	"testing"
)

func TestRuntimeMigrationsCoverCoreTables(t *testing.T) {
	migrations := RuntimeMigrations()
	if len(migrations) != 8 {
		t.Fatalf("RuntimeMigrations len = %d, want 8", len(migrations))
	}
	sqlText := migrations[0].SQL
	for _, table := range []string{
		"request_logs",
		"request_log_content",
		"api_keys",
		"api_key_permission_profiles",
		"model_configs",
		"model_pricing",
		"proxy_pool",
		"routing_config",
		"runtime_settings",
		"identity_fingerprints",
		"ccswitch_import_configs",
	} {
		if !strings.Contains(sqlText, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("runtime migration does not create %s", table)
		}
	}

	scopeSQL := migrations[3].SQL
	for _, table := range []string{"api_keys", "request_logs", "runtime_settings", "identity_fingerprints"} {
		if !strings.Contains(scopeSQL, "ALTER TABLE "+table+" ADD COLUMN IF NOT EXISTS tenant_id") {
			t.Fatalf("tenant scope migration does not alter %s", table)
		}
	}

	constraintsSQL := migrations[4].SQL
	for _, fragment := range []string{
		"ADD PRIMARY KEY (tenant_id, model_id)",
		"FOREIGN KEY (tenant_id, log_id)",
		"idx_ccswitch_import_configs_tenant_route_path",
	} {
		if !strings.Contains(constraintsSQL, fragment) {
			t.Fatalf("tenant constraints migration missing %q", fragment)
		}
	}

	deleteConstraintsSQL := migrations[5].SQL
	for _, fragment := range []string{
		"users_created_by_fkey",
		"audit_logs_actor_user_id_fkey",
		"audit_logs_actor_session_id_fkey",
		"ON DELETE SET NULL",
	} {
		if !strings.Contains(deleteConstraintsSQL, fragment) {
			t.Fatalf("identity delete constraints migration missing %q", fragment)
		}
	}

	ccSwitchConstraintsSQL := migrations[6].SQL
	if !strings.Contains(ccSwitchConstraintsSQL, "ADD PRIMARY KEY (tenant_id, id)") {
		t.Fatal("ccswitch tenant primary key migration is missing composite primary key")
	}

	menuSQL := migrations[7].SQL
	for _, fragment := range []string{"CREATE TABLE IF NOT EXISTS menus", "permission_code", "idx_menus_parent_sort"} {
		if !strings.Contains(menuSQL, fragment) {
			t.Fatalf("dynamic menu migration missing %q", fragment)
		}
	}

	identitySQL := migrations[2].SQL
	for _, table := range []string{"tenants", "users", "roles", "permissions", "role_permissions", "user_roles", "user_sessions", "audit_logs"} {
		if !strings.Contains(identitySQL, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("identity migration does not create %s", table)
		}
	}

	profileSQL := migrations[1].SQL
	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS profile_key",
		"codex_quarantined",
		"ADD PRIMARY KEY (provider, account_key, profile_key)",
		"CREATE TABLE IF NOT EXISTS identity_fingerprint_account_policies",
		"strategy = 'cli_preferred' AND active_profile_key = ''",
	} {
		if !strings.Contains(profileSQL, fragment) {
			t.Fatalf("identity profile migration missing %q", fragment)
		}
	}
}

func TestMigrationChecksumChangesWithSQL(t *testing.T) {
	first := migrationChecksum("SELECT 1")
	second := migrationChecksum("SELECT 2")
	if first == second {
		t.Fatal("migrationChecksum returned identical values for different SQL")
	}
}
