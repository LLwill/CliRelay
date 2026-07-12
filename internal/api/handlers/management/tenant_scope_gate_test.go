package management

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/identity"
)

func TestDeniesTenantResourceScopeAllowsPlatformAdminOnGlobalLogs(t *testing.T) {
	businessTenant := identity.Principal{
		PlatformAdmin:   true,
		EffectiveTenant: identity.Tenant{ID: "tenant-potato"},
		HomeTenant:      identity.Tenant{ID: identity.SystemTenantID},
	}
	if deniesTenantResourceScope(businessTenant, "/v0/management/logs") {
		t.Fatal("platform super-admin switched into a business tenant must still reach process-global /logs")
	}

	tenantOperator := identity.Principal{
		PlatformAdmin:   false,
		EffectiveTenant: identity.Tenant{ID: "tenant-potato"},
		HomeTenant:      identity.Tenant{ID: "tenant-potato"},
	}
	if !deniesTenantResourceScope(tenantOperator, "/v0/management/logs") {
		t.Fatal("non-platform tenant session must not reach process-global /logs")
	}
	if deniesTenantResourceScope(tenantOperator, "/v0/management/usage/logs") {
		t.Fatal("tenant-scoped request logs must stay available to business tenants")
	}

	systemSession := identity.Principal{
		PlatformAdmin:   false,
		EffectiveTenant: identity.Tenant{ID: identity.SystemTenantID},
	}
	if deniesTenantResourceScope(systemSession, "/v0/management/logs") {
		t.Fatal("system-tenant session must reach process-global /logs")
	}
}

func TestTenantScopedManagementPathIncludesProviderRuntimeRoutes(t *testing.T) {
	for _, path := range []string{
		"/v0/management/gemini-api-key",
		"/v0/management/claude-api-key",
		"/v0/management/bedrock-api-key",
		"/v0/management/opencode-go-api-key/usage",
		"/v0/management/cline-api-key/usage",
		"/v0/management/ollama-cloud-api-key/usage",
		"/v0/management/codex-api-key",
		"/v0/management/vertex-api-key",
		"/v0/management/openai-compatibility",
		"/v0/management/oauth-model-alias",
		"/v0/management/codex-oauth-admission",
		// Auth-file quota preview for tenant-imported OAuth credentials.
		"/v0/management/api-call",
		// Per-account identity fingerprints must stay readable by tenant admins
		// after credentials migrate off the system tenant.
		"/v0/management/identity-fingerprint",
		"/v0/management/identity-fingerprint/account",
		"/v0/management/identity-fingerprint/account/policy",
		"/v0/management/identity-fingerprint/codex/recommendations",
	} {
		if !isTenantScopedManagementPath(path) {
			t.Errorf("isTenantScopedManagementPath(%q) = false", path)
		}
	}
}

func TestTenantScopedManagementPathKeepsGlobalRuntimeRoutesBlocked(t *testing.T) {
	for _, path := range []string{
		"/v0/management/config.yaml",
		"/v0/management/oauth-excluded-models",
		"/v0/management/proxy-url",
		"/v0/management/request-retry",
		"/v0/management/update/apply",
		"/v0/management/logs",
	} {
		if isTenantScopedManagementPath(path) {
			t.Errorf("isTenantScopedManagementPath(%q) = true", path)
		}
	}
}

func TestProviderManagementRoutesUseProviderPermissions(t *testing.T) {
	for _, path := range []string{
		"/v0/management/gemini-api-key",
		"/v0/management/openai-compatibility",
	} {
		if got := permissionForManagementRequest("GET", path); got != "providers.read" {
			t.Errorf("GET %s permission = %q, want providers.read", path, got)
		}
		if got := permissionForManagementRequest("PUT", path); got != "providers.write" {
			t.Errorf("PUT %s permission = %q, want providers.write", path, got)
		}
	}
	for _, path := range []string{
		"/v0/management/api-call",
		"/v0/management/opencode-go-api-key/usage",
	} {
		if got := permissionForManagementRequest("POST", path); got != "providers.test" {
			t.Errorf("POST %s permission = %q, want providers.test", path, got)
		}
	}
}
